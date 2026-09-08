package promptfoo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportFixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "promptfoo.yaml")
	if err := os.WriteFile(path, []byte(`
prompts:
  - "Say hello"
providers:
  - mock
tests:
  - description: hello-json
    vars:
      query: "ping"
    assert:
      - type: is-json
  - description: hello-contains
    vars:
      query: "exact:{\"ok\":true,\"answer\":\"mock\"}"
    assert:
      - type: contains
        value: "ok"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cases) != 2 {
		t.Fatalf("want 2 cases got %d", len(res.Cases))
	}
	if !res.Cases[0].Expect.JSONValid {
		t.Fatal("expected is-json → json_valid")
	}
}

func TestUnsupportedWarn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pf.yaml")
	if err := os.WriteFile(path, []byte(`
prompts: ["hi"]
providers: [mock]
tests:
  - description: x
    assert:
      - type: llm-rubric
        value: nice
      - type: is-json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected warning for llm-rubric")
	}
	if !res.Cases[0].Expect.JSONValid {
		t.Fatal("is-json should still apply")
	}
}
