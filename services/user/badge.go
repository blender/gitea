// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"context"
	"fmt"

	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/log"
)

// BLENDER: sync user badges
// This function works in a best-effort fashion:
// it tolerates all errors and tries to perform all badge changes one-by-one.
func UpdateBadgesBestEffort(ctx context.Context, u *user_model.User, newBadges []*user_model.Badge) error {
	return db.WithTx(ctx, func(ctx context.Context) error {
		oldUserBadges, _, err := user_model.GetUserBadges(ctx, u)
		if err != nil {
			return fmt.Errorf("failed to fetch local badges for %s: %w", u.LoginName, err)
		}

		oldBadgeSlugs := map[string]struct{}{}
		for _, badge := range oldUserBadges {
			oldBadgeSlugs[badge.Slug] = struct{}{}
		}

		newBadgeSlugs := map[string]struct{}{}
		for _, badge := range newBadges {
			newBadgeSlugs[badge.Slug] = struct{}{}
		}

		for slug := range newBadgeSlugs {
			if _, has := oldBadgeSlugs[slug]; has {
				continue
			}
			if err := user_model.AddUserBadge(ctx, u, &user_model.Badge{Slug: slug}); err != nil {
				// Don't escalate, continue processing other badges
				log.Error("Failed to add badge slug %s to user %s: %v", slug, u.LoginName, err)
			}
		}
		for slug := range oldBadgeSlugs {
			if _, has := newBadgeSlugs[slug]; has {
				continue
			}
			if err := user_model.RemoveUserBadge(ctx, u, &user_model.Badge{Slug: slug}); err != nil {
				// Don't escalate, continue processing other badges
				log.Error("Failed to remove badge slug %s from user %s: %v", slug, u.LoginName, err)
			}
		}
		return nil
	})
}
