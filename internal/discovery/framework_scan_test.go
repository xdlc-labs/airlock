package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xdlc-labs/airlock/internal/manifest"
)

func TestScanFrameworkStackPython(t *testing.T) {
	root := t.TempDir()
	code := `
from langgraph.prebuilt import create_react_agent
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(model="gpt-4o-mini")
graph = create_react_agent(llm, tools=[])
`
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "agent.py"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, mod := range m.Models {
		if mod.Model == "gpt-4o-mini" {
			found = true
			if mod.Source == "" {
				t.Fatal("expected source path")
			}
		}
	}
	if !found {
		t.Fatalf("expected gpt-4o-mini from langgraph scan, got %+v", m.Models)
	}
	foundLG := false
	for _, s := range m.Sources {
		if s.Kind == "langgraph-scan" {
			foundLG = true
		}
	}
	if !foundLG {
		t.Fatal("expected langgraph-scan source kind")
	}
}

func TestScanFrameworkStackGo(t *testing.T) {
	root := t.TempDir()
	code := `package main

type cfg struct {
	Model string
}

func main() {
	c := cfg{Model: "claude-sonnet-4-20250514"}
	_ = c
}
`
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, mod := range m.Models {
		if mod.Model == "claude-sonnet-4-20250514" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected claude model from go scan, got %+v", m.Models)
	}
}

func TestScanFrameworkStackTags(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		code  string
		kind  string
		model string
	}{
		{
			name: "anthropic sdk",
			file: "agent.py",
			code: `
import anthropic

client = anthropic.Anthropic()
resp = client.messages.create(model="claude-sonnet-4-5", max_tokens=1024)
`,
			kind:  "anthropic-sdk-scan",
			model: "claude-sonnet-4-5",
		},
		{
			name: "llamaindex",
			file: "index.py",
			code: `
from llama_index.llms.openai import OpenAI

llm = OpenAI(model="gpt-4o")
`,
			kind:  "llamaindex-scan",
			model: "gpt-4o",
		},
		{
			name: "crewai",
			file: "crew.py",
			code: `
from crewai import Agent, Crew, LLM

llm = LLM(model="openai/gpt-4o-mini")
crew = Crew(agents=[], tasks=[])
`,
			kind:  "crewai-scan",
			model: "openai/gpt-4o-mini",
		},
		{
			name: "autogen",
			file: "team.py",
			code: `
from autogen_agentchat.agents import AssistantAgent
from autogen_ext.models.openai import OpenAIChatCompletionClient

client = OpenAIChatCompletionClient(model="gpt-4.1")
`,
			kind:  "autogen-scan",
			model: "gpt-4.1",
		},
		{
			name: "vercel ai sdk",
			file: "route.ts",
			code: `
import { generateText } from "ai";
import { anthropic } from "@ai-sdk/anthropic";

const { text } = await generateText({ model: anthropic("claude-haiku-4-5") });
`,
			kind:  "vercel-ai-scan",
			model: "claude-haiku-4-5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.file), []byte(tc.code), 0o644); err != nil {
				t.Fatal(err)
			}
			m, err := Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, mod := range m.Models {
				if mod.Model == tc.model {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected model %q, got %+v", tc.model, m.Models)
			}
			if !hasSourceKind(m.Sources, tc.kind) {
				t.Fatalf("expected source kind %q, got %+v", tc.kind, m.Sources)
			}
		})
	}
}

func TestScanFrameworkStackWithoutModelString(t *testing.T) {
	root := t.TempDir()
	code := `
from crewai import Agent, Crew

crew = Crew(agents=[], tasks=[])
`
	if err := os.WriteFile(filepath.Join(root, "crew.py"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSourceKind(m.Sources, "crewai-scan") {
		t.Fatalf("expected crewai-scan source with no model attached, got %+v", m.Sources)
	}
}

func TestScanFrameworkStackDefaultsToOpenAISDK(t *testing.T) {
	root := t.TempDir()
	code := `
from openai import OpenAI

client = OpenAI()
resp = client.responses.create(model="gpt-4o", input="hi")
`
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSourceKind(m.Sources, "openai-sdk-scan") {
		t.Fatalf("expected openai-sdk-scan source kind, got %+v", m.Sources)
	}
}

func hasSourceKind(sources []manifest.Source, kind string) bool {
	for _, s := range sources {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func TestScanFrameworkStackNonOpenAIFamilies(t *testing.T) {
	root := t.TempDir()
	code := `
from crewai import Agent, Crew, LLM

planner = LLM(model="mistral-large-latest")
worker = LLM(model="llama-3.3-70b-versatile")
judge = LLM(model="deepseek-chat")
pkg = "llama_index"
`
	if err := os.WriteFile(filepath.Join(root, "crew.py"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"mistral-large-latest":    "mistral",
		"llama-3.3-70b-versatile": "meta",
		"deepseek-chat":           "deepseek",
	}
	got := map[string]string{}
	for _, mod := range m.Models {
		got[mod.Model] = mod.Provider
	}
	for model, provider := range want {
		if got[model] != provider {
			t.Errorf("model %q provider = %q, want %q (models: %+v)", model, got[model], provider, m.Models)
		}
	}
	if _, ok := got["llama_index"]; ok {
		t.Errorf("package name llama_index was read as a model: %+v", m.Models)
	}
}
