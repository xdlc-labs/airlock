package out

import (
	"bytes"
	"strings"
	"testing"
)

func TestVerdictNoColor(t *testing.T) {
	var buf bytes.Buffer
	SetWriter(&buf)
	t.Cleanup(func() { SetWriter(nil) })
	t.Setenv("NO_COLOR", "1")
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("FORCE_COLOR", "")
	Verdict("PASS")
	got := buf.String()
	if strings.Contains(got, "\033") {
		t.Fatalf("ANSI leaked: %q", got)
	}
	if !strings.Contains(got, "PASS") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "╭") || !strings.Contains(got, "verdict") {
		t.Fatalf("expected boxed verdict, got %q", got)
	}
}

func TestGitHubAnnotations(t *testing.T) {
	var buf bytes.Buffer
	SetWriter(&buf)
	t.Cleanup(func() { SetWriter(nil) })
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("NO_COLOR", "1")
	Verdict("FAIL")
	end := Group("Gate")
	end()
	got := buf.String()
	if !strings.Contains(got, "::error::Airlock FAIL") {
		t.Fatalf("missing error annotation, got %q", got)
	}
	if !strings.Contains(got, "::group::Gate") || !strings.Contains(got, "::endgroup::") {
		t.Fatalf("missing group markers, got %q", got)
	}
	if strings.Contains(got, "\033") {
		t.Fatalf("ANSI leaked under GITHUB_ACTIONS: %q", got)
	}
}

func TestFramePadsTitle(t *testing.T) {
	got := Frame("init", []string{"agents  1"})
	if !strings.Contains(got, "╭─ init ") {
		t.Fatalf("missing title, got %q", got)
	}
	if !strings.Contains(got, "│ agents  1") {
		t.Fatalf("missing body, got %q", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "╯") {
		t.Fatalf("missing bottom, got %q", got)
	}
}

func TestPrintAddsNewline(t *testing.T) {
	var buf bytes.Buffer
	SetWriter(&buf)
	t.Cleanup(func() { SetWriter(nil) })
	t.Setenv("NO_COLOR", "1")
	Print("hello")
	if buf.String() != "hello\n" {
		t.Fatalf("got %q", buf.String())
	}
}
