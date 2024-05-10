// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitrepo

import (
	"context"

	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/globallock"
)

// FetchRemoteCommit fetches a specific commit and its related objects from a remote
// repository into the managed repository.
//
// If no reference (branch, tag, or other ref) points to the fetched commit, it will
// be treated as unreachable and cleaned up by `git gc` after the default prune
// expiration period (2 weeks). Ref: https://www.kernel.org/pub/software/scm/git/docs/git-gc.html
//
// This behavior is sufficient for temporary operations, such as determining the
// merge base between commits.
func FetchRemoteCommit(ctx context.Context, repo, remoteRepo Repository, commitID string) error {
	return globallock.LockAndDo(ctx, getRepoWriteLockKey(repo.RelativePath()), func(ctx context.Context) error {
		// BLENDER: --no-write-commit-graph works around internal server errors comparing
		// branches on some repos, caused by commit-graph-chain.lock files that should not be there.
		return RunCmd(ctx, repo, gitcmd.NewCommand("fetch", "--no-write-commit-graph", "--no-tags").
			AddDynamicArguments(repoPath(remoteRepo)).
			AddDynamicArguments(commitID))
	})
}
