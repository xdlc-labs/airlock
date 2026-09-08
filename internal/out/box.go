package out

import (
	"strings"
	"unicode/utf8"
)

const minInner = 46

// Frame wraps lines in a rounded box with a title on the top rule.
func Frame(title string, lines []string) string {
	inner := minInner
	titleW := utf8.RuneCountInString(title)
	if need := 3 + titleW + 1; need > inner {
		inner = need
	}
	for _, ln := range lines {
		if w := visibleLen(ln) + 2; w > inner {
			inner = w
		}
	}
	topFill := inner - 3 - titleW
	if topFill < 1 {
		topFill = 1
		inner = 3 + titleW + topFill
	}
	var b strings.Builder
	b.WriteString("╭─ ")
	b.WriteString(title)
	b.WriteByte(' ')
	b.WriteString(strings.Repeat("─", topFill))
	b.WriteString("╮\n")
	for _, ln := range lines {
		pad := inner - 1 - visibleLen(ln)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("│ ")
		b.WriteString(ln)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString("│\n")
	}
	b.WriteString("╰")
	b.WriteString(strings.Repeat("─", inner))
	b.WriteString("╯\n")
	return b.String()
}

// FrameText boxes a multiline string.
func FrameText(title, body string) string {
	body = strings.TrimSuffix(body, "\n")
	if body == "" {
		return Frame(title, nil)
	}
	return Frame(title, strings.Split(body, "\n"))
}

// visibleLen counts printable columns, skipping ANSI escape sequences. It
// counts runes rather than bytes: iterating bytes made every multi-byte
// character (an em-dash in a gate reason, an accented prompt id) count two or
// three columns, so Frame over-padded that row and the box's right border came
// out ragged. Double-width glyphs such as CJK and emoji still count as one.
func visibleLen(s string) int {
	n := 0
	esc := false
	for _, r := range s {
		if r == '\033' {
			esc = true
			continue
		}
		if esc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				esc = false
			}
			continue
		}
		n++
	}
	return n
}

// Glyph is + / - / ~ for a change status. Colored on a TTY.
func Glyph(status string) string {
	var g, col string
	switch status {
	case "added":
		g, col = "+", green
	case "removed":
		g, col = "-", red
	default:
		g, col = "~", yellow
	}
	if color() {
		return col + g + reset
	}
	return g
}

// Dim dims s on a TTY.
func Dim(s string) string {
	if !color() {
		return s
	}
	return "\033[2m" + s + reset
}
