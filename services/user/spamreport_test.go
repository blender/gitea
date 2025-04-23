// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: spam reporting

package user

import (
	"context"
	"testing"

	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/models/unittest"
	user_model "code.gitea.io/gitea/models/user"

	"github.com/stretchr/testify/assert"
)

func TestIsTrustedUser(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())

	userWithOrgs := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	isTrusted, err := IsTrustedUser(context.Background(), userWithOrgs)
	assert.NoError(t, err)
	assert.True(t, isTrusted)

	userWithoutOrgs := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})
	isTrusted, err = IsTrustedUser(context.Background(), userWithoutOrgs)
	assert.NoError(t, err)
	assert.False(t, isTrusted)

	userWithoutOrgs.IsAdmin = true // now becomes trusted
	isTrusted, err = IsTrustedUser(context.Background(), userWithoutOrgs)
	assert.NoError(t, err)
	assert.True(t, isTrusted)
}

func TestCreateSpamReport(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	// Prevent interaction between tests, for whatever reason db is not reset.
	db.GetEngine(db.DefaultContext).Exec("delete from user_spamreport")

	userWithOrgs := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	userWithoutOrgs := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})

	// An untrusted user can't report.
	_, err := CreateSpamReport(context.Background(), userWithoutOrgs, userWithoutOrgs)
	assert.Error(t, err)

	// A trusted user can't be reported.
	_, err = CreateSpamReport(context.Background(), userWithOrgs, userWithOrgs)
	assert.Error(t, err)

	_, err = CreateSpamReport(context.Background(), userWithOrgs, userWithoutOrgs)
	assert.NoError(t, err)
}

func TestProcessSpamReports(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	// Prevent interaction between tests, for whatever reason db is not reset.
	db.GetEngine(db.DefaultContext).Exec("delete from user_spamreport")

	userWithOrgs := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})    // reporter
	userWithoutOrgs := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 9}) // spammer, and a different one
	_, err := CreateSpamReport(context.Background(), userWithOrgs, userWithoutOrgs)
	assert.NoError(t, err)

	ids, err := user_model.GetPendingSpamReportIDs(context.Background())
	assert.Len(t, ids, 1)
	assert.NoError(t, err)
	cronDoer := &user_model.User{
		ID:        -1,
		Name:      "(Cron)",
		LowerName: "(cron)",
	}
	err = ProcessSpamReports(context.Background(), cronDoer, ids)
	assert.NoError(t, err)
	userWithoutOrgs = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 9}) // reload from db
	assert.Equal(t, "Confirmed Spammer", userWithoutOrgs.FullName)
	assert.True(t, userWithoutOrgs.ProhibitLogin)

	ids, err = user_model.GetPendingSpamReportIDs(context.Background())
	assert.Empty(t, ids)
	assert.NoError(t, err)
}
