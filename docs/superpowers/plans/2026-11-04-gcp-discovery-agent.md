# GCP Discovery Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `cmd/cloudpam-agent` discover a single GCP project or every active project under a GCP organization/folder (direct grants or per-project impersonation), with the provider always stated explicitly.

**Architecture:** Two new pure-REST helpers in `internal/discovery/gcp` (Resource Manager project walk, IAM Credentials impersonation token source) feed a new `runGCPOrgSync` in the agent beside the existing AWS org path. The server's `/api/v1/discovery/ingest/org` becomes provider-neutral (`external_id`, required `provider`, provider+external-ID account matching). The Discovery wizard's config strings move into pure builders that gain GCP output; the Helm chart gains GCP values and a required `config.provider`.

**Tech Stack:** Go 1.25 (`net/http`, `golang.org/x/oauth2` v0.36.0, `golang.org/x/oauth2/google`), React/TypeScript/Vitest, Helm 3.

**Spec:** `docs/superpowers/specs/2026-11-04-gcp-discovery-agent-design.md`

## Global Constraints

- Provider values are exactly `aws` and `gcp`. Nothing defaults the provider: not the agent, not the Helm chart, not org ingest.
- Agent error when provider is missing: `provider is required (aws or gcp); set CLOUDPAM_PROVIDER or yaml`.
- Org-ingest per-entry errors: `skipped account: provider is required`, `skipped account: unsupported provider "<v>"`.
- GCP org parent pattern: `^(organizations|folders)/[0-9]+$`.
- Impersonation template placeholder is the literal `{project}`; a template without it is allowed (one shared service account).
- Folder walk depth limit: 10.
- Scopes: Resource Manager `https://www.googleapis.com/auth/cloud-platform.read-only`; IAM Credentials caller `https://www.googleapis.com/auth/cloud-platform`; minted token `https://www.googleapis.com/auth/compute.readonly`; lifetime `3600s`.
- No new Go module dependencies (the repo avoids `google.golang.org/api`; call REST directly).
- No storage interface change, no migration.
- All commands run inside the Nix dev shell: prefix with `nix develop --command` (or run them from a `nix develop` shell).
- Release version for this work: `0.25.0` (minor bump: breaking agent config change on a 0.x line). Both Helm charts' `appVersion` must equal the top `docs/CHANGELOG.md` heading or Helm CI fails.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## File Map

| File | Status | Responsibility |
|---|---|---|
| `internal/discovery/gcp/resourcemanager.go` | Create | `ListProjects` folder/project walk, `ValidParent`, scope constant |
| `internal/discovery/gcp/resourcemanager_test.go` | Create | Walk tests against `httptest` |
| `internal/discovery/gcp/impersonate.go` | Create | `ImpersonatedTokenSource`, `ServiceAccountFor`, `CheckServiceAccountTemplate` |
| `internal/discovery/gcp/impersonate_test.go` | Create | Token source tests |
| `cmd/cloudpam-agent/config.go` | Modify | `Provider`, GCP fields, env, validation |
| `cmd/cloudpam-agent/config_gcp_test.go` | Create | Provider + GCP config tests |
| `cmd/cloudpam-agent/config_load_test.go`, `config_test.go`, `scheduler_test.go` | Modify | Existing fixtures gain `Provider: "aws"` |
| `cmd/cloudpam-agent/main.go` | Modify | Provider-based collector/account, `runAWSOrgSync`, scheduler dispatch |
| `cmd/cloudpam-agent/single_test.go` | Create | Single-account collector/account selection tests |
| `cmd/cloudpam-agent/gcp_org.go` | Create | `runGCPOrgSync`, `gcpOrgDeps`, `defaultGCPOrgDeps` |
| `cmd/cloudpam-agent/gcp_org_test.go` | Create | Org pass tests with fakes |
| `internal/domain/discovery.go` | Modify | `OrgAccountIngest.ExternalID` |
| `internal/api/discovery_handlers.go` | Modify | Provider-neutral `handleOrgIngest` |
| `internal/api/org_ingest_provider_test.go` | Create | New ingest behaviour tests |
| `internal/api/discovery_cov_test.go` | Modify | Existing ingest fixtures gain `provider` |
| `internal/api/openapi.go` | Modify | Route summary/description |
| `deploy/helm/cloudpam-agent/{values.yaml,templates/configmap.yaml,templates/deployment.yaml,ci/*.yaml,Chart.yaml}` | Modify/Create | GCP values, required provider |
| `deploy/helm/cloudpam-server/Chart.yaml` | Modify | `appVersion` bump |
| `scripts/helm-check.sh` | Modify | GCP render + missing-provider failure |
| `ui/src/components/discoveryWizardConfig.ts` | Create | Pure config/IAM builders, tab sets, GCP helpers |
| `ui/src/__tests__/discoveryWizardConfig.test.ts` | Create | Builder tests |
| `ui/src/components/DiscoveryWizard.tsx` | Modify | Provider choice, GCP fields, use builders |
| `ui/src/__tests__/DiscoveryWizardProvider.test.tsx` | Create | Provider switch render test |
| `docs/DISCOVERY.md`, `docs/DISCOVERY_AGENT_PLAN.md`, `docs/CHANGELOG.md`, `CLAUDE.md` | Modify | Docs |

---

### Task 1: GCP Resource Manager project walk

**Files:**
- Create: `internal/discovery/gcp/resourcemanager.go`
- Test: `internal/discovery/gcp/resourcemanager_test.go`

**Interfaces:**
- Consumes: `computeClient{http, limiter}.get(ctx, url, dest)` and `rewriteTransport` (test helper) already in package `gcp`; `discovery.Limiter`, `discovery.HTTPError`.
- Produces:
  - `const ResourceManagerScope = "https://www.googleapis.com/auth/cloud-platform.read-only"`
  - `type Project struct { ID, Name, Number string }`
  - `func ValidParent(parent string) bool`
  - `func ListProjects(ctx context.Context, client *http.Client, parent string, limiter *discovery.Limiter) ([]Project, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/discovery/gcp/resourcemanager_test.go`:

```go
package gcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/discovery"
)

// fakeHierarchy serves Resource Manager v3 projects.list and folders.list from
// in-memory maps keyed by parent, two items per page so paging is exercised.
type fakeHierarchy struct {
	projects map[string][]rmProject
	folders  map[string][]rmFolder
	fail     map[string]int // "projects:<parent>" or "folders:<parent>" -> status
	calls    int
}

func (f *fakeHierarchy) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		parent := r.URL.Query().Get("parent")
		kind := strings.TrimPrefix(r.URL.Path, "/v3/")
		if status, ok := f.fail[kind+":"+parent]; ok {
			w.WriteHeader(status)
			return
		}
		start := 0
		if tok := r.URL.Query().Get("pageToken"); tok != "" {
			start = len(tok) // token is "x" repeated start times
		}
		switch kind {
		case "projects":
			all := f.projects[parent]
			end := min(start+2, len(all))
			resp := rmProjectList{Projects: all[start:end]}
			if end < len(all) {
				resp.NextPageToken = strings.Repeat("x", end)
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "folders":
			all := f.folders[parent]
			end := min(start+2, len(all))
			resp := rmFolderList{Folders: all[start:end]}
			if end < len(all) {
				resp.NextPageToken = strings.Repeat("x", end)
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newFakeRMClient(t *testing.T, f *fakeHierarchy) *http.Client {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}
}

func active(id, name string) rmProject {
	return rmProject{Name: "projects/" + id + "-num", ProjectID: id, DisplayName: name, State: "ACTIVE"}
}

func TestValidParent(t *testing.T) {
	for _, ok := range []string{"organizations/123", "folders/456"} {
		if !ValidParent(ok) {
			t.Errorf("ValidParent(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "organizations/", "organizations/abc", "projects/1", "folders/1/2", " folders/1"} {
		if ValidParent(bad) {
			t.Errorf("ValidParent(%q) = true, want false", bad)
		}
	}
}

func TestListProjectsWalksNestedFoldersAndPages(t *testing.T) {
	f := &fakeHierarchy{
		projects: map[string][]rmProject{
			"organizations/1": {active("root-a", "Root A"), active("root-b", "Root B"), active("root-c", "Root C")},
			"folders/10":      {active("child-a", "Child A"), {ProjectID: "doomed", State: "DELETE_REQUESTED"}},
			"folders/11":      {active("grandchild", "Grandchild"), active("root-a", "Root A dup")},
		},
		folders: map[string][]rmFolder{
			"organizations/1": {{Name: "folders/10", State: "ACTIVE"}, {Name: "folders/99", State: "DELETE_REQUESTED"}},
			"folders/10":      {{Name: "folders/11", State: "ACTIVE"}},
		},
	}
	got, err := ListProjects(t.Context(), newFakeRMClient(t, f), "organizations/1", nil)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	var ids []string
	for _, p := range got {
		ids = append(ids, p.ID)
	}
	want := "root-a,root-b,root-c,child-a,grandchild"
	if strings.Join(ids, ",") != want {
		t.Fatalf("project ids = %v, want %s", ids, want)
	}
	if got[0].Name != "Root A" || got[0].Number != "root-a-num" {
		t.Fatalf("first project = %+v, want name Root A and number root-a-num", got[0])
	}
}

func TestListProjectsRejectsAnInvalidParent(t *testing.T) {
	_, err := ListProjects(t.Context(), http.DefaultClient, "projects/1", nil)
	if err == nil || !strings.Contains(err.Error(), "organizations/<id> or folders/<id>") {
		t.Fatalf("error = %v, want invalid parent", err)
	}
}

func TestListProjectsFailsWhenAnyNodeFails(t *testing.T) {
	f := &fakeHierarchy{
		projects: map[string][]rmProject{"organizations/1": {active("a", "A")}},
		folders:  map[string][]rmFolder{"organizations/1": {{Name: "folders/10", State: "ACTIVE"}}},
		fail:     map[string]int{"projects:folders/10": http.StatusForbidden},
	}
	_, err := ListProjects(t.Context(), newFakeRMClient(t, f), "organizations/1", nil)
	var httpErr *discovery.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusForbidden {
		t.Fatalf("error = %v, want a 403 HTTPError", err)
	}
	if !strings.Contains(err.Error(), "folders/10") {
		t.Fatalf("error = %v, want it to name folders/10", err)
	}
}

func TestListProjectsStopsAtTheDepthLimit(t *testing.T) {
	// Every folder has one child folder, forever.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/folders") {
			parent := r.URL.Query().Get("parent")
			_ = json.NewEncoder(w).Encode(rmFolderList{Folders: []rmFolder{{Name: parent + "0", State: "ACTIVE"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(rmProjectList{})
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}

	_, err := ListProjects(t.Context(), client, "folders/1", nil)
	if err == nil || !strings.Contains(err.Error(), "deeper than 10 levels") {
		t.Fatalf("error = %v, want depth limit", err)
	}
}

func TestListProjectsCountsEveryPageAgainstTheLimiter(t *testing.T) {
	f := &fakeHierarchy{
		projects: map[string][]rmProject{"organizations/1": {active("a", "A"), active("b", "B"), active("c", "C")}},
	}
	limiter := discovery.NewLimiter(1000)
	if _, err := ListProjects(t.Context(), newFakeRMClient(t, f), "organizations/1", limiter); err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	// 2 project pages + 1 folder page.
	if f.calls != 3 {
		t.Fatalf("calls = %d, want 3", f.calls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop --command go test ./internal/discovery/gcp/ -run 'ValidParent|ListProjects' -v`
Expected: FAIL to compile with `undefined: rmProject`, `undefined: ListProjects`, `undefined: ValidParent`.

- [ ] **Step 3: Implement**

Create `internal/discovery/gcp/resourcemanager.go`:

```go
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
```

Note: the depth test's infinite chain is `folders/1` → `folders/10` → `folders/100` …; the 11th child (depth 11) trips the guard.

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop --command go test ./internal/discovery/gcp/ -v -run 'ValidParent|ListProjects'`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/gcp/resourcemanager.go internal/discovery/gcp/resourcemanager_test.go
git commit -m "feat(discovery): list gcp projects under an organization or folder (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: GCP service-account impersonation token source

**Files:**
- Create: `internal/discovery/gcp/impersonate.go`
- Test: `internal/discovery/gcp/impersonate_test.go`

**Interfaces:**
- Consumes: `computeScope` const in `collector.go`; `rewriteTransport` test helper; `discovery.Limiter`, `discovery.HTTPError`.
- Produces:
  - `const IAMCredentialsScope = "https://www.googleapis.com/auth/cloud-platform"`
  - `func ServiceAccountFor(template, project string) string`
  - `func CheckServiceAccountTemplate(template string) error`
  - `func ImpersonatedTokenSource(ctx context.Context, client *http.Client, email string, limiter *discovery.Limiter) oauth2.TokenSource`

- [ ] **Step 1: Write the failing tests**

Create `internal/discovery/gcp/impersonate_test.go`:

```go
package gcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/discovery"
)

func TestServiceAccountFor(t *testing.T) {
	if got := ServiceAccountFor("cloudpam-discovery@{project}.iam.gserviceaccount.com", "p-1"); got != "cloudpam-discovery@p-1.iam.gserviceaccount.com" {
		t.Fatalf("ServiceAccountFor = %q", got)
	}
	shared := "central@ops.iam.gserviceaccount.com"
	if got := ServiceAccountFor(shared, "p-1"); got != shared {
		t.Fatalf("ServiceAccountFor without placeholder = %q, want %q", got, shared)
	}
}

func TestCheckServiceAccountTemplate(t *testing.T) {
	for _, ok := range []string{"cloudpam-discovery@{project}.iam.gserviceaccount.com", "central@ops.iam.gserviceaccount.com"} {
		if err := CheckServiceAccountTemplate(ok); err != nil {
			t.Errorf("CheckServiceAccountTemplate(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"not-an-email", "a@b", "{project}", "a b@c.d"} {
		if err := CheckServiceAccountTemplate(bad); err == nil {
			t.Errorf("CheckServiceAccountTemplate(%q) = nil, want error", bad)
		}
	}
}

func TestImpersonatedTokenSourceMintsAComputeToken(t *testing.T) {
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	var gotPath string
	var gotBody map[string]any
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken": "minted",
			"expireTime":  expiry.Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}

	ts := ImpersonatedTokenSource(t.Context(), client, "sa@p-1.iam.gserviceaccount.com", discovery.NewLimiter(1000))
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if tok.AccessToken != "minted" || tok.TokenType != "Bearer" || !tok.Expiry.Equal(expiry) {
		t.Fatalf("token = %+v, want minted Bearer expiring %s", tok, expiry)
	}
	if gotPath != "/v1/projects/-/serviceAccounts/sa@p-1.iam.gserviceaccount.com:generateAccessToken" {
		t.Fatalf("path = %q", gotPath)
	}
	scopes, _ := gotBody["scope"].([]any)
	if len(scopes) != 1 || scopes[0] != computeScope || gotBody["lifetime"] != "3600s" {
		t.Fatalf("body = %v, want compute.readonly scope and 3600s lifetime", gotBody)
	}

	// A second call reuses the unexpired token.
	if _, err := ts.Token(); err != nil {
		t.Fatalf("second Token() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("generateAccessToken calls = %d, want 1", calls)
	}
}

func TestImpersonatedTokenSourceSurfacesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"secret detail"}}`))
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}

	_, err := ImpersonatedTokenSource(t.Context(), client, "sa@p.iam.gserviceaccount.com", nil).Token()
	var httpErr *discovery.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusForbidden {
		t.Fatalf("error = %v, want a 403 HTTPError", err)
	}
	if !strings.Contains(err.Error(), "sa@p.iam.gserviceaccount.com") {
		t.Fatalf("error = %v, want it to name the service account", err)
	}
	if strings.Contains(err.Error(), "secret detail") {
		t.Fatalf("error = %v, must not echo the provider response body", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop --command go test ./internal/discovery/gcp/ -run 'ServiceAccount|Impersonated' -v`
