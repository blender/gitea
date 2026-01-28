// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// BLENDER: contributor agreement

package user

import (
	"fmt"
	"net/http"
	"strings"

	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/httplib"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/services/context"

	"xorm.io/builder"
)

func CheckContributorAgreement(ctx *context.APIContext) {
	// swagger:operation GET /users/{username}/contributor-agreements/{slug} user userCheckContributorAgreement
	// ---
	// summary: Check if contributor agreement is signed by the user
	// produces:
	// - application/json
	// parameters:
	// - name: username
	//   in: path
	//   description: username of user
	//   type: string
	//   required: true
	// - name: slug
	//   in: path
	//   description: slug of a contributor agreement to check
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/string"
	//   "404":
	//     "$ref": "#/responses/notFound"

	slug := ctx.PathParam("slug")
	ca, exist, err := db.Get[user_model.ContributorAgreement](ctx, builder.Eq{"slug": slug})
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	if !exist {
		ctx.APIError(http.StatusNotFound, fmt.Sprintf("Contributor agreement <%s> is not found.", slug))
		return
	}
	_, exist, err = db.Get[user_model.SignedContributorAgreement](ctx, builder.Eq{
		"contributor_agreement_id": ca.ID,
		"user_id":                  ctx.ContextUser.ID,
	})
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	if !exist {
		url := httplib.MakeAbsoluteURL(ctx, setting.AppSubURL + "/user/settings/contributor_agreements")
		message := []string{
			fmt.Sprintf("Contributor agreement <%s> is not signed.", slug),
			fmt.Sprintf("Sign at %s", url),
		}
		// Add a visible *** decoration.
		maxlen := 0
		for i := range message {
			l := len(message[i])
			if l > maxlen {
				maxlen = l
			}
		}
		hr := strings.Repeat("*", maxlen)
		message = append(append([]string{"", "", hr}, message...), hr, "")
		ctx.PlainText(http.StatusNotFound, strings.Join(message, "\n"))
		return
	}
	ctx.PlainText(http.StatusOK, "OK")
}
