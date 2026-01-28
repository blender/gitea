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
	signedID2Timestamp := make(map[int64]timeutil.TimeStamp)
	for _, sca := range signedContributorAgreements {
		signedID2Timestamp[sca.ContributorAgreementID] = sca.CreatedUnix
	}
	type UserContributorAgreement struct {
		user_model.ContributorAgreement
		SignedAt timeutil.TimeStamp
	}
	userContributorAgreements := make([]*UserContributorAgreement, len(contributorAgreements))
	for i, ca := range contributorAgreements {
		var uca UserContributorAgreement
		uca.ContributorAgreement = *ca
		uca.SignedAt = signedID2Timestamp[ca.ID]
		userContributorAgreements[i] = &uca
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
	if err := db.Insert(ctx, &user_model.SignedContributorAgreement{UserID: ctx.Doer.ID, ContributorAgreementID: ca.ID}); err != nil {
		ctx.ServerError("SignContributorAgreement", err)
		return
	}
	// Check if there are any recent action run failures, trigger a re-run.
	if err := rerunFailedActions(ctx, ctx.Doer); err != nil {
		log.Error("rerunFailedActions for userID=%d failed: %v", ctx.Doer.ID, err)
	}
	ctx.Redirect(setting.AppSubURL + "/user/settings/contributor_agreements")
}

// rerunFailedActions looks for recent cla.yml failures triggered by the user and reruns those.
func rerunFailedActions(ctx *gitea_context.Context, user *user_model.User) error {
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
		if err := db.WithTx(ctx, func(ctx context.Context) error {
			_, err := actions_model.UpdateRunJob(ctx, job, builder.Eq{"status": status}, "task_id", "status", "started", "stopped")
			return err
		}); err != nil {
			return err
		}

		actions_service.CreateCommitStatus(ctx, job)
		notify_service.WorkflowJobStatusUpdate(ctx, job.Run.Repo, job.Run.TriggerUser, job, nil)
	}

	return nil
}
