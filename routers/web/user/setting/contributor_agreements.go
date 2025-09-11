// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: contributor agreement

package setting

import (
	"net/http"

	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/templates"
	"code.gitea.io/gitea/modules/timeutil"
	"code.gitea.io/gitea/services/context"

	"xorm.io/builder"
)

const (
	tplSettingsContributorAgreements templates.TplName = "user/settings/contributor_agreements"
)

// ContributorAgreements lists all agreements and shows which ones were signed.
func ContributorAgreements(ctx *context.Context) {
	contributorAgreements, err := db.Find[user_model.ContributorAgreement](ctx, &db.ListOptions{})
	if err != nil {
		ctx.ServerError("FindContributorAgreements", err)
		return
	}
	signedContributorAgreements, err := db.Find[user_model.SignedContributorAgreement](ctx, &user_model.FindSignedContributorAgreementsOptions{UserID: ctx.Doer.ID})
	if err != nil {
		ctx.ServerError("FindSignedContributorAgreements", err)
		return
	}
	signedID2Timestamp := make(map[int64]timeutil.TimeStamp)
	for _, sca := range signedContributorAgreements {
		signedID2Timestamp[sca.ContributorAgreementID] = sca.CreatedUnix
	}
	type UserContributorAgreement struct {
		user_model.ContributorAgreement
		SignedAt timeutil.TimeStamp
	}
	userContributorAgreements := make([]*UserContributorAgreement, len(contributorAgreements))
	for i, ca := range contributorAgreements {
		var uca UserContributorAgreement
		uca.ContributorAgreement = *ca
		uca.SignedAt = signedID2Timestamp[ca.ID]
		userContributorAgreements[i] = &uca
	}

	ctx.Data["ContributorAgreements"] = userContributorAgreements
	ctx.Data["PageIsSettingsContributorAgreements"] = true
	ctx.Data["Title"] = ctx.Tr("settings.contributor_agreements")
	ctx.HTML(http.StatusOK, tplSettingsContributorAgreements)
}

// SignContributorAgreement signs a contributor agreement.
func SignContributorAgreement(ctx *context.Context) {
	slug := ctx.PathParam("slug")
	ca, exist, err := db.Get[user_model.ContributorAgreement](ctx, builder.Eq{"slug": slug})
	if err != nil {
		ctx.ServerError("GetContributorAgreement", err)
		return
	}
	if !exist {
		ctx.PlainText(http.StatusNotFound, "unknown contributor agreement")
		return
	}
	if err := db.Insert(ctx, &user_model.SignedContributorAgreement{UserID: ctx.Doer.ID, ContributorAgreementID: ca.ID}); err != nil {
		ctx.ServerError("SignContributorAgreement", err)
		return
	}
	ctx.Redirect(setting.AppSubURL + "/user/settings/contributor_agreements")
}
