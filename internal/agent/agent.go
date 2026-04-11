package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/xiwan/harness-factory/internal/acp"
	"github.com/xiwan/harness-factory/internal/llm"
	"github.com/xiwan/harness-factory/internal/logger"
	"github.com/xiwan/harness-factory/internal/permission"
	"github.com/xiwan/harness-factory/internal/profile"
	"github.com/xiwan/harness-factory/internal/skills"
	"github.com/xiwan/harness-factory/internal/tools"
)

type Agent struct {
	profile      *profile.Profile
	registry     *tools.Registry
	checker      *permission.Checker
	llmClient    *llm.Client
	transport    *acp.Transport
	skillsLoader *skills.Loader
	cwd          string
	sessionID    string
}

func New(p *profile.Profile, reg *tools.Registry, transport *acp.Transport, cwd, sessionID string) *Agent {
	// Resolve skills directory
	skillsDir := p.Resources.SkillsDir
	if skillsDir == "" {
		skillsDir = "skills"
	}
	if !filepath.IsAbs(skillsDir) {
		skillsDir = filepath.Join(cwd, skillsDir)
	}
	sl := skills.NewLoader(skillsDir)
	if sl.Count() > 0 {
		logger.Infof("agent", "loaded %d skills from %s: %v", sl.Count(), skillsDir, sl.Names())
	}
	sl.StartWatcher()

	return &Agent{
		profile:      p,
		registry:     reg,
		checker:      permission.NewChecker(p),
		llmClient:    llm.NewClient(p.LiteLLMURL, p.LiteLLMAPIKey),
		transport:    transport,
		skillsLoader: sl,
		cwd:          cwd,
		sessionID:    sessionID,
	}
}

// Run executes the agent loop for a single prompt.
func (a *Agent) Run(prompt string, history []llm.Message) ([]llm.Message, string, error) {
	messages := make([]llm.Message, 0, len(history)+2)
	sysPrompt := a.profile.Agent.SystemPrompt + a.skillsLoader.Metadata()
	if sysPrompt != "" {
		messages = append(messages, llm.Message{Role: "system", Content: sysPrompt})
	}
	messages = append(messages, history...)
	messages = append(messages, llm.Message{Role: "user", Content: prompt})

	toolDefs := a.registry.ActiveTools(a.profile)

	maxTurns := a.profile.Resources.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	for turn := 0; turn < maxTurns; turn++ {
		logger.Debugf("agent", "turn %d/%d calling LLM", turn+1, maxTurns)
		req := &llm.ChatRequest{
			Model:       a.profile.Agent.Model,
			Messages:    messages,
			Temperature: a.profile.Agent.Temperature,
		}
		if len(toolDefs) > 0 {
			req.Tools = toolDefs
		}

		resp, err := a.llmClient.Chat(req)
		if err != nil {
			return nil, "", fmt.Errorf("llm call failed: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, "", fmt.Errorf("llm returned no choices")
		}

		choice := resp.Choices[0]
		messages = append(messages, choice.Message)

		// No tool calls → done
		if len(choice.Message.ToolCalls) == 0 {
			logger.Infof("agent", "turn %d: LLM done (no tool calls)", turn+1)
			text, _ := choice.Message.Content.(string)
			a.transport.SendTextChunk(a.sessionID, text)
			return messages, "end_turn", nil
		}

		// Execute tool calls
		for _, tc := range choice.Message.ToolCalls {
			logger.Infof("agent", "turn %d: tool call %s (id=%s)", turn+1, tc.Function.Name, tc.ID)
			a.transport.SendToolCall(a.sessionID, tc.ID, tc.Function.Name)

			result, execErr := a.executeTool(tc)

			status := "completed"
			if execErr != nil {
				status = "failed"
				logger.Errorf("agent", "tool %s error: %v", tc.Function.Name, execErr)
			}
			a.transport.SendToolCallUpdate(a.sessionID, tc.ID, tc.Function.Name, status, result)

			messages = append(messages, llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return messages, "max_turns", nil
}

func (a *Agent) executeTool(tc llm.ToolCall) (string, error) {
	toolName := tc.Function.Name
	if toolName == "shell_exec" {
		var p struct {
			Command string `json:"command"`
		}
		json.Unmarshal([]byte(tc.Function.Arguments), &p)
		baseCmd := tools.BaseCommand(p.Command)
		if err := a.checker.Check("shell", baseCmd); err != nil {
			return err.Error(), err
		}
	} else {
		if err := a.checker.Check(toolName, ""); err != nil {
			return err.Error(), err
		}
	}

	result, err := a.registry.Execute(toolName, json.RawMessage(tc.Function.Arguments), a.cwd)
	if err != nil {
		return err.Error(), err
	}
	return result, nil
}
