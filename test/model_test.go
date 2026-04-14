package test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/xiwan/harness-factory/internal/profile"
)

func TestModelRegistry(t *testing.T) {
	models := profile.BuiltinModels
	if len(models) == 0 {
		t.Fatal("BuiltinModels is empty")
	}

	// Check no duplicate aliases
	seen := map[string]bool{}
	for _, m := range models {
		if seen[m.Alias] {
			t.Errorf("duplicate alias: %s", m.Alias)
		}
		seen[m.Alias] = true
		if m.ModelID == "" {
			t.Errorf("alias %s has empty ModelID", m.Alias)
		}
		if !strings.HasPrefix(m.ModelID, "bedrock/") {
			t.Errorf("alias %s ModelID %s missing bedrock/ prefix", m.Alias, m.ModelID)
		}
	}
}

func TestResolveModelAlias(t *testing.T) {
	for _, m := range profile.BuiltinModels {
		got := profile.ResolveModel(m.Alias)
		if got != m.ModelID {
			t.Errorf("ResolveModel(%q) = %q, want %q", m.Alias, got, m.ModelID)
		}
	}
}

func TestResolveModelAuto(t *testing.T) {
	valid := map[string]bool{}
	for _, m := range profile.BuiltinModels {
		valid[m.ModelID] = true
	}
	// Run 20 times, all results must be in the registry
	for i := 0; i < 20; i++ {
		got := profile.ResolveModel("auto")
		if !valid[got] {
			t.Errorf("ResolveModel(auto) returned unknown model: %s", got)
		}
	}
	// Empty string should also auto-select
	got := profile.ResolveModel("")
	if !valid[got] {
		t.Errorf("ResolveModel(\"\") returned unknown model: %s", got)
	}
}

func TestResolveModelPassthrough(t *testing.T) {
	custom := "bedrock/anthropic.claude-v2"
	got := profile.ResolveModel(custom)
	if got != custom {
		t.Errorf("ResolveModel(%q) = %q, want passthrough", custom, got)
	}
}

func TestNextModel(t *testing.T) {
	models := profile.BuiltinModels
	if len(models) < 2 {
		t.Skip("need at least 2 models")
	}
	// NextModel should return the next one in list
	next := profile.NextModel(models[0].ModelID)
	if next != models[1].ModelID {
		t.Errorf("NextModel(%s) = %s, want %s", models[0].ModelID, next, models[1].ModelID)
	}
	// Last wraps to first
	last := models[len(models)-1]
	next = profile.NextModel(last.ModelID)
	if next != models[0].ModelID {
		t.Errorf("NextModel(last) = %s, want %s", next, models[0].ModelID)
	}
	// Unknown model returns first
	next = profile.NextModel("bedrock/unknown-model")
	if next != models[0].ModelID {
		t.Errorf("NextModel(unknown) = %s, want %s", next, models[0].ModelID)
	}
}

func TestMatchModelByText(t *testing.T) {
	tests := []struct {
		text  string
		want  string
	}{
		{"use claude-sonnet please", "bedrock/anthropic.claude-sonnet-4-6"},
		{"换个qwen3模型", "bedrock/converse/qwen.qwen3-235b-a22b-2507-v1:0"},
		{"try Anthropic", "bedrock/anthropic.claude-sonnet-4-6"}, // first Anthropic match
		{"用deepseek", "bedrock/deepseek.v3.2"},
		{"random unrelated text", ""},
	}
	for _, tt := range tests {
		got := profile.MatchModelByText(tt.text)
		if got != tt.want {
			t.Errorf("MatchModelByText(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestModelsFlag(t *testing.T) {
	out, err := exec.Command("go", "run", "../cmd/harness-factory", "--models").Output()
	if err != nil {
		t.Fatal(err)
	}
	output := string(out)
	// Should list all aliases
	for _, m := range profile.BuiltinModels {
		if !strings.Contains(output, m.Alias) {
			t.Errorf("--models output missing alias %s", m.Alias)
		}
		if !strings.Contains(output, m.ModelID) {
			t.Errorf("--models output missing model ID %s", m.ModelID)
		}
	}
}

func TestDryRunModelResolution(t *testing.T) {
	out, err := exec.Command("go", "run", "../cmd/harness-factory", "--profile", "reader", "--model", "glm-5", "--dry-run").Output()
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal("invalid JSON:", err)
	}
	agent := result["agent"].(map[string]any)
	if agent["model"] != "glm-5" {
		t.Errorf("model = %v, want glm-5", agent["model"])
	}
	if agent["model_resolved"] != "bedrock/converse/zai.glm-5" {
		t.Errorf("model_resolved = %v, want bedrock/converse/zai.glm-5", agent["model_resolved"])
	}
}
