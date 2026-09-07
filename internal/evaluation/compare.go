package evaluation

import (
	"fmt"
	"strings"

	"github.com/xdlc-labs/airlock/internal/policy"
	"github.com/xdlc-labs/airlock/internal/stats"
)

// CompareRow is one metric in a candidate-vs-baseline experiment compare table.
type CompareRow struct {
	Metric   string             `json:"metric"`
	BaseRate float64            `json:"base_rate"`
	CandRate float64            `json:"candidate_rate"`
	Delta    stats.Interval     `json:"delta"`
	Verdict  policy.VerdictKind `json:"verdict"`
	Reason   string             `json:"reason"`
}

// CompareResults builds experiment-compare rows from paired baseline + candidate runs.
func CompareResults(base, cand *RunResult, pol policy.Policy) []CompareRow {
	if base == nil || cand == nil {
		return nil
	}
	baseRates := map[string]float64{}
	for _, m := range base.Report.Metrics {
		baseRates[m.Name] = m.CI.Estimate
	}
	var rows []CompareRow
	for _, m := range cand.Report.Metrics {
		br, ok := baseRates[m.Name]
		if !ok {
			continue
		}
		row := CompareRow{
			Metric:   m.Name,
			BaseRate: br,
			CandRate: m.CI.Estimate,
			Delta:    stats.Interval{Estimate: m.CI.Estimate - br},
			Verdict:  m.Verdict,
			Reason:   m.Reason,
		}
		if m.Delta != nil {
			row.Delta = *m.Delta
		}
		if g, ok := pol.Gates[m.Name]; ok && g.MaxRegressionPP != nil && m.Delta != nil {
			if stats.FailsMaxRegression(*m.Delta, *g.MaxRegressionPP) {
				row.Verdict = policy.Fail
			} else if stats.PassesMaxRegression(*m.Delta, *g.MaxRegressionPP) {
				row.Verdict = policy.Pass
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// FormatCompareMarkdown renders experiment compare for CI PR comments.
func FormatCompareMarkdown(rows []CompareRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n### Experiment compare (candidate vs baseline)\n\n")
	b.WriteString("| metric | baseline | candidate | Δ (95% CI) | gate |\n")
	b.WriteString("|---|---:|---:|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %.1f%% | %.1f%% | %+.1fpp [%+.1f, %+.1f] | **%s** |\n",
			r.Metric, r.BaseRate*100, r.CandRate*100,
			r.Delta.Estimate*100, r.Delta.Low*100, r.Delta.High*100, r.Verdict)
	}
	return b.String()
}

// FormatCompareText renders a terminal-friendly compare block.
func FormatCompareText(rows []CompareRow) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Compare (candidate vs baseline)\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-16s  base=%.1f%%  cand=%.1f%%  delta=%+.1fpp [%+.1f, %+.1f]  %s\n",
			r.Metric, r.BaseRate*100, r.CandRate*100,
			r.Delta.Estimate*100, r.Delta.Low*100, r.Delta.High*100, r.Verdict)
	}
	return b.String()
}
