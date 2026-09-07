// Package out writes human CLI logs to stderr.
// Color is TTY-only. GitHub Actions gets workflow commands, not ANSI.
package out

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
)

var stderr io.Writer = os.Stderr

// SetWriter redirects output. Tests use this. Pass nil to restore stderr.
func SetWriter(w io.Writer) {
	if w == nil {
		stderr = os.Stderr
		return
	}
	stderr = w
}

func github() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}

func color() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if github() {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	f, ok := stderr.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Printf writes to stderr.
func Printf(format string, args ...any) {
	fmt.Fprintf(stderr, format, args...)
}

// Print writes s to stderr. Adds a trailing newline if missing.
func Print(s string) {
	if s == "" {
		return
	}
	fmt.Fprint(stderr, s)
	if !strings.HasSuffix(s, "\n") {
		fmt.Fprintln(stderr)
	}
}

// Header writes a boxed section title.
func Header(msg string) {
	Print(Frame(msg, nil))
}

// KV writes an aligned key/value line.
func KV(key, value string) {
	fmt.Fprintf(stderr, "  %-10s %s\n", key, value)
}

// Warn writes a warning line.
func Warn(msg string) {
	fmt.Fprintf(stderr, "warning: %s\n", msg)
}

// Group starts a GitHub Actions log group. The returned function ends it.
// Locally it is a no-op closer.
func Group(name string) func() {
	if github() {
		fmt.Fprintf(stderr, "::group::%s\n", name)
		return func() { fmt.Fprintln(stderr, "::endgroup::") }
	}
	return func() {}
}

// Verdict writes the gate result in a box. On GitHub Actions it also emits a
// notice, warning, or error annotation.
func Verdict(kind string, extra ...string) {
	label := strings.ToUpper(kind)
	painted := label
	if color() {
		painted = paint(label)
	}
	lines := append([]string{painted}, extra...)
	Print(Frame("verdict", lines))
	if !github() {
		return
	}
	switch label {
	case "FAIL":
		fmt.Fprintf(stderr, "::error::Airlock %s\n", label)
	case "NEEDS_APPROVAL", "INCONCLUSIVE":
		fmt.Fprintf(stderr, "::warning::Airlock %s\n", label)
	default:
		fmt.Fprintf(stderr, "::notice::Airlock %s\n", label)
	}
}

func paint(label string) string {
	switch label {
	case "PASS":
		return bold + green + label + reset
	case "FAIL":
		return bold + red + label + reset
	case "NEEDS_APPROVAL", "INCONCLUSIVE", "SKIPPED":
		return bold + yellow + label + reset
	default:
		return label
	}
}
