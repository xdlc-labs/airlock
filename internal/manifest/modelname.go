package manifest

import "strings"

// modelFamilies maps a fragment of a model id to the provider that serves it.
// The first fragment found in the name wins.
var modelFamilies = []struct {
	fragment string
	provider string
}{
	{"claude", "anthropic"},
	{"gpt", "openai"},
	{"gemini", "google"},
	{"gemma", "google"},
	{"mixtral", "mistral"},
	{"mistral", "mistral"},
	{"codestral", "mistral"},
	{"devstral", "mistral"},
	{"magistral", "mistral"},
	{"pixtral", "mistral"},
	{"llama", "meta"},
	{"deepseek", "deepseek"},
	{"qwen", "alibaba"},
	{"grok", "xai"},
	{"kimi", "moonshot"},
	{"moonshot", "moonshot"},
	{"glm", "zhipu"},
	{"nova", "amazon"},
	{"phi", "microsoft"},
	// Cohere's ids are spelled out: "command" alone is too common in config files.
	{"command-r", "cohere"},
	{"command-a", "cohere"},
	{"command-light", "cohere"},
}

// modelPrefixes are name shapes that read as a model id on their own, including
// the vendor-prefixed form Bedrock uses.
var modelPrefixes = []string{"claude", "gpt", "gemini", "o1", "o3", "text-", "amazon.", "anthropic.", "openai."}

// LooksLikeModel reports whether a string reads as a model id rather than an
// arbitrary config value. Families beyond the OpenAI, Anthropic, and Google names
// must carry a version marker straight after the family — "llama-3.3-70b" is a
// model, "llama_index" is a package. A bare family name ("mistral") is not
// enough; write the id the provider serves.
func LooksLikeModel(s string) bool {
	l := strings.ToLower(s)
	for _, p := range modelPrefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	if strings.Contains(l, "claude") || strings.Contains(l, "gpt-") {
		return true
	}
	return hasVersionedFamily(l)
}

// GuessProvider names the provider that serves a model id, or "" when no family
// matches.
func GuessProvider(model string) string {
	l := strings.ToLower(model)
	for _, f := range modelFamilies {
		if strings.Contains(l, f.fragment) {
			return f.provider
		}
	}
	if strings.HasPrefix(l, "o1") || strings.HasPrefix(l, "o3") {
		return "openai"
	}
	return ""
}

// hasVersionedFamily reports whether a lowercased name holds a known family
// followed by a version marker: a digit, or one of "-.:" leading either to a
// digit ("grok-4") or to at least two more characters ("mistral-large-latest").
func hasVersionedFamily(l string) bool {
	for _, f := range modelFamilies {
		for off := 0; off < len(l); {
			i := strings.Index(l[off:], f.fragment)
			if i < 0 {
				break
			}
			rest := l[off+i+len(f.fragment):]
			if versionMarker(rest) {
				return true
			}
			off += i + len(f.fragment)
		}
	}
	return false
}

func versionMarker(rest string) bool {
	if rest == "" {
		return false
	}
	c := rest[0]
	if c >= '0' && c <= '9' {
		return true
	}
	if c != '-' && c != '.' && c != ':' {
		return false
	}
	if len(rest) >= 3 {
		return true
	}
	return len(rest) == 2 && rest[1] >= '0' && rest[1] <= '9'
}
