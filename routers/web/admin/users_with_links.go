package admin

import (
	"net/http"
	"strings"

	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/optional"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/templates"
	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/services/context"
)

const tplUsersWithLinks templates.TplName = "admin/users_with_links"

// UsersWithLinks renders a list of users that contain hyperlinks in bio fields
func UsersWithLinks(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.users.with_links")
	ctx.Data["PageIsAdminUsers"] = true

	// Parse filters from query parameters
	statusActive := ctx.FormString("status_filter[is_active]")
	statusAdmin := ctx.FormString("status_filter[is_admin]")
	statusRestricted := ctx.FormString("status_filter[is_restricted]")
	status2fa := ctx.FormString("status_filter[is_2fa_enabled]")
	statusProhibit := ctx.FormString("status_filter[is_prohibit_login]")

	sort := ctx.FormString("sort")
	if sort == "" {
		sort = "created_unix"
	}
	ctx.Data["SortType"] = sort

	// Build search options
	opts := &user_model.SearchUserOptions{
		ListOptions: db.ListOptions{
			Page:     ctx.FormInt("page"),
			PageSize: setting.UI.Admin.UserPagingNum,
		},
		OrderBy: db.SearchOrderBy(sort),
		Type:    user_model.UserTypeIndividual,

		IsActive:           optional.ParseBool(statusActive),
		IsAdmin:            optional.ParseBool(statusAdmin),
		IsRestricted:       optional.ParseBool(statusRestricted),
		IsTwoFactorEnabled: optional.ParseBool(status2fa),
		IsProhibitLogin:    optional.ParseBool(statusProhibit),

		IncludeReserved: true,
		SearchByEmail:   true,
	}

	users, count, err := user_model.SearchUsers(ctx, opts)
	if err != nil {
		ctx.ServerError("SearchUsers", err)
		return
	}

	// Filter users with hyperlinks in bio fields
	filtered := make([]*user_model.User, 0, len(users))
	for _, u := range users {
		if containsHyperlink(u.FullName) || containsHyperlink(u.Description) ||
			containsHyperlink(u.Location) || containsHyperlink(u.Website) {
			filtered = append(filtered, u)
		}
	}

	ctx.Data["Users"] = filtered
	ctx.Data["Total"] = len(filtered)
	ctx.Data["CanDeleteUsers"] = true

	// Pagination
	pager := context.NewPagination(int(count), opts.PageSize, opts.Page, 5)
	pager.AddParamFromRequest(ctx.Req)
	ctx.Data["Page"] = pager

	ctx.HTML(http.StatusOK, tplUsersWithLinks)
}

func containsHyperlink(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "http://") || strings.Contains(text, "https://")
}
