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
	// The walk needs 2 project pages + 1 folder page; two tokens let only the
	// project pages through before the deadline fails the folder page's wait.
	_, err := ListProjects(shortDeadline(t), newFakeRMClient(t, f), "organizations/1", limiterWithTokens(t, 2))
	if err == nil {
		t.Fatal("ListProjects() = nil error, want the limiter to fail the third page")
	}
	if f.calls != 2 {
		t.Fatalf("calls = %d, want 2", f.calls)
	}
}

func TestListProjectsFailsOnARepeatedPageToken(t *testing.T) {
	for _, kind := range []string{"projects", "folders"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls > 10 {
					t.Errorf("walk kept paging: %d requests", calls)
					http.Error(w, "too many pages", http.StatusBadRequest)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/"+kind) {
					_, _ = w.Write([]byte(`{"nextPageToken":"same"}`))
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)
			client := &http.Client{Transport: &rewriteTransport{baseURL: srv.URL}}

			_, err := ListProjects(t.Context(), client, "organizations/1", nil)
			want := "list " + kind + " under organizations/1: repeated page token"
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}
