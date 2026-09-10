package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/store"
)

// githubEnv points the CLI at a fake GitHub that serves one pull request with
// the given reviews (login -> state on the head commit) and roles.
func githubEnv(t *testing.T, reviews map[string]string, roles map[string]string) {
	t.Helper()
	const head = "feedfacefeedfacefeedfacefeedfacefeedface"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/acme/agent/pulls/12":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"head": map[string]any{"sha": head},
				"user": map[string]any{"login": "author"},
			})
		case r.URL.Path == "/repos/acme/agent/pulls/12/reviews":
			out := []map[string]any{}
			for login, state := range reviews {
				out = append(out, map[string]any{
					"state": state, "commit_id": head,
					"user": map[string]any{"login": login},
				})
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, "/repos/acme/agent/collaborators/"):
			login := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/agent/collaborators/"), "/permission")
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": roles[login], "role_name": roles[login]})
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	event := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(event, []byte(`{"pull_request":{"number":12}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("GITHUB_REPOSITORY", "acme/agent")
	t.Setenv("GITHUB_EVENT_PATH", event)
}

func readComment(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.ForRoot(root).Airlock, "ci-comment.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestCmdCIGitHubReviewUnblocksWithoutLedger is issue #7: fail closed on a
// permission expansion, unblock with a review, commit nothing.
func TestCmdCIGitHubReviewUnblocksWithoutLedger(t *testing.T) {
	root, baseID := approvalRepo(t)
	githubEnv(t, map[string]string{"reviewer": "APPROVED"}, map[string]string{"reviewer": "write"})

	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval", "--fail-on-approval", "--github-approvals"})
	if err != nil {
		t.Fatalf("an approving review from a writer should unblock: %v", err)
	}
	if entries, _ := os.ReadDir(store.ForRoot(root).Approvals); len(entries) != 0 {
		t.Fatalf("review approval must not write ledger files, found %d", len(entries))
	}
	body := readComment(t, root)
	if !strings.Contains(body, "Signed off by @reviewer") {
		t.Fatalf("comment should name the reviewer, got:\n%s", body)
	}
	if strings.Contains(body, "airlock approve") {
		t.Fatalf("comment must not ask for a ledger entry in GitHub mode, got:\n%s", body)
	}
}

func TestCmdCIGitHubNoReviewFailsClosedAndAsksForOne(t *testing.T) {
	root, baseID := approvalRepo(t)
	githubEnv(t, map[string]string{"reviewer": "COMMENTED"}, map[string]string{"reviewer": "write"})

	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval", "--fail-on-approval", "--github-approvals"})
	if err == nil || !strings.Contains(err.Error(), "approving review") {
		t.Fatalf("expected a NEEDS_APPROVAL failure naming the review path, got %v", err)
	}
	body := readComment(t, root)
	if !strings.Contains(body, "Approve this pull request") {
		t.Fatalf("comment should ask for a review, got:\n%s", body)
	}
	if strings.Contains(body, "airlock approve") {
		t.Fatalf("comment must not lead with the ledger command in GitHub mode, got:\n%s", body)
	}
}

func TestCmdCIGitHubReadOnlyReviewerDoesNotUnblock(t *testing.T) {
	root, baseID := approvalRepo(t)
	githubEnv(t, map[string]string{"reader": "APPROVED"}, map[string]string{"reader": "read"})

	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval", "--fail-on-approval", "--github-approvals"})
	if err == nil {
		t.Fatal("a read-only reviewer must not unblock a permission expansion")
	}
}

func TestCmdCIGitHubAPIFailureFailsClosed(t *testing.T) {
	root, baseID := approvalRepo(t)
	githubEnv(t, map[string]string{"reviewer": "APPROVED"}, map[string]string{"reviewer": "write"})
	t.Setenv("GITHUB_TOKEN", "wrong")

	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval", "--fail-on-approval", "--github-approvals"})
	if err == nil || !strings.Contains(err.Error(), "could not be checked") {
		t.Fatalf("an API failure must block, not pass, got %v", err)
	}
}

func TestCmdCIGitHubMissingEnvFailsClosed(t *testing.T) {
	root, baseID := approvalRepo(t)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_EVENT_PATH", "")

	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval", "--fail-on-approval", "--github-approvals"})
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("a misconfigured workflow should be told what is missing, got %v", err)
	}
}

// TestCmdCILedgerStillWorksBesideGitHub: a committed ledger entry keeps
// unblocking even when the workflow also reads reviews.
func TestCmdCILedgerStillWorksBesideGitHub(t *testing.T) {
	root, baseID := approvalRepo(t)
	githubEnv(t, nil, nil)
	if err := cmdApprove([]string{"--path", root, "--base", baseID, "--by", "dev"}); err != nil {
		t.Fatal(err)
	}
	err := cmdCI([]string{"--path", root, "--base", baseID, "--skip-eval", "--fail-on-approval", "--github-approvals"})
	if err != nil {
		t.Fatalf("ledger approval should still count: %v", err)
	}
	if body := readComment(t, root); !strings.Contains(body, "Signed off by dev (ledger)") {
		t.Fatalf("comment should say who approved, got:\n%s", body)
	}
}

func TestCmdApproveDefaultsToLatestSnapshot(t *testing.T) {
	root, baseID := approvalRepo(t)
	if err := cmdApprove([]string{"--path", root}); err != nil {
		t.Fatalf("approve without --base should use the last snapshot: %v", err)
	}
	entries, err := os.ReadDir(store.ForRoot(root).Approvals)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one ledger entry, got %d (%v)", len(entries), err)
	}
	if !strings.HasPrefix(entries[0].Name(), baseID+"__") {
		t.Fatalf("ledger entry should be keyed by the latest snapshot %s, got %s", baseID, entries[0].Name())
	}
}

func TestInitWritesGitignore(t *testing.T) {
	root := t.TempDir()
	if err := cmdInit([]string{"--path", root}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.ForRoot(root).Airlock, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"manifest.json", "snapshots/", "approvals/", "ci-comment.md"} {
		if !strings.Contains(string(data), "\n"+want+"\n") {
			t.Fatalf(".airlock/.gitignore should ignore %s, got:\n%s", want, data)
		}
	}
	for _, keep := range []string{"\npolicy.yml\n", "\nevals/\n", "\ncassettes/\n", "\nsentinel/\n"} {
		if strings.Contains(string(data), keep) {
			t.Fatalf(".airlock/.gitignore must not ignore %q, got:\n%s", strings.TrimSpace(keep), data)
		}
	}

	// A team's own .gitignore is left alone.
	if err := os.WriteFile(filepath.Join(store.ForRoot(root).Airlock, ".gitignore"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdInit([]string{"--path", root}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(store.ForRoot(root).Airlock, ".gitignore")); string(data) != "mine\n" {
		t.Fatalf("init overwrote an existing .gitignore: %q", data)
	}
}
