package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xiwan/harness-factory/internal/acp"
	"github.com/xiwan/harness-factory/internal/agent"
	"github.com/xiwan/harness-factory/internal/llm"
	"github.com/xiwan/harness-factory/internal/logger"
	"github.com/xiwan/harness-factory/internal/profile"
	"github.com/xiwan/harness-factory/internal/tools"
)

var version = "0.7.1"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "--profiles" {
		fmt.Println("Built-in profiles:", strings.Join(profile.BuiltinNames(), ", "))
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "--models" {
		fmt.Println("Built-in models (alias → model_id):")
		for _, m := range profile.BuiltinModels {
			fmt.Printf("  %-18s %s  (%s — %s)\n", m.Alias, m.ModelID, m.Provider, m.Desc)
		}
		fmt.Println("\nUse \"auto\" (default) for random selection with auto-fallback.")
		os.Exit(0)
	}

	// Parse flags
	var profileName, profilesDir, modelFlag string
	var dryRun bool
	for i, arg := range os.Args[1:] {
		if arg == "--profile" && i+1 < len(os.Args)-1 {
			profileName = os.Args[i+2]
		}
		if arg == "--profiles-dir" && i+1 < len(os.Args)-1 {
			profilesDir = os.Args[i+2]
		}
		if arg == "--model" && i+1 < len(os.Args)-1 {
			modelFlag = os.Args[i+2]
		}
		if arg == "--dry-run" {
			dryRun = true
		}
	}

	// --dry-run: show what would be assembled, then exit
	if dryRun {
		name := profileName
		if name == "" {
			name = "default"
		}
		p := profile.GetBuiltin(name, profilesDir)
		reg := tools.NewRegistry()
		activated := reg.ActiveToolNames(&p)
		modelRaw := p.Agent.Model
		if modelFlag != "" {
			modelRaw = modelFlag
		}
		if modelRaw == "" {
			modelRaw = "auto"
		}
		resolved := profile.ResolveModel(modelRaw)
		out := map[string]any{
			"profile":   name,
			"tools":     activated,
			"toolCount": len(activated),
			"orchestration": p.Orchestration,
			"resources": p.Resources,
			"agent": map[string]any{
				"model":          modelRaw,
				"model_resolved": resolved,
				"temperature":    p.Agent.Temperature,
			},
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		os.Exit(0)
	}

	logger.Init()
	logger.Infof("main", "harness-factory %s starting", version)

	transport := acp.NewTransport(os.Stdin, os.Stdout)
	registry := tools.NewRegistry()

	var (
		currentAgent *agent.Agent
		sessionID    string
		history      []llm.Message
	)

	for {
		req, err := transport.ReadRequest()
		if err != nil {
			if err == io.EOF {
				logger.Info("main", "stdin closed, exiting")
				os.Exit(0)
			}
			logger.Errorf("main", "read error: %v", err)
			continue
		}

		logger.Debugf("main", "← %s (id=%v)", req.Method, req.ID)

		switch req.Method {
		case "initialize":
			logger.Info("main", "initialize")
			transport.SendResult(req.ID, map[string]any{
				"agentInfo":    map[string]string{"name": "harness-factory", "version": version},
				"capabilities": map[string]any{},
			})

		case "ping":
			transport.SendResult(req.ID, map[string]any{})

		case "session/new":
			var params struct {
				CWD        string          `json:"cwd"`
				Goal       string          `json:"goal"`
				Profile    profile.Profile `json:"profile"`
				MCPServers json.RawMessage `json:"mcpServers"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				transport.SendError(req.ID, -32602, "invalid params: "+err.Error())
				continue
			}
			// Resolve profile: explicit > --profile flag > default
			p := params.Profile
			if len(p.Tools) == 0 {
				if profileName != "" {
					p = profile.GetBuiltin(profileName, profilesDir)
					logger.Infof("main", "using --profile %s", profileName)
				} else {
					p = profile.GetBuiltin("default", profilesDir)
					logger.Info("main", "no profile provided, using default")
				}
				// Preserve litellm config from params if set
				if params.Profile.LiteLLMURL != "" {
					p.LiteLLMURL = params.Profile.LiteLLMURL
				}
				if params.Profile.LiteLLMAPIKey != "" {
					p.LiteLLMAPIKey = params.Profile.LiteLLMAPIKey
				}
				if params.Profile.Agent.Model != "" {
					p.Agent.Model = params.Profile.Agent.Model
				}
			}
			sessionID = fmt.Sprintf("sess_%d", os.Getpid())
			history = nil
			// Apply --model flag (overrides profile default)
			if modelFlag != "" {
				p.Agent.Model = modelFlag
			}
			// Default to "auto" if still empty
			if p.Agent.Model == "" {
				p.Agent.Model = "auto"
			}
			currentAgent = agent.New(&p, registry, transport, params.CWD, sessionID, params.Goal)
			if p.Resources.LogLevel != "" {
				logger.SetLevel(p.Resources.LogLevel)
			}
			logger.Infof("main", "session/new id=%s cwd=%s tools=%v", sessionID, params.CWD, toolNames(p))
			transport.SendResult(req.ID, map[string]any{
				"sessionId": sessionID,
				"activated": map[string]any{
					"tools":         registry.ActiveToolNames(&p),
					"toolCount":     len(registry.ActiveToolNames(&p)),
					"orchestration": p.Orchestration,
				},
			})

		case "session/prompt":
			if currentAgent == nil {
				transport.SendError(req.ID, -32600, "no active session, call session/new first")
				continue
			}
			var params struct {
				SessionID string          `json:"sessionId"`
				Prompt    json.RawMessage `json:"prompt"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				transport.SendError(req.ID, -32602, "invalid params: "+err.Error())
				continue
			}
			prompt := parsePrompt(params.Prompt)
			logger.Infof("main", "session/prompt len=%d", len(prompt))
			newHistory, stopReason, err := currentAgent.Run(prompt, history)
			if err != nil {
				logger.Errorf("main", "agent error: %v", err)
				transport.SendError(req.ID, -32000, err.Error())
				continue
			}
			logger.Infof("main", "session/prompt done reason=%s turns=%d", stopReason, len(newHistory))
			history = newHistory
			transport.SendResult(req.ID, map[string]string{
				"sessionId":  sessionID,
				"stopReason": stopReason,
			})

		case "session/cancel":
			// Notification, no response required
			logger.Info("main", "session/cancel received")

		default:
			logger.Debugf("main", "unknown method: %s", req.Method)
			transport.SendError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

func toolNames(p profile.Profile) []string {
	var names []string
	for k := range p.Tools {
		names = append(names, k)
	}
	return names
}

// parsePrompt handles ACP standard array format and plain string:
//
//	[{"type":"text","text":"hello"}]  → "hello"
//	"hello"                           → "hello"
func parsePrompt(raw json.RawMessage) string {
	// Try ACP array format first (standard)
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil && len(parts) > 0 {
		var sb strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	// Fallback: plain string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
