package manifest_test

import (
	"testing"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

func TestLooksLikeModel(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"claude-sonnet-4-5", true},
		{"gpt-4o-mini", true},
		{"openai/gpt-4o", true},
		{"gemini-2.5-pro", true},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", true},
		{"amazon.nova-pro-v1:0", true},
		{"mistral-large-latest", true},
		{"mixtral-8x7b-instruct", true},
		{"llama-3.3-70b-versatile", true},
		{"llama3.1:8b", true},
		{"meta-llama/Llama-3.1-8B-Instruct", true},
		{"deepseek-chat", true},
		{"deepseek-r1", true},
		{"qwen2.5-72b-instruct", true},
		{"qwen-max", true},
		{"grok-4", true},
		{"command-r-plus", true},
		{"glm-4.6", true},
		{"kimi-k2", true},
		{"gemma2-9b-it", true},
		{"phi-4", true},

		// Not models: package names, config values, bare family names.
		{"llama_index", false},
		{"crewai", false},
		{"command-line", false},
		{"graphical", false},
		{"innovation", false},
		{"mistral", false},
		{"true", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := manifest.LooksLikeModel(tc.in); got != tc.want {
			t.Errorf("LooksLikeModel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestGuessProvider(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-4-5":       "anthropic",
		"gpt-4o":                  "openai",
		"o3-mini":                 "openai",
		"gemini-2.5-pro":          "google",
		"mistral-large-latest":    "mistral",
		"codestral-2508":          "mistral",
		"llama-3.3-70b-versatile": "meta",
		"deepseek-chat":           "deepseek",
		"qwen-max":                "alibaba",
		"grok-4":                  "xai",
		"command-r-plus":          "cohere",
		"glm-4.6":                 "zhipu",
		"amazon.nova-pro-v1:0":    "amazon",
		"text-embedding-3-small":  "",
	}
	for model, want := range cases {
		if got := manifest.GuessProvider(model); got != want {
			t.Errorf("GuessProvider(%q) = %q, want %q", model, got, want)
		}
	}
}