Expected: FAIL to compile with `undefined: ServiceAccountFor`.

- [ ] **Step 3: Implement**

Create `internal/discovery/gcp/impersonate.go`:

```go
package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"cloudpam/internal/discovery"
)

const (
	iamCredentialsBaseURL = "https://iamcredentials.googleapis.com/v1"

	// IAMCredentialsScope is the scope the caller's credentials need to mint an
	// impersonated token.
	IAMCredentialsScope = "https://www.googleapis.com/auth/cloud-platform"

	projectPlaceholder = "{project}"
)

var serviceAccountEmail = regexp.MustCompile(`^[^@\s{}]+@[^@\s{}]+\.[^@\s{}]+$`)

// ServiceAccountFor substitutes project into an impersonation template. A
// template without the {project} placeholder names one shared service account.
func ServiceAccountFor(template, project string) string {
	return strings.ReplaceAll(template, projectPlaceholder, project)
}

// CheckServiceAccountTemplate rejects a template that does not yield an email
// address once {project} is substituted.
func CheckServiceAccountTemplate(template string) error {
	if !serviceAccountEmail.MatchString(ServiceAccountFor(template, "project-id")) {
		return fmt.Errorf("service account %q is not an email address (use {project} for the project id)", template)
	}
	return nil
}

// ImpersonatedTokenSource returns a token source that mints Compute read-only
// access tokens for email through the IAM Credentials API. client must be
// authenticated as an identity holding roles/iam.serviceAccountTokenCreator on
// email, with IAMCredentialsScope. Tokens are cached until they expire.
func ImpersonatedTokenSource(ctx context.Context, client *http.Client, email string, limiter *discovery.Limiter) oauth2.TokenSource {
	return oauth2.ReuseTokenSource(nil, &impersonatedSource{ctx: ctx, client: client, email: email, limiter: limiter})
}

type impersonatedSource struct {
	ctx     context.Context
	client  *http.Client
	email   string
	limiter *discovery.Limiter
}

func (s *impersonatedSource) Token() (*oauth2.Token, error) {
	if err := s.limiter.Wait(s.ctx); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{
		"scope":    []string{computeScope},
		"lifetime": "3600s",
	})
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/projects/-/serviceAccounts/%s:generateAccessToken", iamCredentialsBaseURL, url.PathEscape(s.email))
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("impersonate %s: build request: %w", s.email, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("impersonate %s: %w", s.email, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("impersonate %s: %w", s.email, &discovery.HTTPError{Status: resp.StatusCode})
	}

	var out struct {
		AccessToken string    `json:"accessToken"`
		ExpireTime  time.Time `json:"expireTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("impersonate %s: decode response: %w", s.email, err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("impersonate %s: response carried no access token", s.email)
	}
	return &oauth2.Token{AccessToken: out.AccessToken, TokenType: "Bearer", Expiry: out.ExpireTime}, nil
}
```

Check `discovery.HTTPError.Error()` does not include a body (it only has `Status`); if it does, the "must not echo" assertion will catch it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `nix develop --command go test ./internal/discovery/gcp/ -v`
Expected: PASS (all existing gcp tests plus 4 new).

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/gcp/impersonate.go internal/discovery/gcp/impersonate_test.go
git commit -m "feat(discovery): mint impersonated gcp tokens for per-project discovery (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Agent configuration: required provider and GCP settings

**Files:**
- Modify: `cmd/cloudpam-agent/config.go`
- Create: `cmd/cloudpam-agent/config_gcp_test.go`
- Modify: `cmd/cloudpam-agent/config_load_test.go`, `cmd/cloudpam-agent/config_test.go`, `cmd/cloudpam-agent/scheduler_test.go` (fixtures only)

**Interfaces:**
- Consumes: `gcp.ValidParent`, `gcp.CheckServiceAccountTemplate` (Tasks 1-2); `discovery.ParseRPS`, `discovery.CheckRPS`.
- Produces (fields on `Config`, used by Tasks 4-5):
  - `Provider string` (`yaml:"provider"`)
  - `GCPProject string` (`yaml:"gcp_project"`)
  - `GCPRegions []string` (`yaml:"gcp_regions"`)
  - `DiscoveryGCPRPS float64` (`yaml:"discovery_gcp_rps"`)
  - `GCPOrg GCPOrg` (`yaml:"gcp_org"`) with `Enabled bool`, `Parent string`, `Regions []string`, `ExcludeProjects []string`, `ImpersonateServiceAccount string`
  - `const providerAWS = "aws"`, `const providerGCP = "gcp"`

- [ ] **Step 1: Write the failing tests**

Create `cmd/cloudpam-agent/config_gcp_test.go`:

```go
package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func covBaseConfig(provider string) *Config {
	return &Config{
		ServerURL: "https://pam.example.com", APIKey: "cpk", AgentName: "agent", AccountID: 1,
		SyncInterval: time.Minute, HeartbeatInterval: 10 * time.Second,
		Provider: provider,
	}
}

