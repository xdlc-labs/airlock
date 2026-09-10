package approval_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/approval"
)

type fakeReview struct {
	Login    string
	State    string
	CommitID string
}

// fakeGitHub serves the three endpoints GitHubReviewApprovals reads.
func fakeGitHub(t *testing.T, headSHA, author string, reviews []fakeReview, roles map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/acme/agent/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"head": map[string]any{"sha": headSHA},
				"user": map[string]any{"login": author},
			})
		case r.URL.Path == "/repos/acme/agent/pulls/7/reviews":
			if r.URL.Query().Get("page") != "1" {
				_, _ = w.Write([]byte("[]"))
				return
			}
			out := make([]map[string]any, 0, len(reviews))
			for _, rv := range reviews {
				out = append(out, map[string]any{
					"state":     rv.State,
					"commit_id": rv.CommitID,
					"user":      map[string]any{"login": rv.Login},
				})
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/agent/collaborators/"):
			login := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/agent/collaborators/"), "/permission")
			role, ok := roles[login]
			if !ok {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": role, "role_name": role})
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusBadRequest)
		}
	}))
}

func optsFor(srv *httptest.Server) approval.GitHubOptions {
	return approval.GitHubOptions{APIURL: srv.URL, Token: "tok", Repo: "acme/agent", PR: 7, Client: srv.Client()}
}

func TestGitHubReviewApprovalOnHeadByWriter(t *testing.T) {
	srv := fakeGitHub(t, "head000", "author",
		[]fakeReview{{Login: "reviewer", State: "APPROVED", CommitID: "head000"}},
		map[string]string{"reviewer": "write"})
	defer srv.Close()

	got, err := approval.GitHubReviewApprovals(context.Background(), optsFor(srv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Login != "reviewer" || got[0].Role != "write" {
		t.Fatalf("approvers = %+v", got)
	}
	if note := approval.FormatApprovers(got); !strings.Contains(note, "@reviewer") || !strings.Contains(note, "head000") {
		t.Fatalf("note = %q", note)
	}
}

func TestGitHubReviewApprovalOnOlderCommitDoesNotCount(t *testing.T) {
	srv := fakeGitHub(t, "head000", "author",
		[]fakeReview{{Login: "reviewer", State: "APPROVED", CommitID: "old0000"}},
		map[string]string{"reviewer": "admin"})
	defer srv.Close()

	got, err := approval.GitHubReviewApprovals(context.Background(), optsFor(srv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("an approval on a superseded commit must not stand, got %+v", got)
	}
}

func TestGitHubReviewLatestStateWins(t *testing.T) {
	srv := fakeGitHub(t, "head000", "author",
		[]fakeReview{
			{Login: "reviewer", State: "APPROVED", CommitID: "head000"},
			{Login: "reviewer", State: "COMMENTED", CommitID: "head000"},
			{Login: "reviewer", State: "CHANGES_REQUESTED", CommitID: "head000"},
			{Login: "second", State: "CHANGES_REQUESTED", CommitID: "head000"},
			{Login: "second", State: "APPROVED", CommitID: "head000"},
		},
		map[string]string{"reviewer": "write", "second": "maintain"})
	defer srv.Close()

	got, err := approval.GitHubReviewApprovals(context.Background(), optsFor(srv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Login != "second" {
		t.Fatalf("only the reviewer whose latest review approves should count, got %+v", got)
	}
}

func TestGitHubReviewReadOnlyAndAuthorDoNotCount(t *testing.T) {
	srv := fakeGitHub(t, "head000", "author",
		[]fakeReview{
			{Login: "author", State: "APPROVED", CommitID: "head000"},
			{Login: "reader", State: "APPROVED", CommitID: "head000"},
			{Login: "triager", State: "APPROVED", CommitID: "head000"},
		},
		map[string]string{"author": "admin", "reader": "read", "triager": "triage"})
	defer srv.Close()

	got, err := approval.GitHubReviewApprovals(context.Background(), optsFor(srv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("read, triage, and self-approval must not unblock, got %+v", got)
	}
}

func TestGitHubReviewAPIErrorFailsClosed(t *testing.T) {
	srv := fakeGitHub(t, "head000", "author", nil, nil)
	defer srv.Close()
	opt := optsFor(srv)
	opt.Token = "wrong"

	if _, err := approval.GitHubReviewApprovals(context.Background(), opt); err == nil {
		t.Fatal("an API failure must surface as an error, not as 'not approved'")
	} else if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected the status in the error, got %v", err)
	}
}

func TestGitHubEnvReadsEventPayload(t *testing.T) {
	dir := t.TempDir()
	ev := filepath.Join(dir, "event.json")
	if err := os.WriteFile(ev, []byte(`{"pull_request":{"number":42,"head":{"sha":"abc"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"GITHUB_TOKEN":      "tok",
		"GITHUB_REPOSITORY": "acme/agent",
		"GITHUB_EVENT_PATH": ev,
	}
	opt, err := approval.GitHubEnv(func(k string) string { return env[k] }, 0)
	if err != nil {
		t.Fatal(err)
	}
	if opt.PR != 42 || opt.Repo != "acme/agent" || opt.Token != "tok" {
		t.Fatalf("opt = %+v", opt)
	}

	// An explicit --pr wins over the payload.
	opt, err = approval.GitHubEnv(func(k string) string { return env[k] }, 9)
	if err != nil || opt.PR != 9 {
		t.Fatalf("opt = %+v, err = %v", opt, err)
	}

	// Missing pieces are named, so a misconfigured workflow says what it lacks.
	_, err = approval.GitHubEnv(func(string) string { return "" }, 0)
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") || !strings.Contains(err.Error(), "GITHUB_REPOSITORY") {
		t.Fatalf("expected the missing variables to be named, got %v", err)
	}
}
