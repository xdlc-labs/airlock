package langsmith_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/xdlc-labs/airlock/internal/langsmith"
)

// fakeLangSmith serves the two endpoints the client uses: /datasets?name= and
// paged /examples?dataset=.
func fakeLangSmith(t *testing.T, datasetName string, exampleCount int) (*httptest.Server, *[]string) {
	t.Helper()
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/datasets":
			if r.URL.Query().Get("name") != datasetName {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "11111111-2222-3333-4444-555555555555", "name": datasetName},
			})
		case "/examples":
			if got := r.URL.Query().Get("dataset"); got != "11111111-2222-3333-4444-555555555555" {
				http.Error(w, "wrong dataset "+got, http.StatusBadRequest)
				return
			}
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			rows := []map[string]any{}
			for i := offset; i < exampleCount && len(rows) < limit; i++ {
				rows = append(rows, map[string]any{
					"id":      fmt.Sprintf("ex-%d", i),
					"inputs":  map[string]any{"question": fmt.Sprintf("q%d", i)},
					"outputs": map[string]any{"answer": fmt.Sprintf("a%d", i)},
				})
			}
			_ = json.NewEncoder(w).Encode(rows)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &keys
}

func TestImportDatasetByName(t *testing.T) {
	srv, keys := fakeLangSmith(t, "support-golden", 3)
	t.Setenv("LANGSMITH_API_KEY", "test-key")

	c, err := langsmith.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := c.ImportDataset(context.Background(), "support-golden", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(cases))
	}
	if cases[0].ID != "ls-ex-0" {
		t.Errorf("case id = %q, want ls-ex-0", cases[0].ID)
	}
	if got := cases[0].Input.Messages[0].Content; got != "q0" {
		t.Errorf("input = %q, want q0", got)
	}
	if cases[0].Expect.Contains != "a0" {
		t.Errorf("expect.contains = %q, want a0", cases[0].Expect.Contains)
	}
	for _, k := range *keys {
		if k != "test-key" {
			t.Fatalf("expected every request to carry the api key, got %q", k)
		}
	}
}

func TestImportDatasetPagesAndRespectsLimit(t *testing.T) {
	srv, _ := fakeLangSmith(t, "big", 250)
	t.Setenv("LANGSMITH_API_KEY", "test-key")
	c, err := langsmith.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	all, err := c.ImportDataset(context.Background(), "big", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 250 {
		t.Fatalf("expected all 250 examples across pages, got %d", len(all))
	}

	some, err := c.ImportDataset(context.Background(), "big", 120)
	if err != nil {
		t.Fatal(err)
	}
	if len(some) != 120 {
		t.Fatalf("expected --limit 120 to stop at 120, got %d", len(some))
	}
}

func TestImportDatasetByID(t *testing.T) {
	srv, _ := fakeLangSmith(t, "unused", 1)
	t.Setenv("LANGSMITH_API_KEY", "test-key")
	c, err := langsmith.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// A uuid skips the name lookup: the fake would answer [] for this name.
	cases, err := c.ImportDataset(context.Background(), "11111111-2222-3333-4444-555555555555", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
}

func TestImportDatasetUnknownName(t *testing.T) {
	srv, _ := fakeLangSmith(t, "known", 1)
	t.Setenv("LANGSMITH_API_KEY", "test-key")
	c, err := langsmith.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ImportDataset(context.Background(), "missing", 0)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not-found error, got %v", err)
	}
}

func TestNewClientNeedsKey(t *testing.T) {
	t.Setenv("LANGSMITH_API_KEY", "")
	t.Setenv("LANGCHAIN_API_KEY", "")
	if _, err := langsmith.NewClient(""); err == nil {
		t.Fatal("expected an error with no API key set")
	}
	t.Setenv("LANGCHAIN_API_KEY", "legacy-key")
	c, err := langsmith.NewClient("")
	if err != nil {
		t.Fatalf("LANGCHAIN_API_KEY should still work: %v", err)
	}
	if c.BaseURL != langsmith.DefaultBaseURL {
		t.Errorf("BaseURL = %q, want the hosted default", c.BaseURL)
	}
}

func TestNewClientURLPrecedence(t *testing.T) {
	t.Setenv("LANGSMITH_API_KEY", "test-key")
	t.Setenv("LANGSMITH_ENDPOINT", "https://langsmith.internal/api/v1/")
	c, err := langsmith.NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://langsmith.internal/api/v1" {
		t.Errorf("BaseURL = %q, want the env endpoint with no trailing slash", c.BaseURL)
	}
	c, err = langsmith.NewClient("https://flag.example")
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://flag.example" {
		t.Errorf("BaseURL = %q, want the explicit url to win", c.BaseURL)
	}
}

func TestAPIErrorMentionsTheKeyOnAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	t.Setenv("LANGSMITH_API_KEY", "test-key")
	c, err := langsmith.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ImportDataset(context.Background(), "any", 0)
	if err == nil || !strings.Contains(err.Error(), "LANGSMITH_API_KEY") {
		t.Fatalf("expected the error to point at the key, got %v", err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatal("the error must not echo the key itself")
	}
}