func TestValidateRequiresAProvider(t *testing.T) {
	err := covBaseConfig("").Validate()
	if err == nil || err.Error() != "provider is required (aws or gcp); set CLOUDPAM_PROVIDER or yaml" {
		t.Fatalf("Validate() error = %v", err)
	}
	err = covBaseConfig("azure").Validate()
	if err == nil || !strings.Contains(err.Error(), `unsupported provider "azure" (aws or gcp)`) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateGCPSettings(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"aws org on gcp", func(c *Config) { c.GCPProject = "p"; c.AWSOrg.Enabled = true }, "aws_org.enabled requires provider aws"},
		{"gcp org on aws", func(c *Config) { c.Provider = "aws"; c.GCPOrg = GCPOrg{Enabled: true, Parent: "organizations/1"} }, "gcp_org.enabled requires provider gcp"},
		{"missing project", func(c *Config) {}, "gcp_project is required"},
		{"bad parent", func(c *Config) { c.GCPOrg = GCPOrg{Enabled: true, Parent: "projects/1"} }, "gcp_org.parent"},
		{"missing parent", func(c *Config) { c.GCPOrg = GCPOrg{Enabled: true} }, "gcp_org.parent"},
		{"bad template", func(c *Config) {
			c.GCPOrg = GCPOrg{Enabled: true, Parent: "folders/2", ImpersonateServiceAccount: "nope"}
		}, "gcp_org.impersonate_service_account"},
		{"bad rps", func(c *Config) { c.GCPProject = "p"; c.DiscoveryGCPRPS = -1 }, "discovery_gcp_rps"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := covBaseConfig("gcp")
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAcceptsGCPModes(t *testing.T) {
	single := covBaseConfig("gcp")
	single.GCPProject = "my-project"
	if err := single.Validate(); err != nil {
		t.Fatalf("single-project Validate() = %v", err)
	}
	org := covBaseConfig("gcp")
	org.GCPOrg = GCPOrg{Enabled: true, Parent: "organizations/123", ImpersonateServiceAccount: "cloudpam-discovery@{project}.iam.gserviceaccount.com"}
	if err := org.Validate(); err != nil {
		t.Fatalf("org Validate() = %v", err)
	}
}

func TestLoadConfigReadsGCPEnv(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "gcp-agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "3")
	t.Setenv("CLOUDPAM_PROVIDER", "gcp")
	t.Setenv("CLOUDPAM_GCP_REGIONS", "us-central1, europe-west1")
	t.Setenv("CLOUDPAM_DISCOVERY_GCP_RPS", "4")
	t.Setenv("CLOUDPAM_GCP_ORG_ENABLED", "1")
	t.Setenv("CLOUDPAM_GCP_ORG_PARENT", "folders/42")
	t.Setenv("CLOUDPAM_GCP_ORG_REGIONS", "us-east1")
	t.Setenv("CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS", "sandbox, ,legacy")
	t.Setenv("CLOUDPAM_GCP_ORG_IMPERSONATE_SA", "d@{project}.iam.gserviceaccount.com")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Provider != "gcp" || cfg.DiscoveryGCPRPS != 4 {
		t.Fatalf("provider/rps = %q/%v", cfg.Provider, cfg.DiscoveryGCPRPS)
	}
	if !reflect.DeepEqual(cfg.GCPRegions, []string{"us-central1", "europe-west1"}) {
		t.Fatalf("GCPRegions = %#v", cfg.GCPRegions)
	}
	want := GCPOrg{
		Enabled: true, Parent: "folders/42", Regions: []string{"us-east1"},
		ExcludeProjects:           []string{"sandbox", "legacy"},
		ImpersonateServiceAccount: "d@{project}.iam.gserviceaccount.com",
	}
	if !reflect.DeepEqual(cfg.GCPOrg, want) {
		t.Fatalf("GCPOrg = %#v, want %#v", cfg.GCPOrg, want)
	}
}

func TestLoadConfigReadsGCPYAML(t *testing.T) {
	covIsolateAgentEnv(t)
	path := covWriteAgentConfig(t, `
server_url: https://yaml.example.com
api_key: cpk
agent_name: yaml-gcp
account_id: 9
provider: gcp
gcp_project: my-project
gcp_regions: [us-central1]
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Provider != "gcp" || cfg.GCPProject != "my-project" || !reflect.DeepEqual(cfg.GCPRegions, []string{"us-central1"}) {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadConfigRejectsMissingProvider(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "1")
	if _, err := LoadConfig(""); err == nil || !strings.Contains(err.Error(), "provider is required") {
		t.Fatalf("LoadConfig() error = %v, want provider required", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `nix develop --command go test ./cmd/cloudpam-agent/ -run 'Provider|GCP' -v`
Expected: FAIL to compile with `unknown field Provider in struct literal` / `undefined: GCPOrg`.

- [ ] **Step 3: Implement config changes**

In `cmd/cloudpam-agent/config.go`:

1. Add the import `"cloudpam/internal/discovery/gcp"`.
2. Below the `AWSOrg` type add:

```go
const (
	providerAWS = "aws"
	providerGCP = "gcp"
)

// GCPOrg holds GCP organization/folder discovery configuration.
type GCPOrg struct {
	Enabled                   bool     `yaml:"enabled"`
	Parent                    string   `yaml:"parent"`
	Regions                   []string `yaml:"regions"`
	ExcludeProjects           []string `yaml:"exclude_projects"`
	ImpersonateServiceAccount string   `yaml:"impersonate_service_account"`
}
```

3. In `Config`, add after `AgentIDFile`:

```go
	Provider          string        `yaml:"provider"`
```

and after `AWSOrg`:

```go
	GCPProject      string   `yaml:"gcp_project"`
	GCPRegions      []string `yaml:"gcp_regions"`
	DiscoveryGCPRPS float64  `yaml:"discovery_gcp_rps"`
	GCPOrg          GCPOrg   `yaml:"gcp_org"`
```

4. In `LoadConfig`, after the `CLOUDPAM_AGENT_ID_FILE` block add:

```go
	if v := os.Getenv("CLOUDPAM_PROVIDER"); v != "" {
		cfg.Provider = strings.TrimSpace(v)
	}
```

and after the AWS Organization env block (before `// Validate required fields`) add:

```go
	// GCP env vars
	if v := os.Getenv("CLOUDPAM_GCP_PROJECT"); v != "" {
		cfg.GCPProject = strings.TrimSpace(v)
	}
	if v := os.Getenv("CLOUDPAM_GCP_REGIONS"); v != "" {
		cfg.GCPRegions = splitList(v)
	}
	if v := os.Getenv("CLOUDPAM_DISCOVERY_GCP_RPS"); v != "" {
		rps, err := discovery.ParseRPS(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CLOUDPAM_DISCOVERY_GCP_RPS: %w", err)
		}
		cfg.DiscoveryGCPRPS = rps
	}
	if v := os.Getenv("CLOUDPAM_GCP_ORG_ENABLED"); v == "true" || v == "1" {
		cfg.GCPOrg.Enabled = true
	}
	if v := os.Getenv("CLOUDPAM_GCP_ORG_PARENT"); v != "" {
		cfg.GCPOrg.Parent = strings.TrimSpace(v)
	}
	if v := os.Getenv("CLOUDPAM_GCP_ORG_REGIONS"); v != "" {
		cfg.GCPOrg.Regions = splitList(v)
	}
	if v := os.Getenv("CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS"); v != "" {
		cfg.GCPOrg.ExcludeProjects = splitList(v)
	}
	if v := os.Getenv("CLOUDPAM_GCP_ORG_IMPERSONATE_SA"); v != "" {
		cfg.GCPOrg.ImpersonateServiceAccount = strings.TrimSpace(v)
	}
```

5. Add the helper at the bottom of the file:

```go
// splitList splits a comma-separated setting, trimming spaces and dropping
// empty entries.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
```

6. In `Validate()`, directly after the `account_id` check, insert:

```go
	if err := c.validateProvider(); err != nil {
		return err
	}
```

and after the existing `discovery_aws_rps` check, before `return nil`, add:

```go
	if err := discovery.CheckRPS(c.DiscoveryGCPRPS); err != nil {
		return fmt.Errorf("discovery_gcp_rps: %w", err)
	}
```

7. Add the method:

```go
// validateProvider checks the provider and the settings that depend on it.
// Nothing defaults the provider, so a third one can be added without guessing
// what an unset value meant.
func (c *Config) validateProvider() error {
	switch c.Provider {
	case "":
		return errors.New("provider is required (aws or gcp); set CLOUDPAM_PROVIDER or yaml")
	case providerAWS, providerGCP:
	default:
		return fmt.Errorf("unsupported provider %q (aws or gcp)", c.Provider)
	}
	if c.AWSOrg.Enabled && c.Provider != providerAWS {
		return fmt.Errorf("aws_org.enabled requires provider aws (provider is %q)", c.Provider)
	}
	if c.GCPOrg.Enabled && c.Provider != providerGCP {
		return fmt.Errorf("gcp_org.enabled requires provider gcp (provider is %q)", c.Provider)
	}
	if c.Provider != providerGCP {
		return nil
	}
	if !c.GCPOrg.Enabled {
		if c.GCPProject == "" {
			return errors.New("gcp_project is required when provider is gcp and gcp_org is off (set CLOUDPAM_GCP_PROJECT or yaml)")
		}
		return nil
	}
	if !gcp.ValidParent(c.GCPOrg.Parent) {
		return fmt.Errorf("gcp_org.parent %q must be organizations/<id> or folders/<id> (set CLOUDPAM_GCP_ORG_PARENT or yaml)", c.GCPOrg.Parent)
	}
	if t := c.GCPOrg.ImpersonateServiceAccount; t != "" {
		if err := gcp.CheckServiceAccountTemplate(t); err != nil {
			return fmt.Errorf("gcp_org.impersonate_service_account: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Update existing test fixtures**

1. In `cmd/cloudpam-agent/config_load_test.go`, append to `covAgentEnvKeys`:

```go
	"CLOUDPAM_PROVIDER",
	"CLOUDPAM_GCP_PROJECT",
	"CLOUDPAM_GCP_REGIONS",
	"CLOUDPAM_DISCOVERY_GCP_RPS",
	"CLOUDPAM_GCP_ORG_ENABLED",
	"CLOUDPAM_GCP_ORG_PARENT",
	"CLOUDPAM_GCP_ORG_REGIONS",
	"CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS",
	"CLOUDPAM_GCP_ORG_IMPERSONATE_SA",
```

2. Run `nix develop --command go test ./cmd/cloudpam-agent/ 2>&1 | grep -E '^\s+--- FAIL|provider is required'` to list the existing tests that now fail with `provider is required`. For each:
   - a `Config{...}` literal passed to `Validate()`: add `Provider: "aws",`;
   - a `LoadConfig` test that sets env: add `t.Setenv("CLOUDPAM_PROVIDER", "aws")` right after `covIsolateAgentEnv(t)`;
   - a `LoadConfig` test that reads YAML only: add `provider: aws` to the YAML body.
   - Tests that expect a *different* validation error (e.g. `TestCovValidateRejectsIncompleteConfigs`): add `Provider: "aws"` to `base()` so the intended error is still the first one hit. Note `server_url`/`api_key`/`agent_name`/`account_id` checks run before the provider check, so those cases keep their messages.
3. In `cmd/cloudpam-agent/scheduler_test.go`, add `Provider: "aws",` to every `&Config{...}` literal (the `runSync` test asserts `acct.Provider == "aws"`, which Task 4 derives from config).

- [ ] **Step 5: Run the package tests**

Run: `nix develop --command go test ./cmd/cloudpam-agent/ -v 2>&1 | tail -30`
Expected: PASS, including the 6 new tests.

- [ ] **Step 6: Commit**

```bash
git add cmd/cloudpam-agent/
git commit -m "feat(agent): require a provider and accept gcp project and org settings (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Agent: provider-based single-account discovery and AWS org entries

**Files:**
- Modify: `cmd/cloudpam-agent/main.go`
- Create: `cmd/cloudpam-agent/single_test.go`
- Modify: `cmd/cloudpam-agent/scheduler_test.go` only if a test referenced `runOrgSync` by name

**Interfaces:**
- Consumes: Task 3 `Config` fields, `providerAWS`, `providerGCP`; `gcp.New`, `aws.New`.
- Produces:
  - `func newSingleAccountCollector(cfg *Config) (discovery.Collector, error)`
  - `func singleAccount(cfg *Config) domain.Account`
  - `runOrgSync` renamed to `func runAWSOrgSync(ctx context.Context, cfg *Config, pusher *Pusher, syncJobID *uuid.UUID, logger *slog.Logger)`
  - `domain.OrgAccountIngest.ExternalID string` (`json:"external_id"`), added here because the agent sends it

- [ ] **Step 1: Write the failing tests**

Create `cmd/cloudpam-agent/single_test.go`:

```go
package main

import (
	"reflect"
	"testing"

	"cloudpam/internal/domain"
)

func TestSingleAccountForEachProvider(t *testing.T) {
	aws := singleAccount(&Config{Provider: "aws", AccountID: 5, AWSRegions: []string{"us-east-1"}, GCPProject: "ignored"})
	if !reflect.DeepEqual(aws, domain.Account{ID: 5, Provider: "aws", Regions: []string{"us-east-1"}}) {
		t.Fatalf("aws account = %+v", aws)
	}
	gcp := singleAccount(&Config{Provider: "gcp", AccountID: 6, GCPProject: "my-project", GCPRegions: []string{"us-central1"}, AWSRegions: []string{"ignored"}})
	if !reflect.DeepEqual(gcp, domain.Account{ID: 6, Provider: "gcp", ExternalID: "my-project", Regions: []string{"us-central1"}}) {
		t.Fatalf("gcp account = %+v", gcp)
	}
}

func TestNewSingleAccountCollectorMatchesTheProvider(t *testing.T) {
	for _, provider := range []string{"aws", "gcp"} {
		c, err := newSingleAccountCollector(&Config{Provider: provider})
		if err != nil {
			t.Fatalf("%s: error = %v", provider, err)
		}
		if c.Provider() != provider {
			t.Fatalf("%s: collector provider = %q", provider, c.Provider())
		}
	}
	if _, err := newSingleAccountCollector(&Config{Provider: "azure"}); err == nil {
		t.Fatal("azure: want an error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `nix develop --command go test ./cmd/cloudpam-agent/ -run 'SingleAccount' -v`
Expected: FAIL to compile with `undefined: singleAccount`.

- [ ] **Step 3: Implement**

1. In `internal/domain/discovery.go`, replace the `OrgAccountIngest` type with:

```go
// OrgAccountIngest is one cloud account's (AWS account or GCP project)
// discovered resources for bulk org ingest.
type OrgAccountIngest struct {
	// ExternalID is the provider's account identifier: the AWS account ID or the
	// GCP project ID.
	ExternalID string `json:"external_id,omitempty"`
	// AWSAccountID is the deprecated spelling of ExternalID, still accepted so
	// agents older than 0.25.0 keep working.
	AWSAccountID string               `json:"aws_account_id,omitempty"`
	AccountName  string               `json:"account_name"`
	AccountEmail string               `json:"account_email"`
	Provider     string               `json:"provider"`
	Regions      []string             `json:"regions"`
	Resources    []DiscoveredResource `json:"resources"`
}
```

2. In `cmd/cloudpam-agent/main.go`:
   - Add import `"cloudpam/internal/discovery/gcp"` and `"fmt"`.
   - Replace

```go
	// Create AWS collector
	collector := aws.New()
	collector.SetLimiter(discovery.NewLimiter(cfg.DiscoveryAWSRPS))
```

   with

```go
	collector, err := newSingleAccountCollector(cfg)
	if err != nil {
		logger.Error("failed to create collector", "error", err)
		os.Exit(1)
	}
```

   - Add `"provider", cfg.Provider,` to the `cloudpam-agent starting` log fields.
   - Rename `runOrgSync` to `runAWSOrgSync` (definition and the call in `runScheduler`). In its `ingestAccounts = append(...)` literal, add `ExternalID: orgAcct.ID,` next to the existing `AWSAccountID: orgAcct.ID,` (keep both: equal values are accepted by the new server, and an older server still reads `aws_account_id`).
   - Replace the body of `runSync` from `// Create mock account with AWS regions` through the `account := ...` literal with:

```go
	account := singleAccount(cfg)
```

   and change its first log line to `logger.Info("starting discovery sync", "account_id", cfg.AccountID, "provider", cfg.Provider)`.
   - Add these functions at the end of `main.go`:

```go
// newSingleAccountCollector returns the collector for single-account mode.
func newSingleAccountCollector(cfg *Config) (discovery.Collector, error) {
	switch cfg.Provider {
	case providerAWS:
		c := aws.New()
		c.SetLimiter(discovery.NewLimiter(cfg.DiscoveryAWSRPS))
		return c, nil
	case providerGCP:
		c := gcp.New()
		c.SetLimiter(discovery.NewLimiter(cfg.DiscoveryGCPRPS))
		return c, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}

// singleAccount is the account single-account mode discovers. The server owns
// the real record; the agent only needs what its collector reads.
func singleAccount(cfg *Config) domain.Account {
	account := domain.Account{ID: cfg.AccountID, Provider: cfg.Provider}
	switch cfg.Provider {
	case providerAWS:
		account.Regions = cfg.AWSRegions
	case providerGCP:
		account.ExternalID = cfg.GCPProject
		account.Regions = cfg.GCPRegions
	}
	return account
}
```

   - Confirm `aws.New()` returns a type with `SetLimiter` (it is used that way today) and that `aws` collector's `Provider()` returns `"aws"`.

- [ ] **Step 4: Run tests**

Run: `nix develop --command go test ./cmd/cloudpam-agent/ ./internal/domain/ -v 2>&1 | tail -20`
Expected: PASS.

Run: `nix develop --command go build ./...`
Expected: no output (the server still compiles; it reads `AWSAccountID` until Task 6).

- [ ] **Step 5: Commit**

```bash
git add cmd/cloudpam-agent/ internal/domain/discovery.go
git commit -m "feat(agent): pick the single-account collector from the provider (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Agent: GCP org-mode sync

**Files:**
- Create: `cmd/cloudpam-agent/gcp_org.go`
- Create: `cmd/cloudpam-agent/gcp_org_test.go`
- Modify: `cmd/cloudpam-agent/main.go` (`runScheduler` dispatch)

**Interfaces:**
- Consumes: `gcp.ListProjects`, `gcp.ResourceManagerScope`, `gcp.Project` (Task 1); `gcp.ImpersonatedTokenSource`, `gcp.ServiceAccountFor`, `gcp.IAMCredentialsScope` (Task 2); `gcp.New`, `gcp.NewWithTokenSource`, `(*gcp.Collector).SetLimiter`; `Pusher.PushOrgResources(ctx, domain.BulkIngestRequest, maxRetries int, backoff time.Duration) error`; `domain.OrgAccountIngest.ExternalID` (Task 4).
- Produces:
  - `type gcpOrgDeps struct { listProjects func(ctx context.Context, parent string, limiter *discovery.Limiter) ([]gcp.Project, error); collectorFor func(ctx context.Context, project string, limiter *discovery.Limiter) (discovery.Collector, error) }`
  - `func defaultGCPOrgDeps(cfg *Config) gcpOrgDeps`
  - `func runGCPOrgSync(ctx context.Context, cfg *Config, pusher *Pusher, syncJobID *uuid.UUID, logger *slog.Logger, deps gcpOrgDeps)`

- [ ] **Step 1: Write the failing tests**

Create `cmd/cloudpam-agent/gcp_org_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/discovery"
	"cloudpam/internal/discovery/gcp"
	"cloudpam/internal/domain"
)

type projectCollector struct {
	project string
	fail    bool
	partial bool
	seen    *[]domain.Account
	mu      *sync.Mutex
}

func (c projectCollector) Provider() string { return "gcp" }

func (c projectCollector) Discover(_ context.Context, account domain.Account) ([]domain.DiscoveredResource, error) {
	c.mu.Lock()
	*c.seen = append(*c.seen, account)
	c.mu.Unlock()
	res := []domain.DiscoveredResource{{Provider: "gcp", ResourceType: domain.ResourceTypeVPC, ResourceID: c.project + "-net", CIDR: "10.0.0.0/16"}}
	switch {
	case c.fail:
		return nil, errors.New("compute api disabled")
	case c.partial:
		return res, errors.New("discover addresses: 403")
	}
	return res, nil
}

type orgIngestRecorder struct {
	mu   sync.Mutex
	reqs []domain.BulkIngestRequest
}

func newOrgIngestServer(t *testing.T) (*orgIngestRecorder, string) {
	t.Helper()
	rec := &orgIngestRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/discovery/ingest/org" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req domain.BulkIngestRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, req)
		rec.mu.Unlock()
		_ = json.NewEncoder(w).Encode(domain.BulkIngestResponse{AccountsProcessed: len(req.Accounts)})
	}))
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

func gcpOrgConfig() *Config {
	return &Config{
		Provider: "gcp", AccountID: 1, MaxRetries: 0, RetryBackoff: time.Millisecond,
		GCPRegions: []string{"us-central1"},
		GCPOrg:     GCPOrg{Enabled: true, Parent: "organizations/1", ExcludeProjects: []string{"sandbox"}},
	}
}

func TestRunGCPOrgSyncPushesEveryHealthyProject(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	var mu sync.Mutex
	var seen []domain.Account
	var gotParent string
	deps := gcpOrgDeps{
		listProjects: func(_ context.Context, parent string, _ *discovery.Limiter) ([]gcp.Project, error) {
			gotParent = parent
			return []gcp.Project{
				{ID: "prod", Name: "Production"},
				{ID: "sandbox", Name: "Sandbox"},
				{ID: "broken", Name: "Broken"},
				{ID: "half", Name: "Half"},
				{ID: "noauth", Name: "No Auth"},
				{ID: "dev"},
			}, nil
		},
		collectorFor: func(_ context.Context, project string, _ *discovery.Limiter) (discovery.Collector, error) {
			if project == "noauth" {
				return nil, errors.New("impersonate: 403")
			}
			return projectCollector{project: project, fail: project == "broken", partial: project == "half", seen: &seen, mu: &mu}, nil
		},
	}
	jobID := uuid.New()

	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, &jobID, covQuietLogger(), deps)

	if gotParent != "organizations/1" {
		t.Fatalf("parent = %q", gotParent)
	}
	if len(rec.reqs) != 1 {
		t.Fatalf("org pushes = %d, want 1", len(rec.reqs))
	}
	req := rec.reqs[0]
	if req.SyncJobID == nil || *req.SyncJobID != jobID {
		t.Fatalf("sync_job_id = %v, want %s", req.SyncJobID, jobID)
	}
	var ids []string
	for _, a := range req.Accounts {
		ids = append(ids, a.ExternalID)
		if a.Provider != "gcp" || a.AWSAccountID != "" || !reflect.DeepEqual(a.Regions, []string{"us-central1"}) {
			t.Fatalf("entry = %+v", a)
		}
	}
	if !reflect.DeepEqual(ids, []string{"prod", "dev"}) {
		t.Fatalf("pushed projects = %v, want [prod dev] (sandbox excluded, broken/half/noauth skipped)", ids)
	}
	if req.Accounts[0].AccountName != "Production" || req.Accounts[1].AccountName != "dev" {
		t.Fatalf("names = %q, %q", req.Accounts[0].AccountName, req.Accounts[1].AccountName)
	}
	for _, a := range seen {
		if a.Provider != "gcp" || a.ExternalID == "" {
			t.Fatalf("collector saw account %+v", a)
		}
	}
}

func TestRunGCPOrgSyncPrefersOrgRegions(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	var mu sync.Mutex
	var seen []domain.Account
	cfg := gcpOrgConfig()
	cfg.GCPOrg.Regions = []string{"europe-west1"}
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return []gcp.Project{{ID: "p"}}, nil
		},
		collectorFor: func(_ context.Context, project string, _ *discovery.Limiter) (discovery.Collector, error) {
			return projectCollector{project: project, seen: &seen, mu: &mu}, nil
		},
	}
	runGCPOrgSync(context.Background(), cfg, pusher, nil, covQuietLogger(), deps)
	if len(seen) != 1 || !reflect.DeepEqual(seen[0].Regions, []string{"europe-west1"}) {
		t.Fatalf("collector regions = %+v", seen)
	}
	if len(rec.reqs) != 1 || !reflect.DeepEqual(rec.reqs[0].Accounts[0].Regions, []string{"europe-west1"}) {
		t.Fatalf("pushed regions = %+v", rec.reqs)
	}
}

func TestRunGCPOrgSyncPushesNothingWhenListingFails(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return nil, errors.New("list folders under organizations/1: 403")
		},
		collectorFor: func(context.Context, string, *discovery.Limiter) (discovery.Collector, error) {
			t.Fatal("collectorFor must not be called when listing fails")
			return nil, nil
		},
	}
	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, nil, covQuietLogger(), deps)
	if len(rec.reqs) != 0 {
		t.Fatalf("org pushes = %d, want 0", len(rec.reqs))
	}
}

func TestRunGCPOrgSyncPushesNothingWhenNoProjectSucceeds(t *testing.T) {
	rec, url := newOrgIngestServer(t)
	pusher := NewPusher(url, "cpk", uuid.New(), 5*time.Second, covQuietLogger())
	deps := gcpOrgDeps{
		listProjects: func(context.Context, string, *discovery.Limiter) ([]gcp.Project, error) {
			return []gcp.Project{{ID: "sandbox"}}, nil
		},
		collectorFor: func(context.Context, string, *discovery.Limiter) (discovery.Collector, error) {
			t.Fatal("excluded project must not be discovered")
			return nil, nil
		},
	}
	runGCPOrgSync(context.Background(), gcpOrgConfig(), pusher, nil, covQuietLogger(), deps)
	if len(rec.reqs) != 0 {
		t.Fatalf("org pushes = %d, want 0", len(rec.reqs))
	}
}

