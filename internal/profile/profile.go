package profile

// Profile represents the runtime configuration passed via session/new.
type Profile struct {
	Tools         map[string]ToolConfig `json:"tools"`
	Orchestration string                `json:"orchestration"` // free | constrained | pipeline
	Resources     Resources             `json:"resources"`
	Agent         AgentConfig           `json:"agent"`
	LiteLLMURL    string                `json:"litellm_url"`
	LiteLLMAPIKey string                `json:"litellm_api_key"`
}

type ToolConfig struct {
	Permissions []string `json:"permissions,omitempty"`
	Allowlist   []string `json:"allowlist,omitempty"`
	Blocklist   []string `json:"blocklist,omitempty"`
}

type Resources struct {
	Timeout  string `json:"timeout"`
	MaxTurns int    `json:"max_turns"`
	LogLevel string `json:"log_level,omitempty"`
}

type AgentConfig struct {
	Model        string  `json:"model"`
	SystemPrompt string  `json:"system_prompt"`
	Temperature  float64 `json:"temperature"`
}

// HasTool returns true if the profile activates the given tool.
func (p *Profile) HasTool(name string) bool {
	_, ok := p.Tools[name]
	return ok
}

// HasPermission checks if a tool has a specific permission.
func (p *Profile) HasPermission(tool, perm string) bool {
	tc, ok := p.Tools[tool]
	if !ok {
		return false
	}
	for _, p := range tc.Permissions {
		if p == "all" || p == perm {
			return true
		}
	}
	return false
}

// ShellAllowed checks if a command is allowed by the shell tool config.
func (p *Profile) ShellAllowed(cmd string) bool {
	tc, ok := p.Tools["shell"]
	if !ok {
		return false
	}
	if len(tc.Allowlist) > 0 {
		for _, a := range tc.Allowlist {
			if a == cmd {
				return true
			}
		}
		return false
	}
	if len(tc.Blocklist) > 0 {
		for _, b := range tc.Blocklist {
			if b == cmd {
				return false
			}
		}
		return true
	}
	return true
}
