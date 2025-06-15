// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package utils

import (
	"html"
	"strings"
)

// SanitizeFlashErrorString will sanitize a flash error string
func SanitizeFlashErrorString(x string) string {
	return strings.ReplaceAll(html.EscapeString(x), "\n", "<br>")
}

func ContainsHyperlink(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "http://") || strings.Contains(text, "https://")
}

func ContainsExcludedDomain(snippet string, domains []string) bool {
	for _, domain := range domains {
		if strings.Contains(snippet, domain) {
			return true
		}
	}
	return false
}
