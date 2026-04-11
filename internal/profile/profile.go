// Package profile handles profile parsing and tool activation.
package profile

import (
	"embed"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed bundled/*.yaml
var bundledFS embed.FS

// Profile represents the runtime configuration passed via session/new.
type Profile struct {
	Tools         map[string]ToolConfig `json:"tools" yaml:"tools"`
	Orchestration string                `json:"orchestration" yaml:"orchestration"`
	Resources     Resources             `json:"resources" yaml:"resources"`
	Agent         AgentConfig           `json:"agent" yaml:"agent"`
	LiteLLMURL    string                `json:"litellm_url" yaml:"litellm_url"`
	LiteLLMAPIKey string                `json:"litellm_api_key" yaml:"litellm_api_key"`
}

type ToolConfig struct {
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Allowlist   []string `json:"allowlist,omitempty" yaml:"allowlist,omitempty"`
	Blocklist   []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
}

type Resources struct {
	Timeout   string `json:"timeout" yaml:"timeout"`
	MaxTurns  int    `json:"max_turns" yaml:"max_turns"`
	LogLevel  string `json:"log_level,omitempty" yaml:"log_level,omitempty"`
	SkillsDir string `json:"skills_dir,omitempty" yaml:"skills_dir,omitempty"`
}

type AgentConfig struct {
	Model        string  `json:"model" yaml:"model"`
	SystemPrompt string  `json:"system_prompt" yaml:"system_prompt"`
	Temperature  float64 `json:"temperature" yaml:"temperature"`
}

// Built-in profiles loaded from embedded YAML files.
var builtinProfiles map[string]Profile

func init() {
	builtinProfiles = make(map[string]Profile)
	entries, err := bundledFS.ReadDir("bundled")
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := bundledFS.ReadFile("bundled/" + e.Name())
		if err != nil {
			continue
		}
		var p Profile
		if yaml.Unmarshal(data, &p) != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		builtinProfiles[name] = p
	}
}

// GetBuiltin returns a built-in profile by name, or the default profile if not found.
// If profilesDir is provided, external YAML files override bundled ones.
func GetBuiltin(name string, profilesDir ...string) Profile {
	// Try external dir first
	if len(profilesDir) > 0 && profilesDir[0] != "" {
		if p, ok := loadFromDir(profilesDir[0], name); ok {
			return p
		}
	}
	if p, ok := builtinProfiles[name]; ok {
		return p
	}
	if p, ok := builtinProfiles["default"]; ok {
		return p
	}
	return Profile{}
}

// BuiltinNames returns all available built-in profile names.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtinProfiles))
	for k := range builtinProfiles {
		names = append(names, k)
	}
	return names
}

func loadFromDir(dir, name string) (Profile, bool) {
	path := filepath.Join(dir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, false
	}
	var p Profile
	if yaml.Unmarshal(data, &p) != nil {
		return Profile{}, false
	}
	return p, true
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
