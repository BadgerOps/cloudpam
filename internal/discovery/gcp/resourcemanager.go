package gcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"cloudpam/internal/discovery"
)

const (
	resourceManagerBaseURL = "https://cloudresourcemanager.googleapis.com/v3"

	// ResourceManagerScope is the OAuth scope ListProjects needs.
	ResourceManagerScope = "https://www.googleapis.com/auth/cloud-platform.read-only"

	// maxFolderDepth is GCP's own folder nesting limit. A deeper walk means a
	// malformed response, not a real hierarchy.
	maxFolderDepth = 10
)

var parentPattern = regexp.MustCompile(`^(organizations|folders)/[0-9]+$`)

// ValidParent reports whether parent names an organization or folder.
func ValidParent(parent string) bool { return parentPattern.MatchString(parent) }

// Project is one active GCP project found under an organization or folder.
type Project struct {
	ID     string // projectId, e.g. "my-project-123"
	Name   string // displayName
	Number string // numeric part of "projects/<number>"
}

type rmProject struct {
	Name        string `json:"name"`
	ProjectID   string `json:"projectId"`
	DisplayName string `json:"displayName"`
	State       string `json:"state"`
}

type rmProjectList struct {
	Projects      []rmProject `json:"projects"`
	NextPageToken string      `json:"nextPageToken"`
}

type rmFolder struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type rmFolderList struct {
	Folders       []rmFolder `json:"folders"`
	NextPageToken string     `json:"nextPageToken"`
}

// ListProjects returns every ACTIVE project under parent, walking child folders
// recursively. client must carry credentials with ResourceManagerScope. Any
// listing failure fails the whole call: a partial list would leave projects
// silently undiscovered.
func ListProjects(ctx context.Context, client *http.Client, parent string, limiter *discovery.Limiter) ([]Project, error) {
	if !ValidParent(parent) {
		return nil, fmt.Errorf("invalid parent %q: want organizations/<id> or folders/<id>", parent)
	}
	w := projectWalker{
		client:  computeClient{http: client, limiter: limiter},
		visited: map[string]bool{},
		seen:    map[string]bool{},
	}
	if err := w.walk(ctx, parent, 0); err != nil {
		return nil, err
	}
	return w.projects, nil
}

type projectWalker struct {
	client   computeClient
	visited  map[string]bool
	seen     map[string]bool
	projects []Project
}

func (w *projectWalker) walk(ctx context.Context, node string, depth int) error {
	if depth > maxFolderDepth {
		return fmt.Errorf("folder hierarchy under %s is deeper than %d levels", node, maxFolderDepth)
	}
	if w.visited[node] {
		return nil
	}
	w.visited[node] = true

	for token := ""; ; {
		var page rmProjectList
		if err := w.client.get(ctx, rmListURL("projects", node, token), &page); err != nil {
			return fmt.Errorf("list projects under %s: %w", node, err)
		}
		for _, p := range page.Projects {
			if p.State != "ACTIVE" || p.ProjectID == "" || w.seen[p.ProjectID] {
				continue
			}
			w.seen[p.ProjectID] = true
			w.projects = append(w.projects, Project{
				ID:     p.ProjectID,
				Name:   p.DisplayName,
				Number: strings.TrimPrefix(p.Name, "projects/"),
			})
		}
		if token = page.NextPageToken; token == "" {
			break
		}
	}

	var children []string
	for token := ""; ; {
		var page rmFolderList
		if err := w.client.get(ctx, rmListURL("folders", node, token), &page); err != nil {
			return fmt.Errorf("list folders under %s: %w", node, err)
		}
		for _, f := range page.Folders {
			if f.State == "ACTIVE" && f.Name != "" {
				children = append(children, f.Name)
			}
		}
		if token = page.NextPageToken; token == "" {
			break
		}
	}
	for _, child := range children {
		if err := w.walk(ctx, child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func rmListURL(kind, parent, pageToken string) string {
	q := url.Values{"parent": {parent}}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	return resourceManagerBaseURL + "/" + kind + "?" + q.Encode()
}