func TestDefaultGCPOrgDepsDirectModeUsesADCCollector(t *testing.T) {
	deps := defaultGCPOrgDeps(gcpOrgConfig())
	c, err := deps.collectorFor(context.Background(), "p", nil)
	if err != nil {
		t.Fatalf("collectorFor() error = %v", err)
	}
	if _, ok := c.(*gcp.Collector); !ok || c.Provider() != "gcp" {
		t.Fatalf("collector = %T", c)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `nix develop --command go test ./cmd/cloudpam-agent/ -run 'GCPOrg' -v`
Expected: FAIL to compile with `undefined: gcpOrgDeps`.

- [ ] **Step 3: Implement**

Create `cmd/cloudpam-agent/gcp_org.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"golang.org/x/oauth2/google"

	"cloudpam/internal/discovery"
	"cloudpam/internal/discovery/gcp"
	"cloudpam/internal/domain"
)

// gcpOrgDeps are the cloud calls a GCP org pass makes, injectable for tests.
type gcpOrgDeps struct {
	listProjects func(ctx context.Context, parent string, limiter *discovery.Limiter) ([]gcp.Project, error)
	collectorFor func(ctx context.Context, project string, limiter *discovery.Limiter) (discovery.Collector, error)
}

// defaultGCPOrgDeps lists projects and reads them as the agent's Application
// Default Credentials. With an impersonation template set, each project is read
// through the service account the template names instead.
func defaultGCPOrgDeps(cfg *Config) gcpOrgDeps {
	template := cfg.GCPOrg.ImpersonateServiceAccount
	var iamClient *http.Client
	return gcpOrgDeps{
		listProjects: func(ctx context.Context, parent string, limiter *discovery.Limiter) ([]gcp.Project, error) {
			client, err := google.DefaultClient(ctx, gcp.ResourceManagerScope)
			if err != nil {
				return nil, fmt.Errorf("gcp default credentials: %w", err)
			}
			return gcp.ListProjects(ctx, client, parent, limiter)
		},
		collectorFor: func(ctx context.Context, project string, limiter *discovery.Limiter) (discovery.Collector, error) {
			if template == "" {
				c := gcp.New()
				c.SetLimiter(limiter)
				return c, nil
			}
			if iamClient == nil {
				client, err := google.DefaultClient(ctx, gcp.IAMCredentialsScope)
				if err != nil {
					return nil, fmt.Errorf("gcp default credentials: %w", err)
				}
				iamClient = client
			}
			ts := gcp.ImpersonatedTokenSource(ctx, iamClient, gcp.ServiceAccountFor(template, project), limiter)
			c := gcp.NewWithTokenSource(ts)
			c.SetLimiter(limiter)
			return c, nil
		},
	}
}

// runGCPOrgSync discovers every active project under the configured parent and
// bulk-ingests them. A project that cannot be read is skipped; a listing
// failure pushes nothing, so the next tick retries the whole pass.
func runGCPOrgSync(ctx context.Context, cfg *Config, pusher *Pusher, syncJobID *uuid.UUID, logger *slog.Logger, deps gcpOrgDeps) {
	parent := cfg.GCPOrg.Parent
	logger.Info("starting gcp org discovery sync", "parent", parent,
		"impersonation", cfg.GCPOrg.ImpersonateServiceAccount != "")

	// One limiter for the pass: Resource Manager, IAM Credentials and every
	// project's Compute calls draw from the same budget.
	limiter := discovery.NewLimiter(cfg.DiscoveryGCPRPS)

	projects, err := deps.listProjects(ctx, parent, limiter)
	if err != nil {
		logger.Error("failed to list gcp projects", "parent", parent, "error", err)
		return
	}

	exclude := make(map[string]bool, len(cfg.GCPOrg.ExcludeProjects))
	for _, id := range cfg.GCPOrg.ExcludeProjects {
		exclude[id] = true
	}
	regions := cfg.GCPOrg.Regions
	if len(regions) == 0 {
		regions = cfg.GCPRegions
	}

	var entries []domain.OrgAccountIngest
	for _, project := range projects {
		if exclude[project.ID] {
			logger.Info("excluding project", "project", project.ID)
			continue
		}
		collector, err := deps.collectorFor(ctx, project.ID, limiter)
		if err != nil {
			logger.Error("gcp credentials failed for project", "project", project.ID, "error", err)
			continue
		}
		account := domain.Account{Provider: providerGCP, ExternalID: project.ID, Regions: regions}
		resources, err := collector.Discover(ctx, account)
		if err != nil {
			// Partial results are dropped: pushing them would mark the missing
			// resources stale.
			logger.Error("discovery failed for project", "project", project.ID, "error", err)
			continue
		}
		logger.Info("discovered resources for project", "project", project.ID, "count", len(resources))

		name := project.Name
		if name == "" {
			name = project.ID
		}
		entries = append(entries, domain.OrgAccountIngest{
			Provider:    providerGCP,
			ExternalID:  project.ID,
			AccountName: name,
			Regions:     regions,
			Resources:   resources,
		})
	}

	if len(entries) == 0 {
		logger.Info("no gcp projects to ingest", "parent", parent)
		return
	}

	req := domain.BulkIngestRequest{Accounts: entries, SyncJobID: syncJobID}
	if err := pusher.PushOrgResources(ctx, req, cfg.MaxRetries, cfg.RetryBackoff); err != nil {
		logger.Error("push gcp org resources failed", "error", err)
		return
	}
	logger.Info("gcp org sync completed", "projects", len(entries))
}
```

In `cmd/cloudpam-agent/main.go` `runScheduler`, replace

```go
	if cfg.AWSOrg.Enabled {
		logger.Info("org mode enabled", "role_name", cfg.AWSOrg.RoleName, "regions", cfg.AWSOrg.Regions)
		syncFn = func(syncJobID *uuid.UUID) { runAWSOrgSync(ctx, cfg, pusher, syncJobID, logger) }
	}
```

with

```go
	switch {
	case cfg.AWSOrg.Enabled:
		logger.Info("aws org mode enabled", "role_name", cfg.AWSOrg.RoleName, "regions", cfg.AWSOrg.Regions)
		syncFn = func(syncJobID *uuid.UUID) { runAWSOrgSync(ctx, cfg, pusher, syncJobID, logger) }
	case cfg.GCPOrg.Enabled:
		logger.Info("gcp org mode enabled", "parent", cfg.GCPOrg.Parent, "regions", cfg.GCPOrg.Regions)
		syncFn = func(syncJobID *uuid.UUID) {
			runGCPOrgSync(ctx, cfg, pusher, syncJobID, logger, defaultGCPOrgDeps(cfg))
		}
	}
```

(The `iamClient` cache in `defaultGCPOrgDeps` therefore lives for one pass, so a rotated key file is picked up on the next tick.)

- [ ] **Step 4: Run tests**

Run: `nix develop --command go test ./cmd/cloudpam-agent/ -v 2>&1 | tail -30`
Expected: PASS, including the 5 new tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/cloudpam-agent/
git commit -m "feat(agent): discover every project under a gcp organization or folder (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Server: provider-neutral org ingest

**Files:**
- Modify: `internal/api/discovery_handlers.go` (`handleOrgIngest`, new helpers)
- Create: `internal/api/org_ingest_provider_test.go`
- Modify: `internal/api/discovery_cov_test.go` (`TestDiscoveryOrgIngestCov` fixtures)
- Modify: `internal/api/openapi.go:1112`

**Interfaces:**
- Consumes: `domain.OrgAccountIngest.ExternalID` (Task 4); `d.srv.store.GetAccountByKey(ctx, key) (*domain.Account, error)`, `d.srv.store.ListAccounts(ctx) ([]domain.Account, error)`, `d.srv.store.CreateAccount`; `storage.ErrNotFound`.
- Produces:
  - `func orgIngestExternalID(a domain.OrgAccountIngest) (string, error)`
  - `func (d *DiscoveryServer) resolveOrgAccount(ctx context.Context, provider, externalID string) (*domain.Account, error)` returning `(nil, nil)` when no account matches

- [ ] **Step 1: Write the failing tests**

Create `internal/api/org_ingest_provider_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cloudpam/internal/domain"
)

func postOrgIngest(t *testing.T, discSrv *DiscoveryServer, body string) domain.BulkIngestResponse {
	t.Helper()
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body, http.StatusOK)
	var resp domain.BulkIngestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestOrgIngestCreatesGCPProjectAccounts(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()

	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"gcp","external_id":"my-project","account_name":"My Project","regions":["us-central1"],
		"resources":[{"provider":"gcp","region":"global","resource_type":"vpc","resource_id":"net-1","name":"default","cidr":"10.0.0.0/16","status":"active"}]}]}`)
	if resp.AccountsCreated != 1 || resp.AccountsProcessed != 1 || resp.TotalResources != 1 || len(resp.Errors) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
	acct, err := st.GetAccountByKey(t.Context(), "gcp:my-project")
	if err != nil {
		t.Fatalf("get gcp account: %v", err)
	}
	if acct.Provider != "gcp" || acct.ExternalID != "my-project" || acct.Name != "My Project" {
		t.Fatalf("account = %+v", acct)
	}
}

func TestOrgIngestAcceptsTheLegacyAWSField(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()

	resp := postOrgIngest(t, discSrv, `{"accounts":[
		{"provider":"aws","aws_account_id":"111111111111"},
		{"provider":"aws","aws_account_id":"222222222222","external_id":"222222222222"}
	]}`)
	if resp.AccountsCreated != 2 || len(resp.Errors) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
	for _, key := range []string{"aws:111111111111", "aws:222222222222"} {
		if _, err := st.GetAccountByKey(t.Context(), key); err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
	}
}

func TestOrgIngestRejectsBadEntries(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	resp := postOrgIngest(t, discSrv, `{"accounts":[
		{"external_id":"no-provider"},
		{"provider":"azure","external_id":"sub-1"},
		{"provider":"aws","aws_account_id":"111111111111","external_id":"999999999999"},
		{"provider":"gcp"}
	]}`)
	if resp.AccountsProcessed != 0 || resp.AccountsCreated != 0 {
		t.Fatalf("resp = %+v, want nothing processed", resp)
	}
	want := []string{
		"skipped account: provider is required",
		`skipped account: unsupported provider "azure"`,
		`skipped account: external_id "999999999999" and aws_account_id "111111111111" disagree`,
		"skipped account: external_id is required",
	}
	if len(resp.Errors) != len(want) {
		t.Fatalf("errors = %#v", resp.Errors)
	}
	for i, w := range want {
		if resp.Errors[i] != w {
			t.Errorf("errors[%d] = %q, want %q", i, resp.Errors[i], w)
		}
	}
}

