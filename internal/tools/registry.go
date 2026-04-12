package tools

import (
	"encoding/json"
	"fmt"

	"github.com/xiwan/harness-factory/internal/profile"
)

// Tool defines the interface all core tools implement.
type Tool interface {
	Name() string
	Operations() []Operation
	Execute(op string, params json.RawMessage, cwd string) (string, error)
}

// Operation describes a single tool operation for LLM tool definitions.
type Operation struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  []ParamDef `json:"parameters"`
}

type ParamDef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ToolDefinition is the format sent to LLM as a function/tool definition.
type ToolDefinition struct {
	Type     string         `json:"type"`
	Function FunctionDef    `json:"function"`
}

type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Registry holds all tools and filters by profile.
type Registry struct {
	all map[string]Tool
}

func NewRegistry() *Registry {
	r := &Registry{all: make(map[string]Tool)}
	// Register all core tools
	for _, t := range []Tool{NewFSTool(), NewGitTool(), NewShellTool(), NewWebTool()} {
		r.all[t.Name()] = t
	}
	return r
}

// ActiveTools returns tool definitions for tools activated by the profile.
func (r *Registry) ActiveTools(p *profile.Profile) []ToolDefinition {
	var defs []ToolDefinition
	for name, tool := range r.all {
		if !p.HasTool(name) {
			continue
		}
		for _, op := range tool.Operations() {
			// Filter by permission
			if name == "shell" {
				// Shell always exposes exec if activated
			} else if !p.HasPermission(name, op.Name) {
				continue
			}

			funcName := name + "_" + op.Name
			params := buildParamsSchema(op.Parameters)
			defs = append(defs, ToolDefinition{
				Type: "function",
				Function: FunctionDef{
					Name:        funcName,
					Description: op.Description,
					Parameters:  params,
				},
			})
		}
	}
	return defs
}

// ActiveToolNames returns the list of activated tool function names for a profile.
func (r *Registry) ActiveToolNames(p *profile.Profile) []string {
	var names []string
	for _, d := range r.ActiveTools(p) {
		names = append(names, d.Function.Name)
	}
	return names
}

// Execute runs a tool call. toolName is like "fs_read".
func (r *Registry) Execute(toolName string, params json.RawMessage, cwd string) (string, error) {
	// Parse "fs_read" → tool="fs", op="read"
	for name, tool := range r.all {
		for _, op := range tool.Operations() {
			if name+"_"+op.Name == toolName {
				return tool.Execute(op.Name, params, cwd)
			}
		}
	}
	return "", fmt.Errorf("unknown tool: %s", toolName)
}

func buildParamsSchema(params []ParamDef) json.RawMessage {
	props := make(map[string]any)
	var required []string
	for _, p := range params {
		props[p.Name] = map[string]string{"type": p.Type, "description": p.Description}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	b, _ := json.Marshal(schema)
	return b
}
