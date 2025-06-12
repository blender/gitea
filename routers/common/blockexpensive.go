// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package common

import (
	"context"
	"net/http"
	"strings"

	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/web/middleware"

	"github.com/go-chi/chi/v5"
)

func BlockExpensive() func(next http.Handler) http.Handler {
	if !setting.Service.BlockAnonymousAccessExpensive {
		return nil
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ret := determineRequestPriority(req.Context())
			if !ret.SignedIn {
				if ret.Expensive || ret.LongPolling {
					http.Redirect(w, req, setting.AppSubURL+"/user/login", http.StatusSeeOther)
					return
				}
			}
			next.ServeHTTP(w, req)
		})
	}
}

func isRoutePathExpensive(routePattern string) bool {
	if strings.HasPrefix(routePattern, "/user/") || strings.HasPrefix(routePattern, "/login/") {
		return false
	}

	expensivePaths := []string{
		// code related, very expensive pages
		"/{username}/{reponame}/archive/",
		"/{username}/{reponame}/blame/",
		"/{username}/{reponame}/graph",

		// activity, trailing slash removed
		"/{username}/{reponame}/activity",
	}

	if !(strings.HasPrefix(routePattern, "/blender/") ||
		strings.HasPrefix(routePattern, "/studio/") ||
		strings.HasPrefix(routePattern, "/extensions/") ||
		strings.HasPrefix(routePattern, "/infrastructure/")) {
		// code related, less expensive not allowed in forks
		expensivePaths = append(expensivePaths,
			"/{username}/{reponame}/media/",
			"/{username}/{reponame}/commit/",
			"/{username}/{reponame}/commits/",
			"/{username}/{reponame}/src/",
			"/{username}/{reponame}/raw/")
	}

	for _, path := range expensivePaths {
		if strings.HasPrefix(routePattern, path) {
			return true
		}
	}
	return false
}

func isRoutePathForLongPolling(routePattern string) bool {
	return routePattern == "/user/events"
}

func determineRequestPriority(ctx context.Context) (ret struct {
	SignedIn    bool
	Expensive   bool
	LongPolling bool
},
) {
	dataStore := middleware.GetContextData(ctx)
	chiRoutePath := chi.RouteContext(ctx).RoutePattern()
	if _, ok := dataStore[middleware.ContextDataKeySignedUser].(*user_model.User); ok {
		ret.SignedIn = true
	} else {
		ret.Expensive = isRoutePathExpensive(chiRoutePath)
		ret.LongPolling = isRoutePathForLongPolling(chiRoutePath)
	}
	return ret
}
