package profile

import (
	"math/rand"
	"strings"
)

// ModelEntry represents a built-in model with alias and LiteLLM model ID.
type ModelEntry struct {
	Alias    string `json:"alias"`
	ModelID  string `json:"model_id"`
	Provider string `json:"provider"`
	Desc     string `json:"desc"`
}

// BuiltinModels is the registry of representative Bedrock models (SOTA as of 2026-04).
var BuiltinModels = []ModelEntry{
	{"claude-sonnet", "bedrock/anthropic.claude-sonnet-4-6", "Anthropic", "Flagship, best tool-use"},
	{"claude-opus", "bedrock/anthropic.claude-opus-4-6-v1", "Anthropic", "Most capable"},
	{"deepseek-v3", "bedrock/deepseek.v3.2", "DeepSeek", "General purpose"},
	{"kimi-k2", "bedrock/converse/moonshotai.kimi-k2.5", "Moonshot", "Multimodal reasoning"},
	{"glm-5", "bedrock/converse/zai.glm-5", "Zhipu", "Agentic engineering"},
	{"qwen3", "bedrock/converse/qwen.qwen3-235b-a22b-2507-v1:0", "Qwen", "Alibaba MoE flagship"},
	{"minimax-m2", "bedrock/converse/minimax.minimax-m2.5", "MiniMax", "Agent-native frontier"},
	{"gemma-3", "bedrock/converse/google.gemma-3-12b-it", "Google", "Lightweight open model"},
}

// ResolveModel resolves a model string: alias → model ID, "auto"/empty → random pick.
func ResolveModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		return BuiltinModels[rand.Intn(len(BuiltinModels))].ModelID
	}
	for _, m := range BuiltinModels {
		if strings.EqualFold(m.Alias, model) {
			return m.ModelID
		}
	}
	return model
}

// NextModel returns the next model in the list after the current one.
func NextModel(current string) string {
	if len(BuiltinModels) == 0 {
		return ""
	}
	for i, m := range BuiltinModels {
		if m.ModelID == current {
			return BuiltinModels[(i+1)%len(BuiltinModels)].ModelID
		}
	}
	return BuiltinModels[0].ModelID
}

// MatchModelByText tries to find a model from natural language text.
func MatchModelByText(text string) string {
	lower := strings.ToLower(text)
	for _, m := range BuiltinModels {
		if strings.Contains(lower, strings.ToLower(m.Alias)) {
			return m.ModelID
		}
		if strings.Contains(lower, strings.ToLower(m.Provider)) {
			return m.ModelID
		}
	}
	return ""
}

// ModelNames returns all available model aliases.
func ModelNames() []string {
	names := make([]string, len(BuiltinModels))
	for i, m := range BuiltinModels {
		names[i] = m.Alias
	}
	return names
}
