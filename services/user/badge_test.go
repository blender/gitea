// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: sync user badges

package user

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/models/unittest"
	user_model "code.gitea.io/gitea/models/user"

	"github.com/stretchr/testify/assert"
)

// TestUpdateBadgesBestEffort executes UpdateBadgesBestEffort concurrently.
//
// This test illustrates the need for a database transaction around AddUserBadge and RemoveUserBadge calls.
// This test is not deterministic, but at least it can demonstrate the problem after a few non-cached runs:
//
//     go test -count=1 -v -tags sqlite -run TestUpdateBadgesBestEffort ./services/user/...
func TestUpdateBadgesBestEffort(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	badges := []*user_model.Badge{}
	for i := range 5 {
		badge := &user_model.Badge{Slug: fmt.Sprintf("update-badges-test-%d", i)}
		user_model.CreateBadge(db.DefaultContext, badge)
		badges = append(badges, badge)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	f := func(wg *sync.WaitGroup, badges []*user_model.Badge) {
		<-start
		defer wg.Done()
		UpdateBadgesBestEffort(db.DefaultContext, user, badges)
	}
	updateSets := [][]*user_model.Badge{
		badges[0:1],
		badges[1:3],
		badges[3:5],
	}
	for _, s := range updateSets {
		wg.Add(1)
		go f(&wg, s)
	}
	t.Log("start")
	// Use the channel to start goroutines' execution as close as possible.
	close(start)
	wg.Wait()

	result, _, _ := user_model.GetUserBadges(db.DefaultContext, user)
	resultSlugs := make([]string, 0, len(result))
	for _, b := range result {
		resultSlugs = append(resultSlugs, b.Slug)
	}

	match := false
	for _, set := range updateSets {
		setSlugs := make([]string, 0, len(set))
		for _, b := range set {
			setSlugs = append(setSlugs, b.Slug)
		}
		// Expecting to confirm that what we get at the end is not a mish-mash of different update attempts,
		// but one complete attempt.
		if slices.Equal(setSlugs, resultSlugs) {
			match = true
			break
		}
	}
	if !match {
		t.Fail()
	}
}
