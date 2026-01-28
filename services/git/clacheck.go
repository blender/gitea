// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: contributor agreement

package git

import (
	"strings"

	git_model "gitea.dev/models/git"
)

// HideClacheckStatus filters out clacheck successes from the collection of commit statuses to avoid noise and confusion in the PR listing and elsewhere.
func HideClacheckStatus(statuses []*git_model.CommitStatus) []*git_model.CommitStatus{
	clacheckIndex := -1
	for i, s := range statuses {
		if s.State.IsSuccess() && strings.Contains(s.Context, "clacheck") {
			clacheckIndex = i
		}
	}
	if clacheckIndex != -1 {
		statuses = append(statuses[:clacheckIndex], statuses[clacheckIndex+1:]...)
	}
	return statuses
}
