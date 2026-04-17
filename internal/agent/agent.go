package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

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
	systemPrompt string
	cwd          string
	sessionID    string
}

func New(p *profile.Profile, reg *tools.Registry, transport *acp.Transport, cwd, sessionID, goal string) *Agent {
	// Resolve model: alias/auto → actual model ID
	p.Agent.Model = profile.ResolveModel(p.Agent.Model)
	logger.Infof("agent", "model resolved to %s", p.Agent.Model)

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

	a := &Agent{
		profile:      p,
		registry:     reg,
		checker:      permission.NewChecker(p),
		llmClient:    llm.NewClient(p.LiteLLMURL, p.LiteLLMAPIKey),
		transport:    transport,
		skillsLoader: sl,
		cwd:          cwd,
		sessionID:    sessionID,
	}
	a.systemPrompt = a.buildSystemPrompt(goal)
	logger.Debugf("agent", "system prompt built (%d bytes)", len(a.systemPrompt))
	return a
}

// Run executes the agent loop for a single prompt.
func (a *Agent) Run(prompt string, history []llm.Message) ([]llm.Message, string, error) {
	// Check for natural language model switch
	if newModel := profile.MatchModelByText(prompt); newModel != "" {
		if newModel != a.profile.Agent.Model {
			old := a.profile.Agent.Model
			a.profile.Agent.Model = newModel
			logger.Infof("agent", "[MODEL_RESOLVED] model=%s reason=user_switch prev=%s", newModel, old)
			a.transport.SendTextChunk(a.sessionID, fmt.Sprintf("[model switched to %s]", newModel))
			a.transport.SendModelResolved(a.sessionID, newModel, "user_switch")
		}
	}

	messages := make([]llm.Message, 0, len(history)+2)
	if a.systemPrompt != "" {
		messages = append(messages, llm.Message{Role: "system", Content: a.systemPrompt})
	}
	messages = append(messages, history...)
	messages = append(messages, llm.Message{Role: "user", Content: prompt})

	toolDefs := a.registry.ActiveTools(a.profile)

	maxTurns := a.profile.Resources.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	triedModels := map[string]bool{}
	toolCallCounts := map[string]int{} // loop detection: consecutive calls per tool
	var lastToolName string

	for turn := 0; turn < maxTurns; turn++ {
		logger.Debugf("agent", "turn %d/%d calling LLM model=%s", turn+1, maxTurns, a.profile.Agent.Model)
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
			// Try fallback to next model
			triedModels[a.profile.Agent.Model] = true
			next := a.findFallback(triedModels)
			if next == "" {
				return nil, "", fmt.Errorf("llm call failed (all models exhausted): %w", err)
			}
			logger.Infof("agent", "[MODEL_RESOLVED] model=%s reason=fallback prev=%s err=%v", next, a.profile.Agent.Model, err)
			a.transport.SendTextChunk(a.sessionID, fmt.Sprintf("[model %s failed, switching to %s]", a.profile.Agent.Model, next))
			a.transport.SendModelResolved(a.sessionID, next, "fallback")
			a.profile.Agent.Model = next
			turn-- // retry this turn with new model
			continue
		}
		// Clear tried models on success
		triedModels = map[string]bool{}

		if len(resp.Choices) == 0 {
			return nil, "", fmt.Errorf("llm returned no choices")
		}

		choice := resp.Choices[0]

		// Feedback loop: retry on empty response (up to 2 retries)
		text, _ := choice.Message.Content.(string)
		if len(choice.Message.ToolCalls) == 0 && strings.TrimSpace(text) == "" {
			if turn < maxTurns-1 {
				logger.Infof("agent", "turn %d: empty reply, retrying", turn+1)
				messages = append(messages, choice.Message)
				messages = append(messages, llm.Message{Role: "user", Content: "Your response was empty. Please try again."})
				continue
			}
		}

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
			// Constraint: loop detection — same tool called 5+ times consecutively
			const maxConsecutive = 5
			if tc.Function.Name == lastToolName {
				toolCallCounts[tc.Function.Name]++
			} else {
				toolCallCounts = map[string]int{tc.Function.Name: 1}
				lastToolName = tc.Function.Name
			}
			if toolCallCounts[tc.Function.Name] >= maxConsecutive {
				logger.Infof("agent", "loop detected: %s called %d times consecutively", tc.Function.Name, toolCallCounts[tc.Function.Name])
				a.transport.SendTextChunk(a.sessionID, fmt.Sprintf("[loop detected: %s called %d times, stopping]", tc.Function.Name, toolCallCounts[tc.Function.Name]))
				return messages, "loop_detected", nil
			}

			logger.Infof("agent", "turn %d: tool call %s (id=%s)", turn+1, tc.Function.Name, tc.ID)
			a.transport.SendToolCall(a.sessionID, tc.ID, tc.Function.Name)

			result, execErr := a.executeTool(tc)

			// Tool output truncation — prevent large outputs from exhausting context window
			const maxToolOutput = 32 * 1024
			if len(result) > maxToolOutput {
				result = result[:maxToolOutput] + fmt.Sprintf("\n... (truncated, original %d bytes)", len(result))
			}

			status := "completed"
			if execErr != nil {
				status = "failed"
				result = formatToolError(tc.Function.Name, result, execErr)
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

// findFallback returns the next untried model, or "" if all exhausted.
func (a *Agent) findFallback(tried map[string]bool) string {
	// Walk the built-in list starting from current
	next := profile.NextModel(a.profile.Agent.Model)
	for i := 0; i < len(profile.BuiltinModels); i++ {
		if !tried[next] {
			return next
		}
		next = profile.NextModel(next)
	}
	return ""
}

func (a *Agent) executeTool(tc llm.ToolCall) (string, error) {
	toolName := tc.Function.Name
	if toolName == "shell_exec" {
		var p struct {
			Command string `json:"command"`
		}
		json.Unmarshal([]byte(tc.Function.Arguments), &p)
		for _, cmd := range tools.ParseCommands(p.Command) {
			if err := a.checker.Check("shell", cmd); err != nil {
				return err.Error(), err
			}
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

// SystemPrompt returns the pre-built system prompt (for testing/inspection).
func (a *Agent) SystemPrompt() string { return a.systemPrompt }

// buildSystemPrompt assembles a structured system prompt from profile, tools, cwd, skills, and goal.
// Capped at maxSystemPrompt bytes to avoid wasting context window.
func (a *Agent) buildSystemPrompt(goal string) string {
	const maxSystemPrompt = 4 * 1024

	var sb strings.Builder

	// Section 1: Role (from profile, human-written)
	if a.profile.Agent.SystemPrompt != "" {
		sb.WriteString(a.profile.Agent.SystemPrompt)
		sb.WriteString("\n\n")
	}

	// Section 2: Available tools (auto-generated from registry)
	names := a.registry.ActiveToolNames(a.profile)
	if len(names) > 0 {
		sb.WriteString("Available tools: ")
		sb.WriteString(strings.Join(names, ", "))
		sb.WriteString("\n")
	}

	// Section 3: Working directory
	if a.cwd != "" {
		sb.WriteString("Working directory: ")
		sb.WriteString(a.cwd)
		sb.WriteString("\n")
	}

	// Section 4: Skills (dynamic)
	meta := a.skillsLoader.Metadata()
	if meta != "" {
		sb.WriteString(meta)
	}

	// Section 5: Goal (optional, anchored at end for attention)
	if goal != "" {
		sb.WriteString("\nTask goal: ")
		sb.WriteString(goal)
		sb.WriteString("\n")
	}

	result := sb.String()
	if len(result) > maxSystemPrompt {
		result = result[:maxSystemPrompt] + "\n... (system prompt truncated)"
	}
	return result
}

// formatToolError returns a structured JSON error for LLM consumption.
func formatToolError(tool, detail string, err error) string {
	retryable := strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "connection")
	e := map[string]any{
		"error":     err.Error(),
		"tool":      tool,
		"detail":    detail,
		"retryable": retryable,
	}
	b, _ := json.Marshal(e)
	return string(b)
}
