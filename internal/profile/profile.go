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
	Timeout   string `json:"timeout"`
	MaxTurns  int    `json:"max_turns"`
	LogLevel  string `json:"log_level,omitempty"`
	SkillsDir string `json:"skills_dir,omitempty"`
}

type AgentConfig struct {
	Model        string  `json:"model"`
	SystemPrompt string  `json:"system_prompt"`
	Temperature  float64 `json:"temperature"`
}

// Built-in profile templates.
var BuiltinProfiles = map[string]Profile{
	"default": {
		Tools: map[string]ToolConfig{
			"fs":    {Permissions: []string{"read", "list", "search"}},
			"git":   {Permissions: []string{"status", "diff", "log"}},
			"shell": {Allowlist: []string{"echo", "date", "pwd", "ls", "cat", "grep", "find", "wc", "head", "tail"}},
		},
		Orchestration: "free",
		Resources:     Resources{Timeout: "300s", MaxTurns: 20, LogLevel: "info"},
		Agent: AgentConfig{
			SystemPrompt: "You are a helpful assistant. You can read files, check git status, and run basic commands. You cannot modify files or push changes. Be concise.",
			Temperature:  0.3,
		},
	},
	"pr-reviewer": {
		Tools: map[string]ToolConfig{
			"fs":    {Permissions: []string{"read", "list"}},
			"git":   {Permissions: []string{"diff", "log", "show"}},
			"shell": {Allowlist: []string{"pytest", "mypy", "grep", "find"}},
		},
		Orchestration: "free",
		Resources:     Resources{Timeout: "300s", MaxTurns: 20, LogLevel: "info"},
		Agent: AgentConfig{
			SystemPrompt: "You are a code reviewer. Analyze diffs, read relevant files, run linters if needed. Produce a structured review: summary, issues, suggestions. Do not modify files.",
			Temperature:  0.3,
		},
	},
	"devops": {
		Tools: map[string]ToolConfig{
			"fs":    {Permissions: []string{"all"}},
			"git":   {Permissions: []string{"all"}},
			"shell": {Allowlist: []string{"docker", "kubectl", "terraform", "make", "grep", "find", "cat", "ls"}},
			"web":   {Permissions: []string{"fetch"}},
		},
		Orchestration: "free",
		Resources:     Resources{Timeout: "600s", MaxTurns: 50, LogLevel: "info"},
		Agent: AgentConfig{
			SystemPrompt: "You are a DevOps engineer with full access to filesystem, git, shell, and web. Execute infrastructure tasks, deploy, and troubleshoot. Confirm destructive operations before executing.",
			Temperature:  0.3,
		},
	},
	"research": {
		Tools: map[string]ToolConfig{
			"fs":  {Permissions: []string{"read", "list", "search"}},
			"web": {Permissions: []string{"fetch"}},
		},
		Orchestration: "free",
		Resources:     Resources{Timeout: "300s", MaxTurns: 20, LogLevel: "info"},
		Agent: AgentConfig{
			SystemPrompt: "You are a research assistant. Read files, search codebases, and fetch web content. Summarize findings clearly. You cannot modify files or run commands.",
			Temperature:  0.3,
		},
	},
}

// GetBuiltin returns a built-in profile by name, or the default profile if not found.
func GetBuiltin(name string) Profile {
	if p, ok := BuiltinProfiles[name]; ok {
		return p
	}
	return BuiltinProfiles["default"]
}

// BuiltinNames returns all available built-in profile names.
func BuiltinNames() []string {
	names := make([]string, 0, len(BuiltinProfiles))
	for k := range BuiltinProfiles {
		names = append(names, k)
	}
	return names
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
