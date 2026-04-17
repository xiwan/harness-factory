package profile

// Merge returns a new Profile using base as defaults, with non-zero fields from override applied on top.
func Merge(base, override Profile) Profile {
	p := base

	if len(override.Tools) > 0 {
		p.Tools = override.Tools
	}
	if override.Orchestration != "" {
		p.Orchestration = override.Orchestration
	}
	// Resources: per-field
	if override.Resources.Timeout != "" {
		p.Resources.Timeout = override.Resources.Timeout
	}
	if override.Resources.MaxTurns > 0 {
		p.Resources.MaxTurns = override.Resources.MaxTurns
	}
	if override.Resources.LogLevel != "" {
		p.Resources.LogLevel = override.Resources.LogLevel
	}
	if override.Resources.SkillsDir != "" {
		p.Resources.SkillsDir = override.Resources.SkillsDir
	}
	// Agent: per-field
	if override.Agent.Model != "" {
		p.Agent.Model = override.Agent.Model
	}
	if override.Agent.SystemPrompt != "" {
		p.Agent.SystemPrompt = override.Agent.SystemPrompt
	}
	if override.Agent.Temperature != 0 {
		p.Agent.Temperature = override.Agent.Temperature
	}
	// Bridge-injected
	if override.LiteLLMURL != "" {
		p.LiteLLMURL = override.LiteLLMURL
	}
	if override.LiteLLMAPIKey != "" {
		p.LiteLLMAPIKey = override.LiteLLMAPIKey
	}

	return p
}
