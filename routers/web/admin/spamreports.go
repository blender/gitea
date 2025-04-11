// Copyright 2025 The Gitea Authors.
// SPDX-License-Identifier: MIT

// BLENDER: spam reporting

package admin

import (
	"net/http"
	"strconv"

	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/templates"
	"code.gitea.io/gitea/services/context"
	user_service "code.gitea.io/gitea/services/user"
)

const (
	tplSpamReports templates.TplName = "admin/spamreports/list"
)

// GetPendingSpamReports populates the counter for the header section displayed to site admins.
func GetPendingSpamReports(ctx *context.Context) {
	if ctx.Doer == nil || !ctx.Doer.IsAdmin {
		return
	}
	ids, err := user_model.GetPendingSpamReportIDs(ctx)
	if err != nil {
		log.Error("Failed to GetPendingSpamReportIDs while rendering header: %v", err)
		ctx.Data["PendingSpamReports"] = -1
		return
	}
	ctx.Data["PendingSpamReports"] = len(ids)
}

// SpamReports shows spam reports
func SpamReports(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.spamreports")
	ctx.Data["PageIsSpamReports"] = true

	var (
		count        int64
		err          error
		filterStatus user_model.SpamReportStatusType
	)

	// When no value is specified reports are filtered by status=pending (=0),
	// which luckily makes sense as a default view.
	filterStatus = user_model.SpamReportStatusType(ctx.FormInt("status"))
	ctx.Data["FilterStatus"] = filterStatus
	opts := &user_model.ListSpamReportsOptions{
		ListOptions: db.ListOptions{
			PageSize: setting.UI.Admin.UserPagingNum,
			Page:     ctx.FormInt("page"),
		},
		Status: filterStatus,
	}

	if opts.Page <= 1 {
		opts.Page = 1
	}

	spamReports, count, err := user_model.ListSpamReports(ctx, opts)
	if err != nil {
		ctx.ServerError("SpamReports", err)
		return
	}

	ctx.Data["Total"] = count
	ctx.Data["SpamReports"] = spamReports

	statusCounts, err := user_model.GetSpamReportStatusCounts(ctx)
	if err != nil {
		ctx.ServerError("GetSpamReportStatusCounts", err)
		return
	}
	ctx.Data["StatusCounts"] = statusCounts

	var pagerCount int
	for _, sc := range statusCounts {
		if sc.Status == filterStatus {
			pagerCount = int(sc.Count)
			break
		}
	}

	pager := context.NewPagination(pagerCount, opts.PageSize, opts.Page, 5)
	pager.AddParamFromRequest(ctx.Req)
	ctx.Data["Page"] = pager

	ctx.HTML(http.StatusOK, tplSpamReports)
}

// SpamReportsPost handles "process" and "dismiss" actions for pending reports.
// The processing is done synchronously.
func SpamReportsPost(ctx *context.Context) {
	action := ctx.FormString("action")
	// ctx.Req.PostForm is now parsed due to the call to FormString above
	spamReportIDs := make([]int64, 0, len(ctx.Req.PostForm["spamreport_id"]))
	for _, idStr := range ctx.Req.PostForm["spamreport_id"] {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			ctx.ServerError("ParseSpamReportID", err)
			return
		}
		spamReportIDs = append(spamReportIDs, id)
	}

	if action == "process" {
		if err := user_service.ProcessSpamReports(ctx, ctx.Doer, spamReportIDs); err != nil {
			ctx.ServerError("ProcessSpamReports", err)
			return
		}
	}
	if action == "dismiss" {
		if err := user_service.DismissSpamReports(ctx, spamReportIDs); err != nil {
			ctx.ServerError("DismissSpamReports", err)
			return
		}
	}
	ctx.Redirect(setting.AppSubURL + "/-/admin/spamreports")
}

// PurgeSpammerPost is a shortcut for admins to report and process at the same time.
func PurgeSpammerPost(ctx *context.Context) {
	username := ctx.FormString("username")

	user, err := user_model.GetUserByName(ctx, username)
	if err != nil {
		ctx.NotFoundOrServerError("GetUserByName", user_model.IsErrUserNotExist, nil)
		return
	}
	spamReport, err := user_service.CreateSpamReport(ctx, ctx.Doer, user)
	if err != nil {
		ctx.ServerError("CreateSpamReport", err)
		return
	}
	if err := user_service.ProcessSpamReports(ctx, ctx.Doer, []int64{spamReport.ID}); err != nil {
		ctx.ServerError("ProcessSpamReports", err)
		return
	}

	if ctx.Written() {
		return
	}
	ctx.Redirect(setting.AppSubURL + "/" + username)
}
