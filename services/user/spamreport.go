// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: spam reporting

package user

import (
	"context"
	"fmt"
	"strconv"

	"code.gitea.io/gitea/models/db"
	issues_model "code.gitea.io/gitea/models/issues"
	"code.gitea.io/gitea/models/organization"
	project_model "code.gitea.io/gitea/models/project"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/optional"
	"code.gitea.io/gitea/modules/structs"
	issue_service "code.gitea.io/gitea/services/issue"
	repo_service "code.gitea.io/gitea/services/repository"
)

// IsTrustedUser tells if a user is trusted to report spam and to be excluded from others' spam reports.
func IsTrustedUser(ctx context.Context, user *user_model.User) (bool, error) {
	if user.IsAdmin {
		return true, nil
	}
	count, err := organization.GetOrganizationCount(ctx, user)
	if err != nil {
		return false, fmt.Errorf("GetOrganizationCount: %w", err)
	}
	return count > 0, nil
}

// CreateSpamReport checks that a reporter can report a user,
// and inserts a new record in default status=pending
// for further processing.
// If a record for a given user already exists, it will be returned.
func CreateSpamReport(ctx context.Context, reporter, user *user_model.User) (*user_model.SpamReport, error) {
	reporterIsTrusted, err := IsTrustedUser(ctx, reporter)
	if err != nil {
		return nil, fmt.Errorf("failed IsTrustedUser: %w", err)
	}
	if !reporterIsTrusted {
		return nil, fmt.Errorf("reporter %s is not trusted", reporter.Name)
	}
	userIsTrusted, err := IsTrustedUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed IsTrustedUser: %w", err)
	}
	if userIsTrusted {
		return nil, fmt.Errorf("can't report a trusted user %s", user.Name)
	}

	spamReport := &user_model.SpamReport{
		ReporterID: reporter.ID,
		UserID:     user.ID,
	}
	insertErr := db.Insert(ctx, spamReport)
	if insertErr != nil {
		// Normally the error may happen due to a duplicate record.
		// Let's try to fetch the existing record, and if it doesn't exist, escalate the original error.
		existingSpamReport := &user_model.SpamReport{}
		if has, _ := db.GetEngine(ctx).Where("user_id = ?", user.ID).Get(existingSpamReport); has {
			return existingSpamReport, nil
		}
		return nil, insertErr
	}
	return spamReport, nil
}

