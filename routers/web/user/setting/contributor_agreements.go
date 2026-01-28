// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: contributor agreement

package setting

import (
	"context"
	"fmt"
	"net/http"

	actions_model "code.gitea.io/gitea/models/actions"
	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/templates"
	"code.gitea.io/gitea/modules/timeutil"
	actions_service "code.gitea.io/gitea/services/actions"
	gitea_context "code.gitea.io/gitea/services/context"
	notify_service "code.gitea.io/gitea/services/notify"

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
		uca.ContributorAgreement     = *ca
		if sca, has := id2sca[ca.ID]; has {
			uca.Comment  = sca.Comment
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
	if err := db.WithTx(ctx, func(ctx context.Context) error {
		return rerunFailedActions(ctx, doer)
	}); err != nil {
		log.Error("rerunFailedActions for userID=%d failed: %v", doer.ID, err)
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

	// Core logic copied from Rerun func in routers/web/repo/actions/view.go
	for _, run := range runs {
		// reset run's start and stop time
		run.PreviousDuration = run.Duration()
		run.Started = 0
		run.Stopped = 0
		run.Status = actions_model.StatusWaiting
		if err := actions_model.UpdateRun(ctx, run, "started", "stopped", "status", "previous_duration"); err != nil {
			return fmt.Errorf("failed to UpdateRun: %w", err)
		}
		if err = run.LoadAttributes(ctx); err != nil {
			return fmt.Errorf("LoadAttributes: %w", err)
		}
		notify_service.WorkflowRunStatusUpdate(ctx, run.Repo, run.TriggerUser, run)

		jobs, err := actions_model.GetRunJobsByRunID(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("GetRunJobsByRunID: %w", err)
		}
		// We always expect exactly one job in cla.yml
		if len(jobs) != 1 {
			return fmt.Errorf("cla.yml has more than one job, run.ID=%d", run.ID)
		}

		job := jobs[0]
		status := job.Status
		job.TaskID = 0
		job.Status = actions_model.StatusWaiting
		job.Started = 0
		job.Stopped = 0
		if _, err := actions_model.UpdateRunJob(ctx, job, builder.Eq{"status": status}, "task_id", "status", "started", "stopped"); err != nil {
			return fmt.Errorf("UpdateRunJob failed for job.ID=%d: %w", job.ID, err)
		}

		actions_service.CreateCommitStatus(ctx, job)
		notify_service.WorkflowJobStatusUpdate(ctx, job.Run.Repo, job.Run.TriggerUser, job, nil)
	}

	return nil
}
