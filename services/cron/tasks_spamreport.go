// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: spam reporting

package cron

import (
	"context"
	"fmt"

	user_model "code.gitea.io/gitea/models/user"
	user_service "code.gitea.io/gitea/services/user"
)

func registerProcessSpamReports() {
	RegisterTaskFatal("process_spam_reports", &BaseConfig{
		Enabled:    true,
		RunAtStart: true,
		Schedule:   "@every 5m",
	}, func(ctx context.Context, doer *user_model.User, _ Config) error {
		// This code assumes that all reports may be processed.
		// If we start accepting reports from non-trusted users, we need to add a check here.
		ids, err := user_model.GetPendingSpamReportIDs(ctx)
		if err != nil {
			return fmt.Errorf("failed to GetPendingSpamReportIDs: %w", err)
		}
		return user_service.ProcessSpamReports(ctx, doer, ids)
	})
}

func initSpamReportTasks() {
	registerProcessSpamReports()
}