// ProcessSpamReports performs the cleanup of a reported user account and the content it created.
// Only the reports in "pending" status are processed to avoid race conditions.
// A processed user account becomes inactive, restricted, login prohibited, profile fields erased,
// and the following objects that were created by the user are deleted:
//   - issues and pulls
//   - comments
//   - personal repositories
//   - personal projects
//
// If the processing code fails it leaves the SpamReport record that was being processed in "locked" status.
// It would need to be handled manually, as the error is assumed to be unrecoverable
// (which may not always be true, e.g. during transient db downtime).
//
// We will have to revisit this approach if it actually causes problems.
// E.g. we could
//   - either try to unlock the record on failure (this may not always be possible),
//     or unlock after some timeout (according to the record's UpdatedUnix)
//   - add a new field to keep track of an attempt count per record
//   - retry on subsequent runs, until the attempt budget is exhausted
func ProcessSpamReports(ctx context.Context, doer *user_model.User, spamReportIDs []int64) error {
	var spamReports []user_model.SpamReport
	err := db.GetEngine(ctx).In("id", spamReportIDs).Find(&spamReports)
	if err != nil {
		return fmt.Errorf("failed to fetch SpamReports: %w", err)
	}

	for _, spamReport := range spamReports {
		id := spamReport.ID
		count, err := db.GetEngine(ctx).ID(id).And("status = ?", user_model.SpamReportStatusTypePending).
			Update(&user_model.SpamReport{Status: user_model.SpamReportStatusTypeLocked})
		if err != nil {
			return fmt.Errorf("failed to set SpamReport.Status to locked for id=%d: %w", id, err)
		}
		if count < 1 {
			log.Info("Skipping SpamReport id=%d, status wasn't pending", id)
			continue
		}

		userID := spamReport.UserID
		user := &user_model.User{ID: userID}
		has, err := db.GetEngine(ctx).Get(user)
		if err != nil {
			return fmt.Errorf("failed to fetch user  userID=%d: %w", userID, err)
		}
		if !has {
			return fmt.Errorf("user id=%d was not found", userID)
		}

		// Clean up everything and update report status if there were no errors.
		// On failure the transaction will be rolled back, and the report will be stuck in locked status.
		log.Info("Processing SpamReport id=%d for user %s", id, user.Name)
		err = db.WithTx(ctx, func(ctx context.Context) error {
			if err := cleanupSpam(ctx, user, doer); err != nil {
				return err
			}
			// Everything is cleaned up, marking the spam report as processed.
			count, err = db.GetEngine(ctx).ID(id).And("status = ?", user_model.SpamReportStatusTypeLocked).
				Update(&user_model.SpamReport{Status: user_model.SpamReportStatusTypeProcessed})
			if err != nil {
				return fmt.Errorf("failed to set SpamReport.Status to processed for id=%d: %w", id, err)
			}
			if count < 1 {
				return fmt.Errorf("SpamReport id=%d status wasn't locked, rolling back the transaction", id)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to process SpamReport id=%d: %w", id, err)
		}

		log.Info("Processed SpamReport id=%d for user %s", id, user.Name)
	}
	return nil
}

// cleanupSpam is supposed to be called as a part of a database transaction.
func cleanupSpam(ctx context.Context, user, doer *user_model.User) error {
	// UpdateUser and UpdateAuth to clean the profile and prohibit logins.
	if err := UpdateUser(ctx, user,
		&UpdateOptions{
			Description:     optional.Some(""),
			FullName:        optional.Some("Confirmed Spammer"),
			IsActive:        optional.Some(false),
			IsRestricted:    optional.Some(true),
			Location:        optional.Some(""),
			MaxRepoCreation: optional.Some(0),
			Visibility:      optional.Some(structs.VisibleTypeLimited),
			Website:         optional.Some(""),
		},
	); err != nil {
		return fmt.Errorf("failed to UpdateUser: %w", err)
	}
	if err := UpdateAuth(ctx, user, &UpdateAuthOptions{ProhibitLogin: optional.Some(true)}); err != nil {
		return fmt.Errorf("failed to UpdateAuth: %w", err)
	}

	log.Info("Cleaning up issues and pulls by user %s", user.Name)
	issues, err := issues_model.Issues(ctx, &issues_model.IssuesOptions{
		PosterID: strconv.FormatInt(user.ID, 10),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch IssueIDs: %w", err)
	}
	for _, issue := range issues {
		if err := issue_service.DeleteIssue(ctx, doer, issue); err != nil {
			return fmt.Errorf("failed to delete issue: %w", err)
		}
	}

	log.Info("Cleaning up comments by user %s", user.Name)
	const batchSize = 50
	for {
		comments := make([]*issues_model.Comment, 0, batchSize)
		if err := db.GetEngine(ctx).
			Where("type=? AND poster_id=?", issues_model.CommentTypeComment, user.ID).
			Limit(batchSize, 0).
			Find(&comments); err != nil {
			return fmt.Errorf("failed to find comments to delete: %w", err)
		}
		if len(comments) == 0 {
			break
		}

		for _, comment := range comments {
			if err := issues_model.DeleteComment(ctx, comment); err != nil {
				return fmt.Errorf("failed to delete comment: %w", err)
			}
		}
	}

	log.Info("Cleaning up personal repositories of user %s", user.Name)
	if err := repo_service.DeleteOwnerRepositoriesDirectly(ctx, user); err != nil {
		return fmt.Errorf("failed to clean up repositories: %w", err)
	}

	log.Info("Cleaning up personal projects of user %s", user.Name)
	projectIDs, err := project_model.GetAllProjectsIDsByOwnerIDAndType(ctx, user.ID, project_model.TypeIndividual)
	if err != nil {
		return fmt.Errorf("failed to fetch personal project ids: %w", err)
	}
	for _, projectID := range projectIDs {
		if err := project_model.DeleteProjectByID(ctx, projectID); err != nil {
			return fmt.Errorf("failed to clean up personal project id=%d: %w", projectID, err)
		}
	}
	return nil
}

// DismissSpamReports updates only reports in "pending" status to avoid race conditions
// with the actual processing.
func DismissSpamReports(ctx context.Context, spamReportIDs []int64) error {
	_, err := db.GetEngine(ctx).In("id", spamReportIDs).
		And("status = ?", user_model.SpamReportStatusTypePending).
		Update(&user_model.SpamReport{Status: user_model.SpamReportStatusTypeDismissed})
	return err
}
