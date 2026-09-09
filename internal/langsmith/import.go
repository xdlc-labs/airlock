package langsmith

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/xdlc-labs/airlock/internal/evalcase"
)

// ImportFile reads a LangSmith dataset export JSON into Airlock eval cases.
// Supports { "examples": [ { "inputs": {...}, "outputs": {...} } ] } shape.
func ImportFile(path string) ([]evalcase.Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Examples []map[string]any `json:"examples"`
		Rows     []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	rows := raw.Examples
	if len(rows) == 0 {
		rows = raw.Rows
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("langsmith export: no examples")
	}
	out := casesFromRows(rows)
	if len(out) == 0 {
		return nil, fmt.Errorf("langsmith export: no usable rows")
	}
	return out, nil
}

// casesFromRows converts LangSmith example rows into eval cases. Shared by the
// file export and the API pull so both produce the same cases for the same
// examples. Rows with no usable input are dropped.
func casesFromRows(rows []map[string]any) []evalcase.Case {
	var out []evalcase.Case
	for i, row := range rows {
		in := stringify(row["inputs"])
		if in == "" {
			in = stringify(row["input"])
		}
		if in == "" {
			continue
		}
		exp := stringify(row["outputs"])
		if exp == "" {
			exp = stringify(row["output"])
		}
		id := fmt.Sprintf("ls-%d", i+1)
		if meta, ok := row["id"].(string); ok && meta != "" {
			id = "ls-" + meta
		}
		c := evalcase.Case{
			ID:    id,
			Agent: "default",
			Input: evalcase.Input{
				Provider: "mock",
				Messages: []evalcase.Message{{Role: "user", Content: in}},
			},
			Tags: []string{"langsmith", "import"},
		}
		if exp != "" {
			c.Expect.Contains = exp
		} else {
			c.Expect.JSONValid = true
		}
		out = append(out, c)
	}
	return out
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		for _, k := range []string{"answer", "output", "text", "question", "input", "prompt", "query", "messages"} {
			if s, ok := t[k].(string); ok && s != "" {
				return s
			}
		}
		b, _ := json.Marshal(t)
		return string(b)
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

// ImportJSONL reads LangSmith-style JSONL (one example per line).
func ImportJSONL(path string) ([]evalcase.Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []evalcase.Case
	lineNo := 0
	for _, line := range splitLines(string(data)) {
		lineNo++
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		in := stringify(row["inputs"])
		exp := stringify(row["outputs"])
		id := "ls-" + strconv.Itoa(lineNo)
		c := evalcase.Case{
			ID: id, Agent: "default",
			Input: evalcase.Input{Provider: "mock", Messages: []evalcase.Message{{Role: "user", Content: in}}},
			Tags:  []string{"langsmith", "import"},
		}
		if exp != "" {
			c.Expect.Contains = exp
		}
		out = append(out, c)
	}
	return out, nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
