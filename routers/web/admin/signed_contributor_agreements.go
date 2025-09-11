// Copyright 2025 The Gitea Authors.
// SPDX-License-Identifier: MIT

// BLENDER: contributor agreement

package admin

import (
	"fmt"
	"net/http"
	"strings"

	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/templates"
	"code.gitea.io/gitea/services/context"
)

const (
	tplSignedContributorAgreements    templates.TplName = "admin/signed_contributor_agreements/list"
	tplContributorAgreementsBatchSign templates.TplName = "admin/signed_contributor_agreements/batch_sign"
)

// SignedContributorAgreements shows signed contributor agreements.
func SignedContributorAgreements(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.signed_contributor_agreements")
	ctx.Data["PageIsSignedContributorAgreements"] = true

	page := ctx.FormInt("page")
	if page <= 1 {
		page = 1
	}

	opts := user_model.FindSignedContributorAgreementsOptions{
		ListOptions: db.ListOptions{
			PageSize: setting.UI.Admin.UserPagingNum,
			Page:     page,
		},
	}

	contributorAgreementID := ctx.FormInt64("contributor_agreement_id")
	ctx.Data["ContributorAgreementID"] = contributorAgreementID
	if contributorAgreementID != 0 {
		opts.ContributorAgreementID = contributorAgreementID
	}

	keyword := ctx.FormTrim("q")
	ctx.Data["Keyword"] = keyword
	if keyword != "" {
		users, _, err := user_model.SearchUsers(ctx, &user_model.SearchUserOptions{Keyword: keyword, SearchByEmail: true})
		if err != nil {
			ctx.ServerError("SearchUsers", err)
			return
		}
		userIDs := make([]int64, 0, len(users))
		for _, user := range users {
			userIDs = append(userIDs, user.ID)
		}
		opts.UserIDs = userIDs
	}

	signedContributorAgreements, count, err := db.FindAndCount[user_model.SignedContributorAgreement](ctx, &opts)
	if err != nil {
		ctx.ServerError("SignedContributorAgreements", err)
		return
	}
	ctx.Data["SignedContributorAgreements"] = signedContributorAgreements
	pager := context.NewPagination(int(count), opts.PageSize, opts.Page, 5)
	pager.AddParamFromRequest(ctx.Req)
	ctx.Data["Page"] = pager

	contributorAgreements, err := db.Find[user_model.ContributorAgreement](ctx, &db.ListOptions{})
	if err != nil {
		ctx.ServerError("FindContributorAgreements", err)
		return
	}
	ctx.Data["ContributorAgreements"] = contributorAgreements
	contributorAgreementsLookup := make(map[int64]string)
	for _, ca := range contributorAgreements {
		contributorAgreementsLookup[ca.ID] = ca.Slug
	}
	ctx.Data["ContributorAgreementsLookup"] = contributorAgreementsLookup

	userIDs := make([]int64, 0, len(signedContributorAgreements))
	for _, sca := range signedContributorAgreements {
		userIDs = append(userIDs, sca.UserID)
	}
	userMap, err := user_model.GetUsersMapByIDs(ctx, userIDs)
	if err != nil {
		ctx.ServerError("GetUsersMapByIDs", err)
		return
	}
	ctx.Data["UserMap"] = userMap

	ctx.HTML(http.StatusOK, tplSignedContributorAgreements)
}

// ContributorAgreementsBatchSign displays a form for batch signing contributor agreements on users' behalf.
func ContributorAgreementsBatchSign(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.signed_contributor_agreements.batch_sign")
	ctx.Data["PageIsSignedContributorAgreements"] = true

	contributorAgreements, err := db.Find[user_model.ContributorAgreement](ctx, &db.ListOptions{})
	if err != nil {
		ctx.ServerError("FindContributorAgreements", err)
		return
	}
	ctx.Data["ContributorAgreements"] = contributorAgreements

	ctx.HTML(http.StatusOK, tplContributorAgreementsBatchSign)
}

// ContributorAgreementsBatchSign processes a form for batch signing contributor agreements on users' behalf.
func ContributorAgreementsBatchSignPost(ctx *context.Context) {
	comment := ctx.FormTrim("comment")
	contributorAgreementID := ctx.FormInt64("contributor_agreement_id")
	usernames := strings.Fields(ctx.FormTrim("usernames"))

	ctx.Data["Title"] = ctx.Tr("admin.signed_contributor_agreements.batch_sign")
	ctx.Data["PageIsSignedContributorAgreements"] = true

	ctx.Data["Comment"] = comment
	ctx.Data["ContributorAgreementID"] = contributorAgreementID
	ctx.Data["Usernames"] = strings.Join(usernames, "\n")

	contributorAgreements, err := db.Find[user_model.ContributorAgreement](ctx, &db.ListOptions{})
	if err != nil {
		ctx.ServerError("FindContributorAgreements", err)
		return
	}
	ctx.Data["ContributorAgreements"] = contributorAgreements

	exists, err := db.ExistByID[user_model.ContributorAgreement](ctx, contributorAgreementID)
	if err != nil {
		ctx.ServerError("ExistByID", err)
		return
	}
	if !exists {
		ctx.Data["Error"] = fmt.Sprintf("unknown contributor agreement ID=%d", contributorAgreementID)
		ctx.HTML(http.StatusOK, tplContributorAgreementsBatchSign)
		return
	}

	userIDs, err := user_model.GetUserIDsByNames(ctx, usernames, false)
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			ctx.Data["Error"] = err
			log.Error("GetUserIDsByNames: %v", err)
		} else {
			ctx.Data["Error"] = "something went wrong"
		}
		ctx.HTML(http.StatusOK, tplContributorAgreementsBatchSign)
		return
	}
	// Check for duplicates before inserting.
	existingSCA, err := db.Find[user_model.SignedContributorAgreement](ctx, &user_model.FindSignedContributorAgreementsOptions{
		ContributorAgreementID: contributorAgreementID,
		UserIDs:                userIDs,
	})
	if err != nil {
		ctx.ServerError("FindSignedContributorAgreements", err)
		return
	}
	if len(existingSCA) > 0 {
		existingUserIDs := make([]int64, len(existingSCA))
		for i, sca := range existingSCA {
			existingUserIDs[i] = sca.UserID
		}
		existingUsers, err := user_model.GetUserByIDs(ctx, existingUserIDs)
		if err != nil {
			ctx.ServerError("GetUserByIDs", err)
			return
		}
		existingUsernames := make([]string, len(existingUsers))
		for i, user := range existingUsers {
			existingUsernames[i] = user.Name
		}
		ctx.Data["Error"] = fmt.Sprintf("users already signed this contributor agreement: %v", existingUsernames)
		ctx.HTML(http.StatusOK, tplContributorAgreementsBatchSign)
		return
	}

	insertSCA := make([]*user_model.SignedContributorAgreement, len(userIDs))
	commentWithPrefix := fmt.Sprintf("batch signed: %s", comment)
	for i, userID := range userIDs {
		insertSCA[i] = &user_model.SignedContributorAgreement{
			ContributorAgreementID: contributorAgreementID,
			Comment:                commentWithPrefix,
			UserID:                 userID,
		}
	}
	if err := db.Insert(ctx, insertSCA); err != nil {
		log.Error("Insert SignedContributorAgreements: %v", err)
		ctx.Data["Error"] = "something went wrong"
		ctx.HTML(http.StatusOK, tplContributorAgreementsBatchSign)
		return
	}

	ctx.Redirect(setting.AppSubURL + "/-/admin/signed_contributor_agreements")
}
