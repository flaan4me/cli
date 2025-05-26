package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/cli/cli/v2/internal/ghinstance"
	ghAPI "github.com/cli/go-gh/v2/pkg/api"
)

const (
	accept          = "Accept"
	authorization   = "Authorization"
	cacheTTL        = "X-GH-CACHE-TTL"
	graphqlFeatures = "GraphQL-Features"
	features        = "merge_queue"
	userAgent       = "User-Agent"
)

var linkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)

func NewClientFromHTTP(httpClient *http.Client) *Client {
	client := &Client{http: httpClient}
	return client
}

type Client struct {
	http *http.Client
}

func (c *Client) HTTP() *http.Client {
	return c.http
}

type GraphQLError struct {
	*ghAPI.GraphQLError
}

type HTTPError struct {
	*ghAPI.HTTPError
	scopesSuggestion string
}

func (err HTTPError) ScopesSuggestion() string {
	return err.scopesSuggestion
}

// GraphQL performs a GraphQL request using the query string and parses the response into data receiver. If there are errors in the response,
// GraphQLError will be returned, but the receiver will also be partially populated.
func (c Client) GraphQL(hostname string, query string, variables map[string]interface{}, data interface{}) error {
	opts := clientOptions(hostname, c.http.Transport)
	opts.Headers[graphqlFeatures] = features
	gqlClient, err := ghAPI.NewGraphQLClient(opts)
	if err != nil {
		return err
	}
	return handleResponse(gqlClient.Do(query, variables, data))
}

// Mutate performs a GraphQL mutation based on a struct and parses the response with the same struct as the receiver. If there are errors in the response,
// GraphQLError will be returned, but the receiver will also be partially populated.
func (c Client) Mutate(hostname, name string, mutation interface{}, variables map[string]interface{}) error {
	opts := clientOptions(hostname, c.http.Transport)
	opts.Headers[graphqlFeatures] = features
	gqlClient, err := ghAPI.NewGraphQLClient(opts)
	if err != nil {
		return err
	}
	return handleResponse(gqlClient.Mutate(name, mutation, variables))
}

// Query performs a GraphQL query based on a struct and parses the response with the same struct as the receiver. If there are errors in the response,
// GraphQLError will be returned, but the receiver will also be partially populated.
func (c Client) Query(hostname, name string, query interface{}, variables map[string]interface{}) error {
	opts := clientOptions(hostname, c.http.Transport)
	opts.Headers[graphqlFeatures] = features
	gqlClient, err := ghAPI.NewGraphQLClient(opts)
	if err != nil {
		return err
	}
	return handleResponse(gqlClient.Query(name, query, variables))
}

// QueryWithContext performs a GraphQL query based on a struct and parses the response with the same struct as the receiver. If there are errors in the response,
// GraphQLError will be returned, but the receiver will also be partially populated.
func (c Client) QueryWithContext(ctx context.Context, hostname, name string, query interface{}, variables map[string]interface{}) error {
	opts := clientOptions(hostname, c.http.Transport)
	opts.Headers[graphqlFeatures] = features
	gqlClient, err := ghAPI.NewGraphQLClient(opts)
	if err != nil {
		return err
	}
	return handleResponse(gqlClient.QueryWithContext(ctx, name, query, variables))
}

// REST performs a REST request and parses the response.
func (c Client) REST(hostname string, method string, p string, body io.Reader, data interface{}) error {
	opts := clientOptions(hostname, c.http.Transport)
	restClient, err := ghAPI.NewRESTClient(opts)
	if err != nil {
		return err
	}
	return handleResponse(restClient.Do(method, p, body, data))
}