func TestOrgIngestMatchesAnExistingAccountByExternalID(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	existing, err := st.CreateAccount(t.Context(), domain.CreateAccount{
		Key: "prod-net", Name: "Prod network", Provider: "gcp", ExternalID: "my-project",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"gcp","external_id":"my-project",
		"resources":[{"provider":"gcp","region":"global","resource_type":"vpc","resource_id":"net-1","name":"default","cidr":"10.0.0.0/16","status":"active"}]}]}`)
	if resp.AccountsCreated != 0 || resp.AccountsProcessed != 1 || len(resp.Errors) != 0 {
		t.Fatalf("resp = %+v, want the existing account reused", resp)
	}
	if _, err := st.GetAccountByKey(t.Context(), "gcp:my-project"); err == nil {
		t.Fatal("a duplicate gcp:my-project account was created")
	}
	accounts, err := st.ListAccounts(t.Context())
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != existing.ID {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func TestOrgIngestRefusesAnAmbiguousMatch(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	for _, key := range []string{"one", "two"} {
		if _, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: key, Name: key, Provider: "aws", ExternalID: "123456789012"}); err != nil {
			t.Fatalf("create %s: %v", key, err)
		}
	}
	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"aws","external_id":"123456789012"}]}`)
	if resp.AccountsProcessed != 0 || len(resp.Errors) != 1 || !strings.HasPrefix(resp.Errors[0], "skipped account: ambiguous accounts for aws 123456789012: ids ") {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestOrgIngestIgnoresAnotherProvidersExternalID(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	if _, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws-thing", Name: "x", Provider: "aws", ExternalID: "shared-id"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"gcp","external_id":"shared-id"}]}`)
	if resp.AccountsCreated != 1 {
		t.Fatalf("resp = %+v, want a new gcp account", resp)
	}
}
```

If the memory store's `CreateAccount` refuses two accounts with the same `ExternalID`, change `TestOrgIngestRefusesAnAmbiguousMatch` to create the accounts with different external IDs and then update one with `st.UpdateAccount` to the shared ID; check `internal/storage/store.go` first.

- [ ] **Step 2: Run to verify failure**

Run: `nix develop --command go test ./internal/api/ -run 'OrgIngest' -v 2>&1 | tail -30`
Expected: FAIL: the GCP entry is keyed `aws:` (or skipped with `empty aws_account_id`), and the error strings do not match.

- [ ] **Step 3: Implement**

In `internal/api/discovery_handlers.go`:

1. Update the doc comment of `handleOrgIngest` to:

```go
// handleOrgIngest handles POST /api/v1/discovery/ingest/org
// Accepts discovered resources from many cloud accounts at once (AWS
// Organizations member accounts, GCP projects under an organization or folder)
// and auto-creates CloudPAM Account records for accounts it has not seen.
```

2. Replace the per-entry prologue, from `for _, orgAcct := range req.Accounts {` through the closing brace of the `if err != nil { ... auto-create ... }` block, with:

```go
	for _, orgAcct := range req.Accounts {
		provider := strings.TrimSpace(orgAcct.Provider)
		if provider == "" {
			resp.Errors = append(resp.Errors, "skipped account: provider is required")
			continue
		}
		if !orgIngestProviders[provider] {
			resp.Errors = append(resp.Errors, fmt.Sprintf("skipped account: unsupported provider %q", provider))
			continue
		}
		externalID, err := orgIngestExternalID(orgAcct)
		if err != nil {
			resp.Errors = append(resp.Errors, "skipped account: "+err.Error())
			continue
		}
		key := provider + ":" + externalID

		account, err := d.resolveOrgAccount(r.Context(), provider, externalID)
		if err != nil {
			resp.Errors = append(resp.Errors, "skipped account: "+err.Error())
			continue
		}
		if account == nil {
			name := orgAcct.AccountName
			if name == "" {
				name = externalID
			}
			created, createErr := d.srv.store.CreateAccount(r.Context(), domain.CreateAccount{
				Key:        key,
				Name:       name,
				Provider:   provider,
				ExternalID: externalID,
				Regions:    orgAcct.Regions,
			})
			if createErr != nil {
				resp.Errors = append(resp.Errors, "create account "+key+": "+createErr.Error())
				continue
			}
			account = &created
			resp.AccountsCreated++
		}
```

   The rest of the loop body (setting `AccountID` on resources, `ProcessResources`, `recordSyncAgent`, counters) is unchanged and still uses `key` in its error messages.

3. Add below `handleOrgIngest`:

```go
// orgIngestProviders are the providers org ingest accepts.
var orgIngestProviders = map[string]bool{"aws": true, "gcp": true}

// orgIngestExternalID returns the entry's account identifier. external_id is the
// field; aws_account_id is its deprecated spelling from agents before 0.25.0.
func orgIngestExternalID(a domain.OrgAccountIngest) (string, error) {
	ext := strings.TrimSpace(a.ExternalID)
	legacy := strings.TrimSpace(a.AWSAccountID)
	switch {
	case ext != "" && legacy != "" && ext != legacy:
		return "", fmt.Errorf("external_id %q and aws_account_id %q disagree", ext, legacy)
	case ext != "":
		return ext, nil
	case legacy != "":
		return legacy, nil
	}
	return "", errors.New("external_id is required")
}

// resolveOrgAccount finds the account an org-ingest entry belongs to: the
// <provider>:<external_id> key first, then a single account with the same
// provider and external ID (one an operator created by hand). It returns nil
// when neither exists, and an error when the fallback is ambiguous.
func (d *DiscoveryServer) resolveOrgAccount(ctx context.Context, provider, externalID string) (*domain.Account, error) {
	account, err := d.srv.store.GetAccountByKey(ctx, provider+":"+externalID)
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("lookup %s:%s: %w", provider, externalID, err)
	}
	accounts, err := d.srv.store.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	var matches []domain.Account
	for _, a := range accounts {
		if a.Provider == provider && a.ExternalID == externalID {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = strconv.FormatInt(m.ID, 10)
	}
	return nil, fmt.Errorf("ambiguous accounts for %s %s: ids %s", provider, externalID, strings.Join(ids, ", "))
}
```

   Add `fmt` and `strconv` to the imports if missing.

4. In `internal/api/openapi.go` line 1112, replace the route entry with:

```go
		{Method: "POST", Path: "/api/v1/discovery/ingest/org", Summary: "Bulk-ingest discovered resources for many cloud accounts", Description: "Each entry needs provider (aws or gcp) and external_id (AWS account ID or GCP project ID); aws_account_id is a deprecated alias of external_id. An entry is matched to the account keyed <provider>:<external_id>, else to the single account with that provider and external_id, else a new account is created. Entries that fail are reported in errors and skipped.", Tag: "Discovery", RequestSchema: "BulkIngestRequest", ResponseSchema: "BulkIngestResponse"},
```

- [ ] **Step 4: Update existing fixtures**

In `internal/api/discovery_cov_test.go` `TestDiscoveryOrgIngestCov`:
- `auto creates accounts and records errors`: change the body to

```go
		body := `{"accounts":[
			{"provider":"aws","aws_account_id":"111111111111","account_name":"prod","regions":["us-east-1"],
			 "resources":[{"provider":"aws","region":"us-east-1","resource_type":"vpc","resource_id":"vpc-1","name":"v","cidr":"10.0.0.0/16","status":"active"}]},
			{"provider":"aws","aws_account_id":""}
		]}`
```

  and the error assertion to `strings.Contains(resp.Errors[0], "external_id is required")` with message `"expected an external_id-required error"`.
- `marks supplied sync job failed when errors occur`: body `{"accounts":[{"provider":"aws","aws_account_id":""}],"sync_job_id":"...` and assert `strings.Contains(got.ErrorMessage, "external_id is required")`.
- `refreshes agent heartbeat` and `unknown agent id is tolerated`: add `"provider":"aws",` to the account entry.

Then run `grep -rn 'ingest/org' internal/ cmd/ --include=*_test.go` and give any other entry without `"provider"` a provider.

- [ ] **Step 5: Run tests**

Run: `nix develop --command go test ./internal/api/ -v -run 'OrgIngest|OpenAPI' 2>&1 | tail -30`
Expected: PASS.

Run: `nix develop --command just openapi-validate`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/ internal/domain/
git commit -m "feat(api): make org ingest provider-neutral with external_id matching (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Helm agent chart: required provider and GCP values

**Files:**
- Modify: `deploy/helm/cloudpam-agent/values.yaml`, `templates/configmap.yaml`, `templates/deployment.yaml`, `ci/full-values.yaml`
- Create: `deploy/helm/cloudpam-agent/ci/gcp-org-values.yaml`
- Modify: `scripts/helm-check.sh`

**Interfaces:**
- Consumes: agent YAML keys from Task 3 (`provider`, `gcp_project`, `gcp_regions`, `discovery_gcp_rps`, `gcp_org.{enabled,parent,regions,exclude_projects,impersonate_service_account}`).
- Produces: chart values `config.provider`, `config.gcpProject`, `config.gcpRegions`, `config.discoveryGcpRps`, `config.gcpOrg.*`, `gcpCredentials.existingSecret`.

- [ ] **Step 1: Add the failing check**

In `scripts/helm-check.sh`:
- change the `agent-explicit` render to add `--set config.provider=aws`;
- add after the `agent-full` render:

```bash
helm lint "$AGENT" -f "$AGENT/ci/gcp-org-values.yaml"
render cloudpam-agent "$AGENT" agent-gcp-org -f "$AGENT/ci/gcp-org-values.yaml"
grep -q 'provider: "gcp"' "$OUT/agent-gcp-org-configmap.yaml" || fail "gcp chart does not render provider gcp"
grep -q 'parent: "organizations/123456789"' "$OUT/agent-gcp-org-configmap.yaml" || fail "gcp chart does not render gcp_org.parent"
grep -q 'impersonate_service_account: "cloudpam-discovery@{project}.iam.gserviceaccount.com"' "$OUT/agent-gcp-org-configmap.yaml" ||
	fail "gcp chart does not render the impersonation template"
want_once GOOGLE_APPLICATION_CREDENTIALS "$OUT/agent-gcp-org-deployment.yaml"
want_absent GOOGLE_APPLICATION_CREDENTIALS "$OUT/agent-full-deployment.yaml"
```

- change the existing missing-name check's command to `helm template cloudpam-agent "$AGENT" --set apiKey=x --set config.provider=aws` (so it still fails on the name, not the provider), and add after it:

```bash
if helm template cloudpam-agent "$AGENT" --set apiKey=x --set config.agentName=a >"$OUT/agent-missing-provider.txt" 2>&1; then
	fail "agent chart renders without required config.provider"
elif ! grep -q 'config.provider is required (aws or gcp)' "$OUT/agent-missing-provider.txt"; then
	fail "agent chart does not explain that config.provider is required"
fi
if helm template cloudpam-agent "$AGENT" --set apiKey=x --set config.agentName=a --set config.provider=azure >"$OUT/agent-bad-provider.txt" 2>&1; then
	fail "agent chart renders with an unsupported config.provider"
fi
```

Create `deploy/helm/cloudpam-agent/ci/gcp-org-values.yaml`:

```yaml
apiKey: ci-placeholder
config:
  serverUrl: https://cloudpam.example.com
  accountId: 8
  agentName: ci-gcp-org-agent
  provider: gcp
  gcpRegions: [us-central1]
  discoveryGcpRps: 10
  gcpOrg:
    enabled: true
    parent: organizations/123456789
    excludeProjects: [sandbox-project]
    impersonateServiceAccount: "cloudpam-discovery@{project}.iam.gserviceaccount.com"
gcpCredentials:
  existingSecret: cloudpam-agent-gcp-key
serviceAccount:
  annotations:
    iam.gke.io/gcp-service-account: cloudpam-agent@ops-project.iam.gserviceaccount.com
```

Add `provider: aws` under `config:` in `ci/full-values.yaml`.

Run: `nix develop --command ./scripts/helm-check.sh`
Expected: FAIL (`gcp chart does not render provider gcp`, missing-provider check renders successfully).

- [ ] **Step 2: Implement values**

In `deploy/helm/cloudpam-agent/values.yaml`, under `config:` directly after `agentName: ""` add:

```yaml
  # Cloud provider this agent discovers (required): aws or gcp. There is no
  # default; one agent serves one provider.
  provider: ""
```

and after the `awsOrg:` block add:

```yaml
  # GCP project to discover in single-project mode (required when provider is
  # gcp and gcpOrg is disabled).
  gcpProject: ""

  # GCP regions to keep (empty = all regions). Networks are global.
  gcpRegions: []

  # Cap on GCP API calls per second (Compute, Resource Manager, IAM
  # Credentials), shared by every project in an org pass. 0 is unlimited.
  discoveryGcpRps: 0

  # GCP organization/folder mode: discover every active project under parent.
  gcpOrg:
    enabled: false
    parent: ""                     # organizations/<id> or folders/<id>
    regions: []                    # falls back to gcpRegions
    excludeProjects: []            # project IDs
    # Empty: read every project as the agent's own identity (grant
    # roles/browser + roles/compute.networkViewer on the parent).
    # Set: impersonate this service account per project; {project} is replaced
    # by each project ID.
    impersonateServiceAccount: ""

# GCP service-account key, for clusters without Workload Identity. The secret
# must hold the key JSON under "key.json"; it is mounted read-only and
# GOOGLE_APPLICATION_CREDENTIALS points at it.
gcpCredentials:
  existingSecret: ""
```

Under `serviceAccount.annotations`, below the IRSA example, add:

```yaml
  # Example for GKE Workload Identity:
  # annotations:
  #   iam.gke.io/gcp-service-account: cloudpam-agent@my-project.iam.gserviceaccount.com
```

- [ ] **Step 3: Implement templates**

In `templates/configmap.yaml`:
- add as the second line:

```yaml
{{- $provider := required "config.provider is required (aws or gcp)" (.Values.config.provider | trim) -}}
{{- if not (has $provider (list "aws" "gcp")) }}{{ fail (printf "config.provider is required (aws or gcp), got %q" $provider) }}{{ end -}}
```

- after `agent_name:` add `    provider: {{ $provider | quote }}`;
- after the `aws_org` block's `{{- end }}` add:

```yaml
    {{- with .Values.config.gcpProject }}
    gcp_project: {{ . | quote }}
    {{- end }}
    {{- with .Values.config.gcpRegions }}
    gcp_regions:
      {{- range . }}
      - {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- with .Values.config.discoveryGcpRps }}
    discovery_gcp_rps: {{ . }}
    {{- end }}
    {{- if .Values.config.gcpOrg.enabled }}
    gcp_org:
      enabled: true
      parent: {{ required "config.gcpOrg.parent is required when config.gcpOrg.enabled" .Values.config.gcpOrg.parent | quote }}
      {{- with .Values.config.gcpOrg.regions }}
      regions:
        {{- range . }}
        - {{ . | quote }}
        {{- end }}
      {{- end }}
      {{- with .Values.config.gcpOrg.excludeProjects }}
      exclude_projects:
        {{- range . }}
        - {{ . | quote }}
        {{- end }}
      {{- end }}
      {{- with .Values.config.gcpOrg.impersonateServiceAccount }}
      impersonate_service_account: {{ . | quote }}
      {{- end }}
    {{- end }}
```

In `templates/deployment.yaml`:
- in `env:` after the `CLOUDPAM_API_KEY` entry add:

```yaml
          {{- if .Values.gcpCredentials.existingSecret }}
          - name: GOOGLE_APPLICATION_CREDENTIALS
            value: /var/run/secrets/cloudpam/gcp/key.json
          {{- end }}
```

- in `volumeMounts:` after the config mount add:

```yaml
          {{- if .Values.gcpCredentials.existingSecret }}
          - name: gcp-credentials
            mountPath: /var/run/secrets/cloudpam/gcp
            readOnly: true
          {{- end }}
```

- in `volumes:` after the config volume add:

```yaml
        {{- if .Values.gcpCredentials.existingSecret }}
        - name: gcp-credentials
          secret:
            secretName: {{ .Values.gcpCredentials.existingSecret }}
            items:
              - key: key.json
                path: key.json
        {{- end }}
```

- [ ] **Step 4: Run the check**

Run: `nix develop --command ./scripts/helm-check.sh`
Expected: exits 0 except for the appVersion check (fixed in Task 10). If the appVersion check is the only failure, proceed.

- [ ] **Step 5: Commit**

```bash
git add deploy/helm/cloudpam-agent scripts/helm-check.sh
git commit -m "feat(helm): require the agent provider and add gcp project and org values (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Wizard: extract config builders (AWS, with provider)

**Files:**
- Create: `ui/src/components/discoveryWizardConfig.ts`
- Create: `ui/src/__tests__/discoveryWizardConfig.test.ts`
- Modify: `ui/src/components/DiscoveryWizard.tsx` (remove inline strings, call builders)

**Interfaces:**
- Consumes: `ui/src/utils/escape.ts` helpers.
- Produces (used by Task 9):

```ts
export type WizardProvider = 'aws' | 'gcp'
export type DiscoveryMode = 'single' | 'organization'
export type ConfigTab = 'shell' | 'yaml' | 'terraform' | 'docker' | 'iam'
export interface AwsOrgInput { roleName: string; externalId: string; regions: string[]; exclude: string[] }
export interface GcpOrgInput { parent: string; regions: string[]; exclude: string[]; impersonateServiceAccount: string }
export interface AgentConfigInput {
  provider: WizardProvider; mode: DiscoveryMode; token: string; serverUrl: string; agentName: string
  accountId: number; accountRegions: string[]; awsOrg: AwsOrgInput; gcpProject: string; gcpOrg: GcpOrgInput
}
export function resolveConfigTab(selected: ConfigTab, available: ConfigTab[]): ConfigTab
export function availableTabs(provider: WizardProvider, mode: DiscoveryMode): ConfigTab[]
export function buildAgentConfigs(input: AgentConfigInput): Partial<Record<ConfigTab, string>>
export function splitList(value: string): string[]
```

- [ ] **Step 1: Write the failing tests**

Create `ui/src/__tests__/discoveryWizardConfig.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import {
  availableTabs,
  buildAgentConfigs,
  splitList,
  type AgentConfigInput,
} from '../components/discoveryWizardConfig'

function awsInput(overrides: Partial<AgentConfigInput> = {}): AgentConfigInput {
  return {
    provider: 'aws',
    mode: 'single',
    token: 'tok',
    serverUrl: 'https://pam.example.com',
    agentName: 'prod-agent',
    accountId: 7,
    accountRegions: ['us-east-1', 'us-west-2'],
    awsOrg: { roleName: 'CloudPAMDiscoveryRole', externalId: '', regions: ['us-east-1'], exclude: [] },
    gcpProject: '',
    gcpOrg: { parent: '', regions: [], exclude: [], impersonateServiceAccount: '' },
    ...overrides,
  }
}

describe('splitList', () => {
  it('trims and drops empty entries', () => {
    expect(splitList(' a, ,b ,')).toEqual(['a', 'b'])
  })
})

describe('availableTabs for aws', () => {
  it('offers iam only in org mode', () => {
    expect(availableTabs('aws', 'single')).toEqual(['shell', 'yaml', 'terraform', 'docker'])
    expect(availableTabs('aws', 'organization')).toEqual(['shell', 'yaml', 'terraform', 'docker', 'iam'])
  })
})

describe('buildAgentConfigs for aws', () => {
  it('always states the provider', () => {
    for (const mode of ['single', 'organization'] as const) {
      const c = buildAgentConfigs(awsInput({ mode }))
      expect(c.shell).toContain("export CLOUDPAM_PROVIDER='aws'")
      expect(c.docker).toContain("-e CLOUDPAM_PROVIDER='aws'")
      expect(c.terraform).toContain("CLOUDPAM_PROVIDER='aws'")
      expect(c.yaml).toContain('provider: "aws"')
    }
  })

  it('renders single-account settings', () => {
    const c = buildAgentConfigs(awsInput())
    expect(c.shell).toContain('export CLOUDPAM_ACCOUNT_ID=7')
    expect(c.shell).toContain("export CLOUDPAM_AWS_REGIONS='us-east-1,us-west-2'")
    expect(c.yaml).toContain('aws_regions: ["us-east-1", "us-west-2"]')
    expect(c.iam).toBeUndefined()
  })

  it('renders org settings and the iam snippet', () => {
    const c = buildAgentConfigs(awsInput({
      mode: 'organization',
      awsOrg: { roleName: 'Role', externalId: 'ext', regions: ['eu-west-1'], exclude: ['111'] },
    }))
    expect(c.shell).toContain('export CLOUDPAM_AWS_ORG_ENABLED=true')
    expect(c.shell).toContain("export CLOUDPAM_AWS_ORG_EXTERNAL_ID='ext'")
    expect(c.shell).toContain("export CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS='111'")
    expect(c.shell).toContain("export AWS_REGION='eu-west-1'")
    expect(c.yaml).toContain('aws_org:\n  enabled: true\n  role_name: "Role"')
    expect(c.iam).toContain('sts:AssumeRole')
    expect(c.iam).toContain('"sts:ExternalId" = "ext"')
  })

  it('quotes hostile values', () => {
    const c = buildAgentConfigs(awsInput({ token: `x'; rm -rf /; '` }))
    expect(c.shell).toContain(`export CLOUDPAM_BOOTSTRAP_TOKEN='x'\\''; rm -rf /; '\\'''`)
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd ui && nix develop --command npx vitest run src/__tests__/discoveryWizardConfig.test.ts`
Expected: FAIL with `Failed to resolve import "../components/discoveryWizardConfig"`.

- [ ] **Step 3: Implement the builders**

Create `ui/src/components/discoveryWizardConfig.ts`. Every rendered format comes from one ordered list of environment variables per provider and mode, so shell, Docker and Terraform cannot drift apart:

```ts
import { hclHeredocShellQuote, hclIdent, hclQuote, shellQuote, yamlQuote } from '../utils/escape'

export type WizardProvider = 'aws' | 'gcp'
export type DiscoveryMode = 'single' | 'organization'
export type ConfigTab = 'shell' | 'yaml' | 'terraform' | 'docker' | 'iam'

export interface AwsOrgInput {
  roleName: string
  externalId: string
  regions: string[]
  exclude: string[]
}

export interface GcpOrgInput {
  parent: string
  regions: string[]
  exclude: string[]
  impersonateServiceAccount: string
}

export interface AgentConfigInput {
  provider: WizardProvider
  mode: DiscoveryMode
  token: string
  serverUrl: string
  agentName: string
  accountId: number
  accountRegions: string[]
  awsOrg: AwsOrgInput
  gcpProject: string
  gcpOrg: GcpOrgInput
}

const AGENT_ID_FILE = '/var/lib/cloudpam-agent/agent-id'
const AGENT_IMAGE = 'ghcr.io/badgerops/cloudpam/agent:latest'

/**
 * Resolves which config tab to render. The selection is kept in state, but the
 * set of available tabs depends on provider and mode, so a stale selection
 * must fall back instead of rendering a tab the strip no longer offers.
 */
export function resolveConfigTab(selected: ConfigTab, available: ConfigTab[]): ConfigTab {
  return available.includes(selected) ? selected : available[0]
}

export function availableTabs(provider: WizardProvider, mode: DiscoveryMode): ConfigTab[] {
  if (provider === 'gcp') return ['shell', 'yaml', 'docker', 'iam']
  return mode === 'organization'
    ? ['shell', 'yaml', 'terraform', 'docker', 'iam']
    : ['shell', 'yaml', 'terraform', 'docker']
}

export function splitList(value: string): string[] {
  return value.split(',').map(v => v.trim()).filter(Boolean)
}

/** A literal number is rendered bare; everything else is quoted per format. */
type EnvValue = string | number | true
type EnvVar = [name: string, value: EnvValue]

function envVars(input: AgentConfigInput): EnvVar[] {
  const vars: EnvVar[] = [
    ['CLOUDPAM_BOOTSTRAP_TOKEN', input.token],
    ['CLOUDPAM_AGENT_ID_FILE', AGENT_ID_FILE],
    ['CLOUDPAM_ACCOUNT_ID', input.accountId],
    ['CLOUDPAM_PROVIDER', input.provider],
  ]
  return vars.concat(input.provider === 'gcp' ? gcpEnvVars(input) : awsEnvVars(input))
}

function awsEnvVars(input: AgentConfigInput): EnvVar[] {
  if (input.mode === 'single') {
    return [['CLOUDPAM_AWS_REGIONS', input.accountRegions.join(',')]]
  }
  const org = input.awsOrg
  const primary = org.regions[0] ?? 'us-east-1'
  const vars: EnvVar[] = [
    ['AWS_REGION', primary],
    ['AWS_DEFAULT_REGION', primary],
    ['CLOUDPAM_AWS_ORG_ENABLED', true],
    ['CLOUDPAM_AWS_ORG_ROLE_NAME', org.roleName],
  ]
  if (org.externalId) vars.push(['CLOUDPAM_AWS_ORG_EXTERNAL_ID', org.externalId])
  vars.push(['CLOUDPAM_AWS_ORG_REGIONS', org.regions.join(',')])
  if (org.exclude.length > 0) vars.push(['CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS', org.exclude.join(',')])
  return vars
}

// Task 9 replaces this with the real GCP variables.
function gcpEnvVars(_input: AgentConfigInput): EnvVar[] {
  return []
}

function renderShellValue(value: EnvValue, quote: (v: unknown) => string): string {
  if (value === true) return 'true'
  if (typeof value === 'number') return String(value)
  return quote(value)
}

function shellConfig(vars: EnvVar[]): string {
  const lines = vars.map(([name, value]) => `export ${name}=${renderShellValue(value, shellQuote)}`)
  return ['#!/bin/bash', ...lines, './cloudpam-agent'].join('\n')
}

function dockerConfig(vars: EnvVar[]): string {
  const lines = [
    'docker run -d \\',
    '  --name cloudpam-agent \\',
    '  -v /var/lib/cloudpam-agent:/var/lib/cloudpam-agent \\',
    ...vars.map(([name, value]) => `  -e ${name}=${renderShellValue(value, shellQuote)} \\`),
    `  ${AGENT_IMAGE}`,
  ]
  return lines.join('\n')
}

function terraformConfig(input: AgentConfigInput, vars: EnvVar[]): string {
  const envLines = vars.map(([name, value]) => name === 'CLOUDPAM_BOOTSTRAP_TOKEN'
    ? `      CLOUDPAM_BOOTSTRAP_TOKEN=\${var.cloudpam_bootstrap_token} \\`
    : `      ${name}=${renderShellValue(value, hclHeredocShellQuote)} \\`)
  return `variable "cloudpam_bootstrap_token" {
  default   = ${hclQuote(input.token)}
  sensitive = true
}

resource "null_resource" "cloudpam_agent" {
  provisioner "local-exec" {
    command = <<-EOT
${envLines.join('\n')}
      ./cloudpam-agent
    EOT
  }
}`
}

function yamlList(values: string[]): string {
  return `[${values.map(yamlQuote).join(', ')}]`
}

function yamlConfig(input: AgentConfigInput): string {
  const lines = [
    `server_url: ${yamlQuote(input.serverUrl)}`,
    `bootstrap_token: ${yamlQuote(input.token)}`,
    `agent_id_file: ${yamlQuote(AGENT_ID_FILE)}`,
    `account_id: ${input.accountId}`,
    `agent_name: ${yamlQuote(input.agentName)}`,
    `provider: ${yamlQuote(input.provider)}`,
    'sync_interval: "15m"',
    'heartbeat_interval: "1m"',
  ]
  return lines.concat(input.provider === 'gcp' ? gcpYamlLines(input) : awsYamlLines(input)).join('\n')
}

function awsYamlLines(input: AgentConfigInput): string[] {
  if (input.mode === 'single') return [`aws_regions: ${yamlList(input.accountRegions)}`]
  const org = input.awsOrg
  const lines = ['aws_org:', '  enabled: true', `  role_name: ${yamlQuote(org.roleName)}`]
  if (org.externalId) lines.push(`  external_id: ${yamlQuote(org.externalId)}`)
  lines.push(`  regions: ${yamlList(org.regions)}`)
  if (org.exclude.length > 0) lines.push(`  exclude_accounts: ${yamlList(org.exclude)}`)
  return lines
}

// Task 9 replaces this with the real GCP YAML.
function gcpYamlLines(_input: AgentConfigInput): string[] {
  return []
}

function awsIamConfig(input: AgentConfigInput): string {
  const { roleName, externalId } = input.awsOrg
  const roleIdent = hclIdent(roleName, 'cloudpam_discovery_role')
  return `# IAM Setup for AWS Organizations Discovery
#
# 1. Deploy the member role to all accounts via CloudFormation StackSet
#    or Terraform (deploy/terraform/aws-org-discovery/member-role/)
#
# 2. Attach the management policy to the agent's IAM role
#    (deploy/terraform/aws-org-discovery/management-policy/)

# --- Member Account Role (Terraform) ---
# Deploy to each member account:

resource "aws_iam_role" "${roleIdent}" {
  name = ${hclQuote(roleName)}

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::<MANAGEMENT_ACCOUNT_ID>:root" }
      Action    = "sts:AssumeRole"${externalId ? `
      Condition = { StringEquals = { "sts:ExternalId" = ${hclQuote(externalId)} } }` : ''}
    }]
  })
}

resource "aws_iam_role_policy" "discovery" {
  role = aws_iam_role.${roleIdent}.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = [
        "ec2:DescribeVpcs",
        "ec2:DescribeSubnets",
        "ec2:DescribeAddresses"
      ]
      Resource = "*"
    }]
  })
}

# --- Management Account Policy ---
# Attach to the IAM role running the CloudPAM agent:

resource "aws_iam_policy" "cloudpam_org" {
  name = "CloudPAMOrgDiscoveryPolicy"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["organizations:ListAccounts", "organizations:DescribeOrganization"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = "sts:AssumeRole"
        Resource = ${hclQuote(`arn:aws:iam::*:role/${roleName}`)}
      }
    ]
  })
}`
}

// Task 9 replaces this with the gcloud commands.
function gcpIamConfig(_input: AgentConfigInput): string {
  return ''
}

/** Builds the content of every tab the provider and mode offer. */
export function buildAgentConfigs(input: AgentConfigInput): Partial<Record<ConfigTab, string>> {
  const vars = envVars(input)
  const out: Partial<Record<ConfigTab, string>> = {}
  for (const tab of availableTabs(input.provider, input.mode)) {
    switch (tab) {
      case 'shell': out.shell = shellConfig(vars); break
      case 'yaml': out.yaml = yamlConfig(input); break
      case 'docker': out.docker = dockerConfig(vars); break
      case 'terraform': out.terraform = terraformConfig(input, vars); break
      case 'iam': out.iam = input.provider === 'gcp' ? gcpIamConfig(input) : awsIamConfig(input); break
    }
  }
  return out
}
```

- [ ] **Step 4: Wire the component to the builders**

In `ui/src/components/DiscoveryWizard.tsx`:

1. Replace the local `ConfigTab`/`DiscoveryMode` types and the `resolveConfigTab` function (lines 17-29) with:

```ts
import {
  availableTabs as tabsFor,
  buildAgentConfigs,
  resolveConfigTab,
  splitList,
  type ConfigTab,
  type DiscoveryMode,
} from './discoveryWizardConfig'

export { resolveConfigTab, type ConfigTab }
```

   (The re-export keeps `ui/src/__tests__/discoveryWizardConfigTab.test.ts` importing from `../components/DiscoveryWizard` working.)
2. Remove `hclHeredocShellQuote`, `hclIdent`, `hclQuote`, `shellQuote`, `yamlQuote` from the `../utils/escape` import; keep `safeFilename`.
3. Delete everything from `const orgRegions = orgRegionsInput.split(...)` through the end of `const availableTabs: ConfigTab[] = ...` (the inline config strings, `orgRoleIdent`, `iamSetupContent`, the four `const xConfig = isOrg ? ...` lines and the tab list) and put in their place:

```ts
  const orgRegions = splitList(orgRegionsInput)
  const orgExclude = splitList(orgExcludeInput)

  const generated = buildAgentConfigs({
    provider: 'aws',
    mode: discoveryMode,
    token,
    serverUrl,
    agentName: agentNameFinal,
    accountId,
    accountRegions,
    awsOrg: { roleName: orgRoleName, externalId: orgExternalId, regions: orgRegions, exclude: orgExclude },
    gcpProject: '',
    gcpOrg: { parent: '', regions: [], exclude: [], impersonateServiceAccount: '' },
  })
  const availableTabs = tabsFor('aws', discoveryMode)
```

4. Replace the `configs` record with:

```ts
  const tabMeta: Record<ConfigTab, { filename: string; label: string }> = {
    shell: { filename: `${filenameStem}.sh`, label: 'Shell' },
    yaml: { filename: `${filenameStem}.yaml`, label: 'YAML' },
    terraform: { filename: `${filenameStem}.tf`, label: 'Terraform' },
    docker: { filename: `${filenameStem}-docker.sh`, label: 'Docker' },
    iam: { filename: `${filenameStem}-iam.tf`, label: 'IAM Setup' },
  }
  const configs = Object.fromEntries(
    availableTabs.map(tab => [tab, { content: generated[tab] ?? '', ...tabMeta[tab] }]),
  ) as Record<ConfigTab, { content: string; filename: string; label: string }>
```

   Check the JSX that reads `configs[...]` only indexes tabs from `availableTabs` / `activeConfigTab`; if it iterates `Object.keys(configs)` instead, switch it to `availableTabs`.

- [ ] **Step 5: Run tests and type check**

Run: `cd ui && nix develop --command npx vitest run src/__tests__/discoveryWizardConfig.test.ts src/__tests__/discoveryWizardConfigTab.test.ts`
Expected: PASS.

Run: `cd ui && nix develop --command npx tsc --noEmit`
Expected: no output. (`noUnusedParameters` may flag the `_input` placeholders; if so, the leading underscore is the repo's convention, so check `ui/tsconfig*.json` and `eslint` config and keep them as written, since Task 9 replaces them.)

- [ ] **Step 6: Commit**

```bash
git add ui/src/components/discoveryWizardConfig.ts ui/src/components/DiscoveryWizard.tsx ui/src/__tests__/discoveryWizardConfig.test.ts
git commit -m "refactor(ui): build discovery agent configs in pure functions and state the provider (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Wizard: GCP provider, single project and org

**Files:**
- Modify: `ui/src/components/discoveryWizardConfig.ts`
- Modify: `ui/src/__tests__/discoveryWizardConfig.test.ts`
- Modify: `ui/src/components/DiscoveryWizard.tsx`
- Create: `ui/src/__tests__/DiscoveryWizardProvider.test.tsx`

**Interfaces:**
- Consumes: Task 8 exports.
- Produces:
  - `export const GCP_PARENT_PATTERN = /^(organizations|folders)\/[0-9]+$/`
  - `export function gcpProjectFor(account: { external_id?: string; key: string } | null | undefined): string`
  - GCP branches of `buildAgentConfigs`

- [ ] **Step 1: Write the failing builder tests**

Append to `ui/src/__tests__/discoveryWizardConfig.test.ts` (and add `GCP_PARENT_PATTERN, gcpProjectFor` to its import):

```ts
function gcpInput(overrides: Partial<AgentConfigInput> = {}): AgentConfigInput {
  return awsInput({
    provider: 'gcp',
    gcpProject: 'my-project',
    accountRegions: ['us-central1'],
    gcpOrg: { parent: 'organizations/123', regions: ['us-central1'], exclude: ['sandbox'], impersonateServiceAccount: '' },
    ...overrides,
  })
}

describe('gcp helpers', () => {
  it('validates parents', () => {
    expect(GCP_PARENT_PATTERN.test('organizations/1')).toBe(true)
    expect(GCP_PARENT_PATTERN.test('folders/22')).toBe(true)
    expect(GCP_PARENT_PATTERN.test('projects/1')).toBe(false)
    expect(GCP_PARENT_PATTERN.test('folders/x')).toBe(false)
  })

  it('derives the project from external_id, then key', () => {
    expect(gcpProjectFor({ external_id: 'ext', key: 'gcp:k' })).toBe('ext')
    expect(gcpProjectFor({ external_id: '', key: 'gcp:k' })).toBe('k')
    expect(gcpProjectFor({ key: 'bare' })).toBe('bare')
    expect(gcpProjectFor(null)).toBe('')
  })
})

describe('availableTabs for gcp', () => {
  it('has no terraform tab and always an iam tab', () => {
    expect(availableTabs('gcp', 'single')).toEqual(['shell', 'yaml', 'docker', 'iam'])
    expect(availableTabs('gcp', 'organization')).toEqual(['shell', 'yaml', 'docker', 'iam'])
  })
})

describe('buildAgentConfigs for gcp', () => {
  it('renders single-project settings', () => {
    const c = buildAgentConfigs(gcpInput())
    expect(c.terraform).toBeUndefined()
    expect(c.shell).toContain("export CLOUDPAM_PROVIDER='gcp'")
    expect(c.shell).toContain("export CLOUDPAM_GCP_PROJECT='my-project'")
    expect(c.shell).toContain("export CLOUDPAM_GCP_REGIONS='us-central1'")
    expect(c.shell).not.toContain('AWS')
    expect(c.yaml).toContain('provider: "gcp"\n')
    expect(c.yaml).toContain('gcp_project: "my-project"')
    expect(c.docker).toContain('GOOGLE_APPLICATION_CREDENTIALS')
    expect(c.iam).toContain('gcloud projects add-iam-policy-binding \'my-project\'')
    expect(c.iam).toContain('roles/compute.networkViewer')
    expect(c.iam).not.toContain('roles/browser')
  })

  it('renders org settings with direct grants', () => {
    const c = buildAgentConfigs(gcpInput({ mode: 'organization' }))
    expect(c.shell).toContain('export CLOUDPAM_GCP_ORG_ENABLED=true')
    expect(c.shell).toContain("export CLOUDPAM_GCP_ORG_PARENT='organizations/123'")
    expect(c.shell).toContain("export CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS='sandbox'")
    expect(c.shell).not.toContain('CLOUDPAM_GCP_PROJECT=')
    expect(c.shell).not.toContain('IMPERSONATE')
    expect(c.yaml).toContain('gcp_org:\n  enabled: true\n  parent: "organizations/123"')
    expect(c.iam).toContain("gcloud organizations add-iam-policy-binding '123'")
    expect(c.iam).toContain('roles/browser')
    expect(c.iam).toContain('roles/compute.networkViewer')
    expect(c.iam).not.toContain('serviceAccountTokenCreator')
  })

  it('renders folder grants and impersonation', () => {
    const c = buildAgentConfigs(gcpInput({
      mode: 'organization',
      gcpOrg: { parent: 'folders/9', regions: [], exclude: [], impersonateServiceAccount: 'cloudpam-discovery@{project}.iam.gserviceaccount.com' },
    }))
    expect(c.shell).toContain("export CLOUDPAM_GCP_ORG_IMPERSONATE_SA='cloudpam-discovery@{project}.iam.gserviceaccount.com'")
    expect(c.yaml).toContain('  impersonate_service_account: "cloudpam-discovery@{project}.iam.gserviceaccount.com"')
    expect(c.iam).toContain("gcloud resource-manager folders add-iam-policy-binding '9'")
    expect(c.iam).toContain('roles/iam.serviceAccountTokenCreator')
    expect(c.iam).toContain('PROJECT_ID')
    // With impersonation the agent itself gets no compute role on the folder:
    // every networkViewer grant goes to the per-project service account.
    const viewerGrants = c.iam!.split('\n').filter(l => l.includes('compute.networkViewer'))
    expect(viewerGrants.length).toBeGreaterThan(0)
    expect(viewerGrants.every(l => l.includes('cloudpam-discovery@'))).toBe(true)
  })
})
```

Run: `cd ui && nix develop --command npx vitest run src/__tests__/discoveryWizardConfig.test.ts`
Expected: FAIL (`GCP_PARENT_PATTERN` not exported; GCP output empty).

- [ ] **Step 2: Implement the GCP builders**

In `ui/src/components/discoveryWizardConfig.ts`:

1. Add near the top:

```ts
export const GCP_PARENT_PATTERN = /^(organizations|folders)\/[0-9]+$/

const GCP_KEY_FILE = '/var/run/secrets/cloudpam/gcp/key.json'

/** The GCP project an account maps to: external ID, else key without "gcp:". */
export function gcpProjectFor(account: { external_id?: string; key: string } | null | undefined): string {
  if (!account) return ''
  if (account.external_id) return account.external_id
  return account.key.startsWith('gcp:') ? account.key.slice(4) : account.key
}
```

2. Replace the placeholder `gcpEnvVars` with:

```ts
function gcpEnvVars(input: AgentConfigInput): EnvVar[] {
  if (input.mode === 'single') {
    const vars: EnvVar[] = [['CLOUDPAM_GCP_PROJECT', input.gcpProject]]
    if (input.accountRegions.length > 0) vars.push(['CLOUDPAM_GCP_REGIONS', input.accountRegions.join(',')])
    return vars
  }
  const org = input.gcpOrg
  const vars: EnvVar[] = [
    ['CLOUDPAM_GCP_ORG_ENABLED', true],
    ['CLOUDPAM_GCP_ORG_PARENT', org.parent],
  ]
  if (org.regions.length > 0) vars.push(['CLOUDPAM_GCP_ORG_REGIONS', org.regions.join(',')])
  if (org.exclude.length > 0) vars.push(['CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS', org.exclude.join(',')])
  if (org.impersonateServiceAccount) vars.push(['CLOUDPAM_GCP_ORG_IMPERSONATE_SA', org.impersonateServiceAccount])
  return vars
}
```

3. In `dockerConfig`, give GCP a key mount. Change its signature to `dockerConfig(input: AgentConfigInput, vars: EnvVar[])`, update the call in `buildAgentConfigs` to `dockerConfig(input, vars)`, and build the lines as:

```ts
  const isGcp = input.provider === 'gcp'
  // The note goes above `docker run`: a comment line inside a
  // backslash-continued command would end the command.
  const note = isGcp
    ? ['# Omit the key mount and GOOGLE_APPLICATION_CREDENTIALS when the host has Application Default Credentials (GCE metadata, Workload Identity).']
    : []
  const gcpKey = isGcp
    ? [
        `  -v "$PWD/key.json:${GCP_KEY_FILE}:ro" \\`,
        `  -e GOOGLE_APPLICATION_CREDENTIALS=${shellQuote(GCP_KEY_FILE)} \\`,
      ]
    : []
  const lines = [
    ...note,
    'docker run -d \\',
    '  --name cloudpam-agent \\',
    '  -v /var/lib/cloudpam-agent:/var/lib/cloudpam-agent \\',
    ...gcpKey,
    ...vars.map(([name, value]) => `  -e ${name}=${renderShellValue(value, shellQuote)} \\`),
    `  ${AGENT_IMAGE}`,
  ]
```

4. Replace the placeholder `gcpYamlLines` with:

```ts
function gcpYamlLines(input: AgentConfigInput): string[] {
  if (input.mode === 'single') {
    const lines = [`gcp_project: ${yamlQuote(input.gcpProject)}`]
    if (input.accountRegions.length > 0) lines.push(`gcp_regions: ${yamlList(input.accountRegions)}`)
    return lines
  }
  const org = input.gcpOrg
  const lines = ['gcp_org:', '  enabled: true', `  parent: ${yamlQuote(org.parent)}`]
  if (org.regions.length > 0) lines.push(`  regions: ${yamlList(org.regions)}`)
  if (org.exclude.length > 0) lines.push(`  exclude_projects: ${yamlList(org.exclude)}`)
  if (org.impersonateServiceAccount) lines.push(`  impersonate_service_account: ${yamlQuote(org.impersonateServiceAccount)}`)
  return lines
}
```

5. Replace the placeholder `gcpIamConfig` with:

```ts
function gcpBindCommand(parent: string): string {
  const [kind, id] = parent.split('/')
  const group = kind === 'folders' ? 'resource-manager folders' : 'organizations'
  return `gcloud ${group} add-iam-policy-binding ${shellQuote(id ?? '')}`
}

function gcpIamConfig(input: AgentConfigInput): string {
  const header = `#!/bin/bash
# IAM setup for CloudPAM GCP discovery.
# Replace AGENT_SA_EMAIL with the service account the agent runs as
# (its Workload Identity service account, VM service account, or key's account).
AGENT_SA_EMAIL='cloudpam-agent@YOUR_PROJECT.iam.gserviceaccount.com'
`
  if (input.mode === 'single') {
    return `${header}
gcloud projects add-iam-policy-binding ${shellQuote(input.gcpProject)} \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/compute.networkViewer'`
  }

  const org = input.gcpOrg
  const bind = gcpBindCommand(org.parent)
  const listing = `# List projects under ${org.parent} (needed in every org mode):
${bind} \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/browser'`

  if (!org.impersonateServiceAccount) {
    return `${header}
${listing}

# Read every project's networks through inheritance:
${bind} \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/compute.networkViewer'`
  }

  const template = org.impersonateServiceAccount
  const accountExpr = template.replace(/\{project\}/g, '${PROJECT_ID}')
  const name = template.split('@')[0].replace(/\{project\}/g, '${PROJECT_ID}')
  return `${header}
${listing}

# Per project: create the discovery service account, let it read networks, and
# let the agent impersonate it. Run once per project ID.
PROJECT_ID='my-project'
gcloud iam service-accounts create "${name}" --project="\${PROJECT_ID}" \\
  --display-name='CloudPAM discovery'
gcloud projects add-iam-policy-binding "\${PROJECT_ID}" \\
  --member="serviceAccount:${accountExpr}" --role='roles/compute.networkViewer'
gcloud iam service-accounts add-iam-policy-binding "${accountExpr}" --project="\${PROJECT_ID}" \\
  --member="serviceAccount:\${AGENT_SA_EMAIL}" --role='roles/iam.serviceAccountTokenCreator'`
}
```

   Note: for a template without `{project}` (one shared service account) the per-project block still works: `accountExpr` is the fixed email; the `create` line then only needs running once. Add this sentence as a comment line when `!template.includes('{project}')`:
   `# The template names one shared service account: create it and grant token creator once, then grant roles/compute.networkViewer in each project.`

- [ ] **Step 3: Run builder tests**

Run: `cd ui && nix develop --command npx vitest run src/__tests__/discoveryWizardConfig.test.ts`
Expected: PASS.

- [ ] **Step 4: Write the failing component test**

Create `ui/src/__tests__/DiscoveryWizardProvider.test.tsx`:

```tsx
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import DiscoveryWizard from '../components/DiscoveryWizard'
import type { Account } from '../api/types'

vi.mock('../hooks/useDiscovery', () => ({
  useAgentProvisioning: () => ({ provision: vi.fn(), loading: false, error: null }),
  useDiscoveryAgents: () => ({ agents: [], fetch: vi.fn() }),
}))
vi.mock('../hooks/useAccounts', () => ({ useAccounts: () => ({ createAccount: vi.fn() }) }))
vi.mock('../hooks/useToast', () => ({ useToast: () => ({ showToast: vi.fn() }) }))
vi.mock('../components/ConnectivityBadge', () => ({ default: () => null }))

const accounts = [
  { id: 1, key: 'aws:111', name: 'AWS Prod', provider: 'aws', regions: [] },
  { id: 2, key: 'gcp:net-prod', name: 'GCP Net', provider: 'gcp', external_id: 'net-prod', regions: [] },
] as unknown as Account[]

function renderWizard() {
  return render(<DiscoveryWizard accounts={accounts} onAccountCreated={vi.fn()} onClose={vi.fn()} />)
}

describe('DiscoveryWizard provider choice', () => {
  it('lists only the chosen provider accounts and relabels the modes', () => {
    renderWizard()
    expect(screen.getByRole('option', { name: /AWS Prod/ })).toBeTruthy()
    expect(screen.queryByRole('option', { name: /GCP Net/ })).toBeNull()
    expect(screen.getByText('AWS Organization')).toBeTruthy()

    fireEvent.click(screen.getByLabelText('GCP'))

    expect(screen.getByRole('option', { name: /GCP Net/ })).toBeTruthy()
    expect(screen.queryByRole('option', { name: /AWS Prod/ })).toBeNull()
    expect(screen.getByText('Single Project')).toBeTruthy()
    expect(screen.getByText('GCP Organization or Folder')).toBeTruthy()
  })

  it('requires a valid parent before leaving step 1 in gcp org mode', () => {
    renderWizard()
    fireEvent.click(screen.getByLabelText('GCP'))
    fireEvent.click(screen.getByText('GCP Organization or Folder'))
    const next = screen.getByRole('button', { name: /next/i }) as HTMLButtonElement
    expect(next.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Organization or folder'), { target: { value: 'folders/12' } })
    expect(next.disabled).toBe(false)
  })
})
```

Check `ui/src/__tests__/DiscoveryResourcesTab.test.tsx` for how this repo imports `@testing-library/react` and sets up jsdom; mirror it (e.g. a `// @vitest-environment jsdom` pragma if that file uses one). Check the Next button's accessible name in the footer JSX and adjust `/next/i` if it differs.

Run: `cd ui && nix develop --command npx vitest run src/__tests__/DiscoveryWizardProvider.test.tsx`
Expected: FAIL (`Unable to find a label with the text of: GCP`).

- [ ] **Step 5: Implement the component changes**

In `ui/src/components/DiscoveryWizard.tsx`:

1. Extend the builder import with `GCP_PARENT_PATTERN, gcpProjectFor, type WizardProvider`.
2. Add state after `discoveryMode`:

```ts
  const [provider, setProvider] = useState<WizardProvider>('aws')
  const providerAccounts = accounts.filter(a => a.provider === provider)
```

   change the `selectedAccountId` initial value to `accounts.find(a => a.provider === 'aws')?.id ?? null`, and add after the org-mode fields:

```ts
  // GCP fields
  const [gcpProjectInput, setGcpProjectInput] = useState('')
  const [gcpOrgParent, setGcpOrgParent] = useState('')
  const [gcpOrgRegionsInput, setGcpOrgRegionsInput] = useState('')
  const [gcpOrgExcludeInput, setGcpOrgExcludeInput] = useState('')
  const [gcpOrgImpersonate, setGcpOrgImpersonate] = useState('')
```

3. Add a handler next to `goToStep2`:

```ts
  function chooseProvider(next: WizardProvider) {
    setProvider(next)
    setSelectedAccountId(accounts.find(a => a.provider === next)?.id ?? null)
    setNewAccount(prev => ({ ...prev, provider: next }))
  }
```

4. At the top of the Step 1 body (before `<h3 ...>Discovery Mode</h3>`), add:

```tsx
              <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100">Cloud Provider</h3>
              <div className="flex gap-3" role="radiogroup" aria-label="Cloud provider">
                {(['aws', 'gcp'] as const).map(p => (
                  <label
                    key={p}
                    className={`flex-1 cursor-pointer rounded-lg border-2 p-3 text-sm font-medium transition-colors ${
                      provider === p
                        ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20 dark:border-blue-400 text-gray-900 dark:text-gray-100'
                        : 'border-gray-200 dark:border-gray-600 hover:border-gray-300 dark:hover:border-gray-500 text-gray-700 dark:text-gray-300'
                    }`}
                  >
                    <input
                      type="radio"
                      name="provider"
                      value={p}
                      checked={provider === p}
                      onChange={() => chooseProvider(p)}
                      className="sr-only"
                      aria-label={p.toUpperCase()}
                    />
                    {p.toUpperCase()}
                  </label>
                ))}
              </div>
```

5. Mode card copy: replace the four literal strings with expressions:
   - `Single Account` → `{provider === 'gcp' ? 'Single Project' : 'Single Account'}`
   - `Discover resources in one AWS account` → `{provider === 'gcp' ? 'Discover networks in one GCP project' : 'Discover resources in one AWS account'}`
   - `AWS Organization` → `{provider === 'gcp' ? 'GCP Organization or Folder' : 'AWS Organization'}`
   - `Discover across all member accounts via AssumeRole` → `{provider === 'gcp' ? 'Discover every active project under an organization or folder' : 'Discover across all member accounts via AssumeRole'}`
6. Account picker: change `accounts.map(a => (` to `providerAccounts.map(a => (`, and the heading expression to:

```tsx
                    {discoveryMode === 'organization'
                      ? (provider === 'gcp' ? 'Select the Account This Agent Reports As' : 'Select Management Account')
                      : (provider === 'gcp' ? 'Select GCP Project Account' : 'Select Cloud Account')}
```

   Below the `<select>`, add the project fallback input for GCP single mode:

```tsx
                      {provider === 'gcp' && discoveryMode === 'single' && selectedAccount && !gcpProjectFor(selectedAccount) && (
                        <div>
                          <label htmlFor="gcp-project" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">GCP project ID</label>
                          <input
                            id="gcp-project"
                            value={gcpProjectInput}
                            onChange={e => setGcpProjectInput(e.target.value)}
                            placeholder="my-project-123"
                            className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                          />
                        </div>
                      )}
```

7. Wrap the existing organization-mode block's condition as `discoveryMode === 'organization' && provider === 'aws'`, and add a GCP org block after it:

```tsx
              {discoveryMode === 'organization' && provider === 'gcp' && (
                <div className="space-y-3">
                  <p className="text-sm text-gray-500 dark:text-gray-400">
                    The agent lists every active project under the organization or folder and discovers each one.
                    Projects are auto-created in CloudPAM as <code className="text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">gcp:&lt;project&gt;</code> accounts.
                  </p>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="col-span-2">
                      <label htmlFor="gcp-org-parent" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Organization or folder</label>
                      <input
                        id="gcp-org-parent"
                        value={gcpOrgParent}
                        onChange={e => setGcpOrgParent(e.target.value.trim())}
                        placeholder="organizations/123456789 or folders/987654321"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                      {gcpOrgParent && !GCP_PARENT_PATTERN.test(gcpOrgParent) && (
                        <p className="mt-1 text-xs text-red-600 dark:text-red-400">Use organizations/&lt;id&gt; or folders/&lt;id&gt;.</p>
                      )}
                    </div>
                    <div>
                      <label htmlFor="gcp-org-regions" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Regions (optional, comma-separated)</label>
                      <input
                        id="gcp-org-regions"
                        value={gcpOrgRegionsInput}
                        onChange={e => setGcpOrgRegionsInput(e.target.value)}
                        placeholder="us-central1, europe-west1"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                    <div>
                      <label htmlFor="gcp-org-exclude" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Exclude projects (optional)</label>
                      <input
                        id="gcp-org-exclude"
                        value={gcpOrgExcludeInput}
                        onChange={e => setGcpOrgExcludeInput(e.target.value)}
                        placeholder="sandbox-project, legacy-project"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                    </div>
                    <div className="col-span-2">
                      <label htmlFor="gcp-org-impersonate" className="block text-xs text-gray-500 dark:text-gray-400 mb-1">Impersonate service account (optional)</label>
                      <input
                        id="gcp-org-impersonate"
                        value={gcpOrgImpersonate}
                        onChange={e => setGcpOrgImpersonate(e.target.value.trim())}
                        placeholder="cloudpam-discovery@{project}.iam.gserviceaccount.com"
                        className="w-full px-3 py-1.5 border dark:border-gray-600 rounded text-sm dark:bg-gray-700 dark:text-gray-100"
                      />
                      <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                        Empty: the agent reads every project as itself, through grants on the organization or folder.
                        Set: it impersonates this account in each project; <code>{'{project}'}</code> becomes the project ID.
                      </p>
                    </div>
                  </div>
                </div>
              )}
```

8. Step-2 description: replace `'Create a discovery agent for your AWS Organization. ...'` with `` `Create a discovery agent for your ${provider === 'gcp' ? 'GCP organization or folder' : 'AWS Organization'}. This generates a bootstrap token and API key.` ``.
9. Step-1 validity: replace

```ts
  const canGoNext = (step === 1 && selectedAccountId !== null) ||
    (step === 2 && provisionResult !== null)
```

   with

```ts
  const gcpProject = gcpProjectFor(selectedAccount) || gcpProjectInput.trim()
  const step1Valid = selectedAccountId !== null && (
    provider !== 'gcp' || (isOrg ? GCP_PARENT_PATTERN.test(gcpOrgParent) : gcpProject !== '')
  )
  const canGoNext = (step === 1 && step1Valid) ||
    (step === 2 && provisionResult !== null)
```

   Move the `gcpProject` line above its first use if `isOrg` is declared later; `isOrg` is declared in the config section, so place both lines after it.
10. Builder input: change the `buildAgentConfigs({...})` call from Task 8 to:

```ts
  const generated = buildAgentConfigs({
    provider,
    mode: discoveryMode,
    token,
    serverUrl,
    agentName: agentNameFinal,
    accountId,
    accountRegions,
    awsOrg: { roleName: orgRoleName, externalId: orgExternalId, regions: orgRegions, exclude: orgExclude },
    gcpProject,
    gcpOrg: {
      parent: gcpOrgParent,
      regions: splitList(gcpOrgRegionsInput),
      exclude: splitList(gcpOrgExcludeInput),
      impersonateServiceAccount: gcpOrgImpersonate,
    },
  })
  const availableTabs = tabsFor(provider, discoveryMode)
```

    and in `tabMeta` make the IAM filename provider-aware: `iam: { filename: provider === 'gcp' ? \`${filenameStem}-iam.sh\` : \`${filenameStem}-iam.tf\`, label: 'IAM Setup' }`.
11. `goToStep2`: for GCP org mode default the agent name to `gcp-org-discovery-agent`:

```ts
    } else if (discoveryMode === 'organization' && !agentName) {
      setAgentName(provider === 'gcp' ? 'gcp-org-discovery-agent' : 'org-discovery-agent')
    }
```

- [ ] **Step 6: Run UI tests, types and lint**

Run: `cd ui && nix develop --command npx vitest run`
Expected: PASS (all suites).

Run: `cd ui && nix develop --command npx tsc --noEmit && nix develop --command npm run lint --if-present`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add ui/src
git commit -m "feat(ui): set up gcp project and organization agents from the discovery wizard (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Docs, changelog and release version

**Files:**
- Modify: `docs/DISCOVERY.md`, `docs/DISCOVERY_AGENT_PLAN.md`, `docs/CHANGELOG.md`, `CLAUDE.md`
- Modify: `deploy/helm/cloudpam-agent/Chart.yaml`, `deploy/helm/cloudpam-server/Chart.yaml`

- [ ] **Step 1: Changelog**

Insert above `## [0.24.0] - 2026-11-03` in `docs/CHANGELOG.md`:

```markdown
## [0.25.0] - 2026-11-04

### Breaking
- The discovery agent no longer assumes AWS. Set `CLOUDPAM_PROVIDER` (`provider:` in YAML) to `aws` or `gcp`; an agent without it refuses to start with `provider is required (aws or gcp)`. Existing AWS agents need `CLOUDPAM_PROVIDER=aws`. The agent Helm chart requires `config.provider` and fails to render without it. (#274)

### Added
- The discovery agent discovers GCP: one project with `CLOUDPAM_PROVIDER=gcp` and `CLOUDPAM_GCP_PROJECT`, or every active project under an organization or folder with `CLOUDPAM_GCP_ORG_ENABLED` and `CLOUDPAM_GCP_ORG_PARENT`. Org mode reads projects as the agent's own identity (grant `roles/browser` and `roles/compute.networkViewer` on the parent) or, with `CLOUDPAM_GCP_ORG_IMPERSONATE_SA`, impersonates a per-project service account. `CLOUDPAM_DISCOVERY_GCP_RPS` caps GCP calls. (#274)
- The Discovery wizard offers AWS or GCP and generates GCP single-project and organization configs plus the `gcloud` IAM commands. (#274)
- The agent Helm chart accepts `config.gcpProject`, `config.gcpRegions`, `config.discoveryGcpRps`, `config.gcpOrg.*`, and `gcpCredentials.existingSecret` for a mounted service-account key. (#274)

### Changed
- `POST /api/v1/discovery/ingest/org` takes `provider` (required, `aws` or `gcp`) and `external_id` on every entry; `aws_account_id` is a deprecated alias. An entry with no `<provider>:<external_id>` account now attaches to an existing account with the same provider and external ID instead of creating a duplicate; this applies to AWS org ingest as well. (#274)
- Server and agent Helm charts advertise application version `0.25.0`; chart package versions remain `0.2.1`.
```

- [ ] **Step 2: Chart versions**

Set `appVersion: "0.25.0"` in `deploy/helm/cloudpam-agent/Chart.yaml` and `deploy/helm/cloudpam-server/Chart.yaml`.

Run: `nix develop --command ./scripts/helm-check.sh`
Expected: exit 0, no `FAIL:` lines.

- [ ] **Step 3: DISCOVERY.md**

1. In "### How discovery reaches an account" (around line 551), add rows to its table for "Agent, GCP single-project mode" (identity: the agent's ADC; reach: that project) and "Agent, GCP organization mode" (identity: the agent's ADC, optionally impersonating a per-project service account; reach: every active project under the organization or folder). Match the existing column layout of that table.
2. Add a new top-level section before `## AWS Setup`:

```markdown
## GCP Agent

The agent discovers GCP networks, subnetworks and external addresses with `CLOUDPAM_PROVIDER=gcp`. It authenticates with Application Default Credentials: GKE Workload Identity, a service-account key at `GOOGLE_APPLICATION_CREDENTIALS`, or the metadata server on a GCE VM. It adds no credential settings of its own.

### Single project

| Setting | Meaning |
|---|---|
| `CLOUDPAM_PROVIDER=gcp` | Required. |
| `CLOUDPAM_GCP_PROJECT` | The project ID to discover. Required. |
| `CLOUDPAM_GCP_REGIONS` | Optional comma list of regions to keep. Networks are global. |
| `CLOUDPAM_DISCOVERY_GCP_RPS` | Optional cap on GCP API calls per second. |

Grant the agent's service account `roles/compute.networkViewer` on the project:

    gcloud projects add-iam-policy-binding my-project \
      --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" \
      --role=roles/compute.networkViewer

### Organization or folder

| Setting | Meaning |
|---|---|
| `CLOUDPAM_GCP_ORG_ENABLED=true` | Enables org mode. |
| `CLOUDPAM_GCP_ORG_PARENT` | `organizations/<id>` or `folders/<id>`. Required. |
| `CLOUDPAM_GCP_ORG_REGIONS` | Optional; falls back to `CLOUDPAM_GCP_REGIONS`. |
| `CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS` | Optional comma list of project IDs to skip. |
| `CLOUDPAM_GCP_ORG_IMPERSONATE_SA` | Optional. A service-account email; `{project}` is replaced by each project ID. |

Each pass lists every `ACTIVE` project under the parent (walking folders up to 10 levels deep), discovers each project, and sends one bulk ingest. Projects arrive in CloudPAM as `gcp:<project>` accounts, or attach to an existing account whose provider is `gcp` and whose external ID is the project ID. A project that fails is logged and skipped; if listing fails, nothing is sent and the next pass retries.

`CLOUDPAM_ACCOUNT_ID` is still required: it is the account the agent heartbeats as.

**Direct grants** (no impersonation). Grant the agent's service account, on the organization or folder:

    gcloud organizations add-iam-policy-binding 123456789 \
      --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" --role=roles/browser
    gcloud organizations add-iam-policy-binding 123456789 \
      --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" --role=roles/compute.networkViewer

For a folder, use `gcloud resource-manager folders add-iam-policy-binding <folder-id>`.

**Impersonation.** Keep `roles/browser` on the parent (project listing always runs as the agent). In each project, create the discovery service account, grant it `roles/compute.networkViewer` on its project, and grant the agent `roles/iam.serviceAccountTokenCreator` on it:

    PROJECT_ID=my-project
    gcloud iam service-accounts create cloudpam-discovery --project="$PROJECT_ID"
    gcloud projects add-iam-policy-binding "$PROJECT_ID" \
      --member="serviceAccount:cloudpam-discovery@${PROJECT_ID}.iam.gserviceaccount.com" --role=roles/compute.networkViewer
    gcloud iam service-accounts add-iam-policy-binding "cloudpam-discovery@${PROJECT_ID}.iam.gserviceaccount.com" \
      --project="$PROJECT_ID" \
      --member="serviceAccount:cloudpam-agent@ops-project.iam.gserviceaccount.com" --role=roles/iam.serviceAccountTokenCreator

Then set `CLOUDPAM_GCP_ORG_IMPERSONATE_SA='cloudpam-discovery@{project}.iam.gserviceaccount.com'`. A value without `{project}` reads every project through that one service account.

### Helm

Set `config.provider: gcp` and either `config.gcpProject` or `config.gcpOrg.enabled` with `config.gcpOrg.parent`. On GKE, annotate the Kubernetes service account for Workload Identity:

    serviceAccount:
      annotations:
        iam.gke.io/gcp-service-account: cloudpam-agent@ops-project.iam.gserviceaccount.com

Without Workload Identity, store a key as a secret with the key `key.json` and set `gcpCredentials.existingSecret`.

### Troubleshooting

| Log message | Cause | Fix |
|---|---|---|
| `failed to list gcp projects ... list projects under ...: http 403` | The agent lacks `resourcemanager.projects.list` on that node. | Grant `roles/browser` on the organization or folder. |
| `gcp credentials failed for project` / `discovery failed for project ... impersonate ...: http 403` | The agent lacks `roles/iam.serviceAccountTokenCreator` on that project's service account, or the account does not exist. | Create the account and grant token creator. |
| `discovery failed for project ... http 403` on Compute | The identity lacks `roles/compute.networkViewer`, or the Compute API is disabled in that project. | Grant the role, or enable `compute.googleapis.com`, or exclude the project. |
```

  Check how `discovery.HTTPError.Error()` formats the status (e.g. `http 403` vs `status 403`) and match the troubleshooting table to it.

- [ ] **Step 4: DISCOVERY_AGENT_PLAN.md and CLAUDE.md**

1. In `docs/DISCOVERY_AGENT_PLAN.md`, find the GCP agent in the plan diagram/text and add a line under it: `Implemented in 0.25.0: single-project and organization/folder modes (see docs/DISCOVERY.md "GCP Agent").`
2. In `CLAUDE.md`, rename the section heading `### Agent (Org Mode)` to `### Agent`, and add at its top:

```markdown
- `CLOUDPAM_PROVIDER`: Required, `aws` or `gcp` (no default)
- `CLOUDPAM_GCP_PROJECT`: GCP project for single-project mode
- `CLOUDPAM_GCP_REGIONS`: Comma-separated GCP regions to keep
- `CLOUDPAM_DISCOVERY_GCP_RPS`: Cap on GCP API calls per second
- `CLOUDPAM_GCP_ORG_ENABLED`, `CLOUDPAM_GCP_ORG_PARENT` (`organizations/<id>` or `folders/<id>`), `CLOUDPAM_GCP_ORG_REGIONS`, `CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS`, `CLOUDPAM_GCP_ORG_IMPERSONATE_SA` (`{project}` placeholder): GCP organization/folder mode
```

   and in the "Remaining Work" Phase 2 list, nothing changes (Azure is still missing).

- [ ] **Step 5: Commit**

```bash
git add docs/ CLAUDE.md deploy/helm/*/Chart.yaml
git commit -m "docs: gcp discovery agent setup, breaking provider setting, 0.25.0 (#274)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Full verification

**Files:** none changed unless a check fails.

- [ ] **Step 1: Go tests, race, SQLite and lint**

Run: `nix develop --command just test`
Expected: PASS.

Run: `nix develop --command just test-race`
Expected: PASS.

Run: `nix develop --command go test -tags sqlite ./internal/api/ ./cmd/...`
Expected: PASS (org ingest against the SQLite store).

Run: `nix develop --command just lint`
Expected: `0 issues.`

- [ ] **Step 2: Frontend**

Run: `cd ui && nix develop --command npx vitest run && nix develop --command npx tsc --noEmit && nix develop --command npm run build`
Expected: all pass; build writes `web/dist/`.

- [ ] **Step 3: Helm and OpenAPI**

Run: `nix develop --command ./scripts/helm-check.sh && nix develop --command just openapi-validate`
Expected: exit 0.

- [ ] **Step 4: Agent smoke**

Run: `nix develop --command go build -o /tmp/cloudpam-agent-smoke ./cmd/cloudpam-agent && CLOUDPAM_SERVER_URL=http://127.0.0.1:1 CLOUDPAM_API_KEY=x CLOUDPAM_AGENT_NAME=a CLOUDPAM_ACCOUNT_ID=1 /tmp/cloudpam-agent-smoke 2>&1 | head -3`
Expected: a JSON log line with `"msg":"failed to load config"` and `provider is required (aws or gcp)`.

- [ ] **Step 5: UI evidence**

Invoke the `verify-cloudpam` skill. Drive the Discovery wizard: choose GCP, single-project mode with a GCP account, provision, and capture the Shell and IAM tabs; then GCP org mode with `organizations/123456789` and an impersonation template, and capture the YAML and IAM tabs. Save the screenshots the skill produces and list their paths in the PR description.

- [ ] **Step 6: Record the known gap**

In the PR description, state: no run against a real GCP organization; Resource Manager, IAM Credentials and Compute are exercised through `httptest` fakes following the documented REST shapes.
