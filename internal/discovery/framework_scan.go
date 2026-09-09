package discovery

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

// ponytail: regex + go/ast heuristics, not full Python/TS AST; upgrade path = tree-sitter per language.

var (
	openAIModelRE  = regexp.MustCompile(`(?i)(?:model|model_name|model_id)\s*[=:]\s*["']([a-z0-9._:/-]+)["']`)
	chatOpenAIRE   = regexp.MustCompile(`(?i)ChatOpenAI\s*\([^)]*model\s*[=:]\s*["']([a-z0-9._:/-]+)["']`)
	openAIClientRE = regexp.MustCompile(`(?i)OpenAI\s*\([^)]*model\s*[=:]\s*["']([a-z0-9._:/-]+)["']`)
	// Vercel AI SDK and friends name the model positionally: openai('gpt-4o'), anthropic("claude-...").
	providerCallRE = regexp.MustCompile(`(?i)\b(?:openai|anthropic|google|vertex|bedrock|azure|mistral|groq|xai|deepseek)\s*\(\s*["']([a-z0-9._:/-]+)["']`)
)

// frameworkPatterns tags a source file with the agent framework it uses. Order is
// precedence: an orchestration framework wins over the raw SDK it calls underneath.
// Anything that yields a model but matches nothing here is reported as openai-sdk.
var frameworkPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"langgraph", regexp.MustCompile(`(?i)(?:from|import)\s+langgraph[\w.]*|langgraph\.|@langchain/langgraph|create_react_agent\s*\(|StateGraph\s*\(`)},
	{"llamaindex", regexp.MustCompile(`(?i)(?:from|import)\s+llama_index[\w.]*|llama_index\.|@llamaindex/|["']llamaindex["']|VectorStoreIndex\s*\(`)},
	{"crewai", regexp.MustCompile(`(?i)(?:from|import)\s+crewai[\w.]*|crewai\.|\bCrew\s*\(`)},
	{"autogen", regexp.MustCompile(`(?i)(?:from|import)\s+autogen[\w.]*|autogen_agentchat|autogen_core|autogen\.|AssistantAgent\s*\(|OpenAIChatCompletionClient\s*\(`)},
	{"vercel-ai", regexp.MustCompile(`(?i)from\s+["']ai["']|["']@ai-sdk/[\w-]+["']|\bgenerateText\s*\(|\bstreamText\s*\(|\bgenerateObject\s*\(|\bstreamObject\s*\(`)},
	{"anthropic-sdk", regexp.MustCompile(`(?i)(?:from|import)\s+anthropic[\w.]*|@anthropic-ai/sdk|anthropic-sdk-go|anthropic\.messages|ChatAnthropic\s*\(|new\s+Anthropic\s*\(`)},
}

const defaultFramework = "openai-sdk"

// scanFrameworkStack discovers models and agent frameworks in source trees.
func scanFrameworkStack(root string, m *manifest.Manifest) error {
	seen := map[string]bool{}
	for _, x := range m.Models {
		seen[x.Model] = true
	}
	firstSeenAt := map[string]string{}
	var order []string
	emitted := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".airlock", "vendor", "__pycache__", ".venv", "venv":
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".py", ".ts", ".tsx", ".js", ".jsx", ".go":
		default:
			return nil
		}
		framework, models := scanSourceFile(path, ext)
		if framework != "" {
			if _, ok := firstSeenAt[framework]; !ok {
				firstSeenAt[framework] = rel(root, path)
				order = append(order, framework)
			}
		}

		for _, model := range models {
			if kind := addScannedModel(m, seen, model, path, root, framework); kind != "" {
				emitted[kind] = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// A framework with no model string of its own still changes the blast radius,
	// so record it even when nothing above attached a model to it.
	for _, name := range order {
		kind := name + "-scan"
		if emitted[kind] {
			continue
		}
		m.Sources = append(m.Sources, manifest.Source{Kind: kind, Path: firstSeenAt[name], Detail: name})
	}
	return nil
}

// scanSourceFile reads one source file and reports the framework it uses and the
// model strings it names. An unreadable file is skipped, not fatal.
func scanSourceFile(path, ext string) (string, []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	if ext == ".go" {
		return detectFramework(string(data)), goModels(path, data)
	}
	return detectFramework(string(data)), textModels(string(data))
}

func detectFramework(text string) string {
	for _, fw := range frameworkPatterns {
		if fw.re.MatchString(text) {
			return fw.name
		}
	}
	return ""
}

func textModels(text string) []string {
	var models []string
	for _, re := range []*regexp.Regexp{openAIModelRE, chatOpenAIRE, openAIClientRE, providerCallRE} {
		for _, match := range re.FindAllStringSubmatch(text, -1) {
			if len(match) > 1 && manifest.LooksLikeModel(match[1]) {
				models = append(models, match[1])
			}
		}
	}
	return unique(models)
}

// ponytail: Go SDKs that name models with exported constants (anthropic.ModelClaude...)
// stay invisible here; only string literals in Model-ish fields are read.
func goModels(path string, src []byte) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil
	}
	var models []string
	ast.Inspect(f, func(n ast.Node) bool {
		be, ok := n.(*ast.BasicLit)
		if !ok || be.Kind != token.STRING {
			return true
		}
		val := strings.Trim(be.Value, `"`)
		if !manifest.LooksLikeModel(val) {
			return true
		}
		// ponytail: only strings near Model field names in Go struct literals
		if parentIsModelField(fset, f, be) {
			models = append(models, val)
		}
		return true
	})
	return unique(models)
}

func parentIsModelField(fset *token.FileSet, f *ast.File, lit *ast.BasicLit) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok || kv.Value != lit {
			return true
		}
		if ident, ok := kv.Key.(*ast.Ident); ok {
			name := strings.ToLower(ident.Name)
			if name == "model" || strings.HasSuffix(name, "model") {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// addScannedModel records a model and returns the source kind it wrote, or "" if
// the model was already known.
func addScannedModel(m *manifest.Manifest, seen map[string]bool, model, path, root, framework string) string {
	if seen[model] {
		return ""
	}
	seen[model] = true
	id := "model-" + slug(model)
	if hasModel(m, id) {
		return ""
	}
	if framework == "" {
		framework = defaultFramework
	}
	kind := framework + "-scan"
	detail := rel(root, path)
	provider := manifest.GuessProvider(model)
	m.Models = append(m.Models, manifest.Model{
		ID: id, Provider: provider, Model: model,
		ContentHash: manifest.HashString(provider + "|" + model),
		Source:      detail, Confidence: "medium",
	})
	m.Sources = append(m.Sources, manifest.Source{
		Kind: kind, Path: detail, Detail: model,
	})
	return kind
}
