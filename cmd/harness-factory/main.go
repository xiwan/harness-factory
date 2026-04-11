package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/xiwan/harness-factory/internal/acp"
	"github.com/xiwan/harness-factory/internal/agent"
	"github.com/xiwan/harness-factory/internal/llm"
	"github.com/xiwan/harness-factory/internal/logger"
	"github.com/xiwan/harness-factory/internal/profile"
	"github.com/xiwan/harness-factory/internal/tools"
)

var version = "0.2.3"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
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
			transport.SendResult(req.ID, "pong")

		case "session/new":
			var params struct {
				CWD     string          `json:"cwd"`
				Profile profile.Profile `json:"profile"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				transport.SendError(req.ID, -32602, "invalid params: "+err.Error())
				continue
			}
			sessionID = fmt.Sprintf("sess_%d", os.Getpid())
			history = nil
			currentAgent = agent.New(&params.Profile, registry, transport, params.CWD, sessionID)
			logger.Infof("main", "session/new id=%s cwd=%s tools=%v", sessionID, params.CWD, toolNames(params.Profile))
			transport.SendResult(req.ID, map[string]string{"sessionId": sessionID})

		case "session/prompt":
			if currentAgent == nil {
				transport.SendError(req.ID, -32600, "no active session, call session/new first")
				continue
			}
			var params struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				transport.SendError(req.ID, -32602, "invalid params: "+err.Error())
				continue
			}
			logger.Infof("main", "session/prompt len=%d", len(params.Prompt))
			newHistory, stopReason, err := currentAgent.Run(params.Prompt, history)
			if err != nil {
				logger.Errorf("main", "agent error: %v", err)
				transport.SendError(req.ID, -32000, err.Error())
				continue
			}
			logger.Infof("main", "session/prompt done reason=%s turns=%d", stopReason, len(newHistory))
			history = newHistory
			transport.SendResult(req.ID, map[string]string{"stopReason": stopReason})

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
