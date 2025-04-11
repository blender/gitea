// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: spam reporting

package setting

import (
	"net/http"

	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/services/context"
	user_service "code.gitea.io/gitea/services/user"
)

// SpamReportUserPost creates a spam report for a given user.
func SpamReportUserPost(ctx *context.Context) {
	canReportSpam, err := user_service.IsTrustedUser(ctx, ctx.Doer)
	if err != nil {
		ctx.ServerError("IsTrustedUser", err)
		return
	}
	if !canReportSpam {
		ctx.PlainText(http.StatusForbidden, "you are not allowed to report spam")
	}
	username := ctx.FormString("username")

	user, err := user_model.GetUserByName(ctx, username)
	if err != nil {
		ctx.NotFoundOrServerError("GetUserByName", user_model.IsErrUserNotExist, nil)
		return
	}
	if err := user_service.CreateSpamReport(ctx, ctx.Doer, user); err != nil {
		ctx.ServerError("CreateSpamReport", err)
		return
	}

	if ctx.Written() {
		return
	}
	ctx.Redirect(setting.AppSubURL + "/" + username)
}
