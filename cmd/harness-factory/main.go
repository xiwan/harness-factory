package main

import (
	"encoding/json"
	"flag"
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

var version = "0.9.2"

func main() {
	showVersion := flag.Bool("version", false, "Print version")
	showProfiles := flag.Bool("profiles", false, "List built-in profiles")
	showModels := flag.Bool("models", false, "List built-in models")
	profileName := flag.String("profile", "", "Profile name")
	profilesDir := flag.String("profiles-dir", "", "External profiles directory")
	modelFlag := flag.String("model", "", "Model alias or full ID")
	dryRun := flag.Bool("dry-run", false, "Show assembled config and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}
	if *showProfiles {
		fmt.Println("Built-in profiles:", strings.Join(profile.BuiltinNames(), ", "))
		os.Exit(0)
	}
	if *showModels {
		fmt.Println("Built-in models (alias → model_id):")
		for _, m := range profile.BuiltinModels {
			fmt.Printf("  %-18s %s  (%s — %s)\n", m.Alias, m.ModelID, m.Provider, m.Desc)
		}
		fmt.Println("\nUse \"auto\" (default) for random selection with auto-fallback.")
		os.Exit(0)
	}

	// --dry-run: show what would be assembled, then exit
	if *dryRun {
		name := *profileName
		if name == "" {
			name = "default"
		}
		p := profile.GetBuiltin(name, *profilesDir)
		reg := tools.NewRegistry()
		activated := reg.ActiveToolNames(&p)
		modelRaw := p.Agent.Model
		if *modelFlag != "" {
			modelRaw = *modelFlag
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
			// Resolve profile: load builtin as base, merge caller's overrides on top
			baseName := *profileName
			if baseName == "" {
				baseName = "default"
			}
			base := profile.GetBuiltin(baseName, *profilesDir)
			p := profile.Merge(base, params.Profile)
			logger.Infof("main", "profile base=%s merged", baseName)
			sessionID = fmt.Sprintf("sess_%d", os.Getpid())
			history = nil
			// Apply --model flag (overrides profile default)
			if *modelFlag != "" {
				p.Agent.Model = *modelFlag
			}
			// Default to "auto" if still empty
			if p.Agent.Model == "" {
				p.Agent.Model = "auto"
			}
			// Resolve model now so session/new response can expose the actual ID (R1/R3).
			// ResolveModel is idempotent for full IDs, so agent.New can safely call it again.
			resolvedModel := profile.ResolveModel(p.Agent.Model)
			p.Agent.Model = resolvedModel
			logger.Infof("main", "[MODEL_RESOLVED] model=%s profile=%s", resolvedModel, *profileName)
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
					"resolvedModel": resolvedModel,
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
