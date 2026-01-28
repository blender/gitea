// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: contributor agreement

package setting

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	actions_model "gitea.dev/models/actions"
	"gitea.dev/models/db"
	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/commitstatus"
	"gitea.dev/modules/log"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/timeutil"
	actions_service "gitea.dev/services/actions"
	gitea_context "gitea.dev/services/context"
	pull_service "gitea.dev/services/pull"
	commitstatus_service "gitea.dev/services/repository/commitstatus"

	"xorm.io/builder"
)

const (
	tplSettingsContributorAgreements templates.TplName = "user/settings/contributor_agreements"
)

// ContributorAgreements lists all agreements and shows which ones were signed.
func ContributorAgreements(ctx *gitea_context.Context) {
	contributorAgreements, err := db.Find[user_model.ContributorAgreement](ctx, &db.ListOptions{})
	if err != nil {
		ctx.ServerError("FindContributorAgreements", err)
		return
	}
	signedContributorAgreements, err := db.Find[user_model.SignedContributorAgreement](ctx, &user_model.FindSignedContributorAgreementsOptions{UserID: ctx.Doer.ID})
	if err != nil {
		ctx.ServerError("FindSignedContributorAgreements", err)
		return
	}
	id2sca := make(map[int64]*user_model.SignedContributorAgreement)
	for _, sca := range signedContributorAgreements {
		id2sca[sca.ContributorAgreementID] = sca
	}
	type UserContributorAgreement struct {
		user_model.ContributorAgreement
		Comment  string
		SignedAt timeutil.TimeStamp
	}
	userContributorAgreements := make([]*UserContributorAgreement, len(contributorAgreements))
	for i, ca := range contributorAgreements {
		var uca UserContributorAgreement
		userContributorAgreements[i] = &uca
		uca.ContributorAgreement = *ca
		if sca, has := id2sca[ca.ID]; has {
			uca.Comment = sca.Comment
			uca.SignedAt = sca.CreatedUnix
		}
	}

	ctx.Data["ContributorAgreements"] = userContributorAgreements
	ctx.Data["PageIsSettingsContributorAgreements"] = true
	ctx.Data["Title"] = ctx.Tr("settings.contributor_agreements")
	ctx.HTML(http.StatusOK, tplSettingsContributorAgreements)
}

// SignContributorAgreement signs a contributor agreement.
func SignContributorAgreement(ctx *gitea_context.Context) {
	slug := ctx.PathParam("slug")
	ca, exist, err := db.Get[user_model.ContributorAgreement](ctx, builder.Eq{"slug": slug})
	if err != nil {
		ctx.ServerError("GetContributorAgreement", err)
		return
	}
	if !exist {
		ctx.PlainText(http.StatusNotFound, "unknown contributor agreement")
		return
	}
	// Prefer to fail on duplicate submission (e.g. when a user hits the button twice).
	// If rerunFailedActions fails, we won't allow to re-enter the flow and
	// - either a manual re-run by an admin
	// - or a re-trigger by the workflow conditions would be required.
	sca := user_model.SignedContributorAgreement{UserID: ctx.Doer.ID, ContributorAgreementID: ca.ID}
	if err := db.Insert(ctx, &sca); err != nil {
		ctx.ServerError("SignContributorAgreement", err)
		return
	}
	// Check if there are any recent action run failures and trigger a re-run.
	// Execute all updates transactionally, otherwise an ActionRun may be left in Waiting status indefinitely
	// when UpdateRun is called but UpdateRunJob is not, e.g. due to request being cancelled by a user.
	//
	// Currently it seems better not to couple signing and rerun in a single transaction,
	// to make sure that nothing interferes with storing of the sca record.
	// Theoretically a rerun may be processed asynchronously.
	doer := ctx.Doer
	if err := rerunFailedActions(ctx, doer); err != nil {
		log.Error("rerunFailedActions for userID=%d failed: %v", doer.ID, err)
	}
	// Update failed statuses created by the clacheck webhook.
	if err := db.WithTx(ctx, func(ctx context.Context) error {
		return updateClacheckCommitStatus(ctx, doer)
	}); err != nil {
		log.Error("updateClacheckCommitStatus for userID=%d failed: %v", doer.ID, err)
	}
	ctx.Redirect(setting.AppSubURL + "/user/settings/contributor_agreements")
}

// rerunFailedActions looks for recent cla.yml failures triggered by the user and reruns those.
// It doesn't pick up runs that were initiated by someone else, e.g. when synchronizing a PR with a target branch.
func rerunFailedActions(ctx context.Context, user *user_model.User) error {
	runs, err := db.Find[actions_model.ActionRun](ctx, actions_model.FindRunOptions{
		ListOptions:   db.ListOptions{PageSize: 10},
		Status:        []actions_model.Status{actions_model.StatusFailure},
		TriggerUserID: user.ID,
		WorkflowID:    "cla.yml",
	})
	if err != nil {
		return fmt.Errorf("failed to find failed action runs: %w", err)
	}

	for _, run := range runs {
		if err := run.LoadAttributes(ctx); err != nil {
			return fmt.Errorf("failed to LoadAttributes runID=%d: %w", run.ID, err)
		}
		_, err := actions_service.RerunWorkflowRunJobs(ctx, run.Repo, run, run.TriggerUser, []*actions_model.ActionRunJob{})
		if err != nil {
			return fmt.Errorf("failed to RerunWorkflowRunJobs runID=%d: %w", run.ID, err)
		}
	}

	return nil
}

// updateClacheckCommitStatus goes through user's recent PRs and looks for failed clacheck statuses to update them.
func updateClacheckCommitStatus(ctx context.Context, user *user_model.User) error {
	pullsAsIssues, err := issues_model.Issues(ctx, &issues_model.IssuesOptions{
		IsPull: optional.Some(true),
		Paginator: &db.ListOptions{
			PageSize: 20,
		},
		PosterID: strconv.FormatInt(user.ID, 10),
		SortType: "recentupdate",
	})
	if err != nil {
		return fmt.Errorf("failed to fetch issues: %w", err)
	}
	issueID2statuses, _, err := pull_service.GetIssuesAllCommitStatus(ctx, pullsAsIssues, false)
	if err != nil {
		return fmt.Errorf("failed to GetIssuesAllCommitStatus: %w", err)
	}
	clacheckStatusContext := "clacheck"
	for _, statuses := range issueID2statuses {
		hasClacheckFailure, hasClacheckSuccess := false, false
		var sha string
		var repoID int64
		for _, status := range statuses {
			if status.Context != clacheckStatusContext {
				continue
			}
			if status.State == commitstatus.CommitStatusFailure {
				hasClacheckFailure = true
				sha = status.SHA
				repoID = status.RepoID
			}
			if status.State == commitstatus.CommitStatusSuccess {
				hasClacheckSuccess = true
			}
		}
		if hasClacheckFailure && !hasClacheckSuccess {
			repo, err := repo_model.GetRepositoryByID(ctx, repoID)
			if err != nil {
				return fmt.Errorf("getRepositoryByID [%d]: %w", repoID, err)
			}
			statusSuccess := git_model.CommitStatus{
				Context:     clacheckStatusContext,
				Description: "Contributor agreement is signed",
				State:       commitstatus.CommitStatusSuccess,
			}
			if err := commitstatus_service.CreateCommitStatus(ctx, repo, user, sha, &statusSuccess); err != nil {
				return fmt.Errorf("failed to CreateCommitStatus for repoID [%d] sha [%s]: %w", repo.ID, sha, err)
			}
		}
	}
	return nil
}
