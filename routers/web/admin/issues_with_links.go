package admin

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"code.gitea.io/gitea/modules/log"

	issue_model "code.gitea.io/gitea/models/issues"
	repo_model "code.gitea.io/gitea/models/repo"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/templates"
	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/services/context"
	user_model "code.gitea.io/gitea/models/user"
)

const tplIssuesWithLinks templates.TplName = "admin/issues_with_links"

type linkItem struct {
	Type        string
	Content     string
	User        *user_model.User
	UserCreated time.Time
	RepoName 		string
	RepoLink    string
	ItemLink    string
	Created     time.Time
	Updated     time.Time
}

func IssuesWithLinks(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.issues_with_links")
	ctx.Data["PageIsIssuesWithLinks"] = true

	page := ctx.FormInt("page")
	if page <= 1 {
		page = 1
	}

	sortField := ctx.FormString("sort")
	sortOrder := strings.ToLower(ctx.FormString("order")) // asc or desc
	if sortOrder != "asc" {
		sortOrder = "desc"
	}
	ctx.Data["Sort"] = sortField
	ctx.Data["Order"] = sortOrder

	// fetch recent issues and comments
	limit := setting.UI.Admin.UserPagingNum
	issues, err := issue_model.GetRecentIssues(ctx, &db.ListOptions{Page: page, PageSize: limit})
	if err != nil {
		ctx.ServerError("GetRecentIssues", err)
		return
	}
	comments, err := issue_model.GetRecentComments(ctx, &db.ListOptions{Page: page, PageSize: limit})
	if err != nil {
		ctx.ServerError("GetRecentComments", err)
		return
	}

	var excludedDomains = []string{
		getDomain(setting.AppURL),
		"github.com",
	}

	var items []linkItem
	appendIfHasLink := func(typeLabel, content, itemLink, repoName string, repoLink string, created time.Time, updated time.Time, u *user_model.User) {
		links := extractAllLinks(content)
		if len(links) == 0 {
			return
		}

		var validLinks []string
		for _, link := range links {
			if !containsExcludedDomain(link, excludedDomains) {
				validLinks = append(validLinks, link)
			}
		}

		if len(validLinks) == 0 {
			return
		}

		items = append(items, linkItem{
			Type:        typeLabel,
			Content:     strings.Join(validLinks, ", "),
			User:        u,
			UserCreated: u.CreatedUnix.AsTime(),
			RepoName:    repoName,
			RepoLink:    repoLink,
			ItemLink:    itemLink,
			Created:     created,
			Updated:		 updated,
		})
	}

	for _, issue := range issues {
		if issue.Content == "" {
			continue
		}
		if issue.Repo == nil {
			issue.Repo, err = repo_model.GetRepositoryByID(ctx, issue.RepoID)
			if err != nil {
				log.Warn("Could not load repo for issue %d: %v", issue.ID, err)
				continue
			}
		}
		u, err := user_model.GetUserByID(ctx, issue.PosterID)
		if err != nil {
			continue
		}
		appendIfHasLink("Issue", issue.Content, issue.HTMLURL(), issue.Repo.Name, issue.Repo.HTMLURL(), issue.CreatedUnix.AsTime(), issue.UpdatedUnix.AsTime(), u)
	}

	for _, comment := range comments {
		if comment.Issue == nil {
			comment.Issue, err = issue_model.GetIssueByID(ctx, comment.IssueID)
			if err != nil {
				log.Warn("Could not load issue for comment %d: %v", comment.ID, err)
				continue
			}
		}
		if comment.Issue.Repo == nil {
			comment.Issue.Repo, err = repo_model.GetRepositoryByID(ctx, comment.Issue.RepoID)
			if err != nil {
				log.Warn("Could not load repo for issue %d: %v", comment.Issue.ID, err)
				continue
			}
		}
		if comment.Content == "" {
			continue
		}
		u, err := user_model.GetUserByID(ctx, comment.PosterID)
		if err != nil {
			continue
		}
		appendIfHasLink("Comment", comment.Content, comment.HTMLURL(ctx), comment.Issue.Repo.Name, comment.Issue.Repo.HTMLURL(), comment.CreatedUnix.AsTime(), comment.UpdatedUnix.AsTime(), u)
	}

	// Sort
	switch sortField {
	case "usercreated":
		sort.Slice(items, func(i, j int) bool {
			if sortOrder == "asc" {
				return items[i].UserCreated.Before(items[j].UserCreated)
			}
			return items[i].UserCreated.After(items[j].UserCreated)
		})
	case "created":
		sort.Slice(items, func(i, j int) bool {
			if sortOrder == "asc" {
				return items[i].Created.Before(items[j].Created)
			}
			return items[i].Created.After(items[j].Created)
		})
	default: // fallback to descending by UserCreated
		sort.Slice(items, func(i, j int) bool {
			return items[i].UserCreated.After(items[j].UserCreated)
		})
	}

	total := len(items)
	start := (page - 1) * limit
	end := start + limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	paged := items[start:end]

	ctx.Data["Items"] = paged
	ctx.Data["Total"] = total

	pager := context.NewPagination(total, limit, page, 5)
	pager.AddParamFromRequest(ctx.Req)
	ctx.Data["Page"] = pager

	ctx.HTML(http.StatusOK, tplIssuesWithLinks)
}

// extractAllLinks returns all http(s) URLs from raw text and Markdown-style links.
func extractAllLinks(text string) []string {
	// Match Markdown-style links: [label](http://example.com)
	mdLinkRegex := regexp.MustCompile(`\[[^\]]*\]\((https?://[^\s\)]+)\)`)
	// Match raw URLs
	rawURLRegex := regexp.MustCompile(`https?://[^\s<>"')\]]+`)

	seen := make(map[string]bool)
	var links []string

	// Extract Markdown links
	for _, match := range mdLinkRegex.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			url := match[1]
			if !seen[url] {
				seen[url] = true
				links = append(links, url)
			}
		}
	}

	// Extract raw URLs
	for _, url := range rawURLRegex.FindAllString(text, -1) {
		if !seen[url] {
			seen[url] = true
			links = append(links, url)
		}
	}

	return links
}

// containsExcludedDomain returns true if any domain in the list matches the link
func containsExcludedDomain(link string, excluded []string) bool {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return false
	}
	for _, d := range excluded {
		if strings.EqualFold(u.Hostname(), d) {
			return true
		}
	}
	return false
}

// getDomain extracts domain from full AppURL (e.g. https://example.com)
func getDomain(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