func (c Client) RESTWithNext(hostname string, method string, p string, body io.Reader, data interface{}) (string, error) {
	opts := clientOptions(hostname, c.http.Transport)
	restClient, err := ghAPI.NewRESTClient(opts)
	if err != nil {
		return "", err
	}

	resp, err := restClient.Request(method, p, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !success {
		return "", HandleHTTPError(resp)
	}

	if resp.StatusCode == http.StatusNoContent {
		return "", nil
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	err = json.Unmarshal(b, &data)
	if err != nil {
		return "", err
	}

	var next string
	for _, m := range linkRE.FindAllStringSubmatch(resp.Header.Get("Link"), -1) {
		if len(m) > 2 && m[2] == "next" {
			next = m[1]
		}
	}

	return next, nil
}

// HandleHTTPError parses a http.Response into a HTTPError.
func HandleHTTPError(resp *http.Response) error {
	return handleResponse(ghAPI.HandleHTTPError(resp))
}

// handleResponse takes a ghAPI.HTTPError or ghAPI.GraphQLError and converts it into an
// HTTPError or GraphQLError respectively.
func handleResponse(err error) error {
	if err == nil {
		return nil
	}

	var restErr *ghAPI.HTTPError
	if errors.As(err, &restErr) {
		return HTTPError{
			HTTPError: restErr,
			scopesSuggestion: generateScopesSuggestion(restErr.StatusCode,
				restErr.Headers.Get("X-Accepted-Oauth-Scopes"),
				restErr.Headers.Get("X-Oauth-Scopes"),
				restErr.RequestURL.Hostname()),
		}
	}

	var gqlErr *ghAPI.GraphQLError
	if errors.As(err, &gqlErr) {
		return GraphQLError{
			GraphQLError: gqlErr,
		}
	}

	return err
}

// ScopesSuggestion is an error messaging utility that prints the suggestion to request additional OAuth
// scopes in case a server response indicates that there are missing scopes.
func ScopesSuggestion(resp *http.Response) string {
	return generateScopesSuggestion(resp.StatusCode,
		resp.Header.Get("X-Accepted-Oauth-Scopes"),
		resp.Header.Get("X-Oauth-Scopes"),
		resp.Request.URL.Hostname())
}

// EndpointNeedsScopes adds additional OAuth scopes to an HTTP response as if they were returned from the
// server endpoint. This improves HTTP 4xx error messaging for endpoints that don't explicitly list the
// OAuth scopes they need.
func EndpointNeedsScopes(resp *http.Response, s string) *http.Response {
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		oldScopes := resp.Header.Get("X-Accepted-Oauth-Scopes")
		resp.Header.Set("X-Accepted-Oauth-Scopes", fmt.Sprintf("%s, %s", oldScopes, s))
	}
	return resp
}

func generateScopesSuggestion(statusCode int, endpointNeedsScopes, tokenHasScopes, hostname string) string {
	if statusCode < 400 || statusCode > 499 || statusCode == 422 {
		return ""
	}

	if tokenHasScopes == "" {
		return ""
	}

	gotScopes := map[string]struct{}{}
	for _, s := range strings.Split(tokenHasScopes, ",") {
		s = strings.TrimSpace(s)
		gotScopes[s] = struct{}{}

		// Certain scopes may be grouped under a single "top-level" scope. The following branch
		// statements include these grouped/implied scopes when the top-level scope is encountered.
		// See https://docs.github.com/en/developers/apps/building-oauth-apps/scopes-for-oauth-apps.
		if s == "repo" {
			gotScopes["repo:status"] = struct{}{}
			gotScopes["repo_deployment"] = struct{}{}
			gotScopes["public_repo"] = struct{}{}
			gotScopes["repo:invite"] = struct{}{}
			gotScopes["security_events"] = struct{}{}
		} else if s == "user" {
			gotScopes["read:user"] = struct{}{}
			gotScopes["user:email"] = struct{}{}
			gotScopes["user:follow"] = struct{}{}
		} else if s == "codespace" {
			gotScopes["codespace:secrets"] = struct{}{}
		} else if strings.HasPrefix(s, "admin:") {
			gotScopes["read:"+strings.TrimPrefix(s, "admin:")] = struct{}{}
			gotScopes["write:"+strings.TrimPrefix(s, "admin:")] = struct{}{}
		} else if strings.HasPrefix(s, "write:") {
			gotScopes["read:"+strings.TrimPrefix(s, "write:")] = struct{}{}
		}
	}

	for _, s := range strings.Split(endpointNeedsScopes, ",") {
		s = strings.TrimSpace(s)
		if _, gotScope := gotScopes[s]; s == "" || gotScope {
			continue
		}
		return fmt.Sprintf(
			"This API operation needs the %[1]q scope. To request it, run:  gh auth refresh -h %[2]s -s %[1]s",
			s,
			ghinstance.NormalizeHostname(hostname),
		)
	}

	return ""
}

func clientOptions(hostname string, transport http.RoundTripper) ghAPI.ClientOptions {
	// AuthToken, and Headers are being handled by transport,
	// so let go-gh know that it does not need to resolve them.
	opts := ghAPI.ClientOptions{
		AuthToken: "none",
		Headers: map[string]string{
			authorization: "",
		},
		Host:               hostname,
		SkipDefaultHeaders: true,
		Transport:          transport,
		LogIgnoreEnv:       true,
	}
	return opts
}

// GetRepository fetches repository details using the GitHub API
func (c Client) GetRepository(owner, repo string) (*Repository, error) {
	var result struct {
		Repository *Repository
	}
	query := `
	query($owner: String!, $repo: String!) {
		repository(owner: $owner, name: $repo) {
			id
			name
			owner {
				login
			}
			description
			url
			createdAt
			updatedAt
			pushedAt
			isPrivate
			isFork
			isArchived
			isTemplate
			hasIssuesEnabled
			hasWikiEnabled
			hasProjectsEnabled
			hasDiscussionsEnabled
			mergeCommitAllowed
			squashMergeAllowed
			rebaseMergeAllowed
			autoMergeAllowed
			forkCount
			stargazerCount
			watchers {
				totalCount
			}
			issues {
				totalCount
			}
			pullRequests {
				totalCount
			}
			primaryLanguage {
				name
			}
			licenseInfo {
				key
				name
			}
			defaultBranchRef {
				name
			}
			viewerPermission
			viewerCanAdminister
			viewerCanPush
			viewerCanTriage
			viewerCanCreateProjects
			viewerCanCreateDiscussions
			viewerCanCreateIssues
			viewerCanCreatePullRequests
			viewerCanCreateRepositories
			viewerCanCreateTeams
			viewerCanCreateGists
			viewerCanCreatePackages
			viewerCanCreatePages
			viewerCanCreateReleases
			viewerCanCreateDeployments
			viewerCanCreateEnvironments
			viewerCanCreateSecrets
			viewerCanCreateVariables
			viewerCanCreateWebhooks
			viewerCanCreateWorkflows
			viewerCanCreateCodespaces
			viewerCanCreateCodespaceSecrets
			viewerCanCreateCodespaceVariables
			viewerCanCreateCodespaceWebhooks
			viewerCanCreateCodespaceWorkflows
			viewerCanCreateCodespaceDeployments
			viewerCanCreateCodespaceEnvironments
			viewerCanCreateCodespacePackages
			viewerCanCreateCodespacePages
			viewerCanCreateCodespaceReleases
			viewerCanCreateCodespaceTeams
			viewerCanCreateCodespaceRepositories
			viewerCanCreateCodespacePullRequests
			viewerCanCreateCodespaceIssues
			viewerCanCreateCodespaceDiscussions
			viewerCanCreateCodespaceProjects
			viewerCanCreateCodespaceWiki
			viewerCanCreateCodespaceTemplates
			viewerCanCreateCodespaceForks
			viewerCanCreateCodespaceStargazers
			viewerCanCreateCodespaceWatchers
			viewerCanCreateCodespaceIssuesEnabled
			viewerCanCreateCodespaceWikiEnabled
			viewerCanCreateCodespaceProjectsEnabled
			viewerCanCreateCodespaceDiscussionsEnabled
			viewerCanCreateCodespaceMergeCommitAllowed
			viewerCanCreateCodespaceSquashMergeAllowed
			viewerCanCreateCodespaceRebaseMergeAllowed
			viewerCanCreateCodespaceAutoMergeAllowed
			viewerCanCreateCodespaceForkCount
			viewerCanCreateCodespaceStargazerCount
			viewerCanCreateCodespaceWatcherCount
			viewerCanCreateCodespaceIssueCount
			viewerCanCreateCodespacePullRequestCount
			viewerCanCreateCodespacePrimaryLanguage
			viewerCanCreateCodespaceLicenseInfo
			viewerCanCreateCodespaceDefaultBranchRef
			viewerCanCreateCodespaceViewerPermission
			viewerCanCreateCodespaceViewerCanAdminister
			viewerCanCreateCodespaceViewerCanPush
			viewerCanCreateCodespaceViewerCanTriage
			viewerCanCreateCodespaceViewerCanCreateProjects
			viewerCanCreateCodespaceViewerCanCreateDiscussions
			viewerCanCreateCodespaceViewerCanCreateIssues
			viewerCanCreateCodespaceViewerCanCreatePullRequests
			viewerCanCreateCodespaceViewerCanCreateRepositories
			viewerCanCreateCodespaceViewerCanCreateTeams
			viewerCanCreateCodespaceViewerCanCreateGists
			viewerCanCreateCodespaceViewerCanCreatePackages
			viewerCanCreateCodespaceViewerCanCreatePages
			viewerCanCreateCodespaceViewerCanCreateReleases
			viewerCanCreateCodespaceViewerCanCreateDeployments
			viewerCanCreateCodespaceViewerCanCreateEnvironments
			viewerCanCreateCodespaceViewerCanCreateSecrets
			viewerCanCreateCodespaceViewerCanCreateVariables
			viewerCanCreateCodespaceViewerCanCreateWebhooks
			viewerCanCreateCodespaceViewerCanCreateWorkflows
			viewerCanCreateCodespaceViewerCanCreateCodespaces
			viewerCanCreateCodespaceViewerCanCreateCodespaceSecrets
			viewerCanCreateCodespaceViewerCanCreateCodespaceVariables
			viewerCanCreateCodespaceViewerCanCreateCodespaceWebhooks
			viewerCanCreateCodespaceViewerCanCreateCodespaceWorkflows
			viewerCanCreateCodespaceViewerCanCreateCodespaceDeployments
			viewerCanCreateCodespaceViewerCanCreateCodespaceEnvironments
			viewerCanCreateCodespaceViewerCanCreateCodespacePackages
			viewerCanCreateCodespaceViewerCanCreateCodespacePages
			viewerCanCreateCodespaceViewerCanCreateCodespaceReleases
			viewerCanCreateCodespaceViewerCanCreateCodespaceTeams
			viewerCanCreateCodespaceViewerCanCreateCodespaceRepositories
			viewerCanCreateCodespaceViewerCanCreateCodespacePullRequests
			viewerCanCreateCodespaceViewerCanCreateCodespaceIssues
			viewerCanCreateCodespaceViewerCanCreateCodespaceDiscussions
			viewerCanCreateCodespaceViewerCanCreateCodespaceProjects
			viewerCanCreateCodespaceViewerCanCreateCodespaceWiki
			viewerCanCreateCodespaceViewerCanCreateCodespaceTemplates
			viewerCanCreateCodespaceViewerCanCreateCodespaceForks
			viewerCanCreateCodespaceViewerCanCreateCodespaceStargazers
			viewerCanCreateCodespaceViewerCanCreateCodespaceWatchers
			viewerCanCreateCodespaceViewerCanCreateCodespaceIssuesEnabled
			viewerCanCreateCodespaceViewerCanCreateCodespaceWikiEnabled
			viewerCanCreateCodespaceViewerCanCreateCodespaceProjectsEnabled
			viewerCanCreateCodespaceViewerCanCreateCodespaceDiscussionsEnabled
			viewerCanCreateCodespaceViewerCanCreateCodespaceMergeCommitAllowed
			viewerCanCreateCodespaceViewerCanCreateCodespaceSquashMergeAllowed
			viewerCanCreateCodespaceViewerCanCreateCodespaceRebaseMergeAllowed
			viewerCanCreateCodespaceViewerCanCreateCodespaceAutoMergeAllowed
			viewerCanCreateCodespaceViewerCanCreateCodespaceForkCount
			viewerCanCreateCodespaceViewerCanCreateCodespaceStargazerCount
			viewerCanCreateCodespaceViewerCanCreateCodespaceWatcherCount
			viewerCanCreateCodespaceViewerCanCreateCodespaceIssueCount
			viewerCanCreateCodespaceViewerCanCreateCodespacePullRequestCount
			viewerCanCreateCodespaceViewerCanCreateCodespacePrimaryLanguage
			viewerCanCreateCodespaceViewerCanCreateCodespaceLicenseInfo
			viewerCanCreateCodespaceViewerCanCreateCodespaceDefaultBranchRef
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerPermission
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanAdminister
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanPush
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanTriage
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateProjects
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateDiscussions
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateIssues
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreatePullRequests
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateRepositories
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateTeams
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateGists
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreatePackages
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreatePages
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateReleases
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateDeployments
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateEnvironments
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateSecrets
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateVariables
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateWebhooks
			viewerCanCreateCodespaceViewerCanCreateCodespaceViewerCanCreateWorkflows
			viewerCanCreateCodespaceViewerCanCreateCodespaces
		}
	}`
	variables := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
	}
	err := c.GraphQL(ghinstance.Default(), query, variables, &result)
	if err != nil {
		return nil, err
	}
	return result.Repository, nil
}

// GetUser fetches user details using the GitHub API
func (c Client) GetUser(login string) (*User, error) {
	var result struct {
		User *User
	}
	query := `
	query($login: String!) {
		user(login: $login) {
			id
			login
			name
			bio
			company
			location
			email
			websiteUrl
			createdAt
			updatedAt
			followers {
				totalCount
			}
			following {
				totalCount
			}
			repositories {
				totalCount
			}
			gists {
				totalCount
			}
			organizations {
				totalCount
			}
			starredRepositories {
				totalCount
			}
			watching {
				totalCount
			}
			issues {
				totalCount
			}
			pullRequests {
				totalCount
			}
			projects {
				totalCount
			}
			packages {
				totalCount
			}
			securityAdvisories {
				totalCount
			}
			sponsorshipsAsMaintainer {
				totalCount
			}
			sponsorshipsAsSponsor {
				totalCount
			}
			status {
				emoji
				message
			}
		}
	}`
	variables := map[string]interface{}{
		"login": login,
	}
	err := c.GraphQL(ghinstance.Default(), query, variables, &result)
	if err != nil {
		return nil, err
	}
	return result.User, nil
}
