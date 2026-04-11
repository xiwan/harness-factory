package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/xiwan/harness-factory/internal/acp"
	"github.com/xiwan/harness-factory/internal/agent"
	"github.com/xiwan/harness-factory/internal/llm"
	"github.com/xiwan/harness-factory/internal/profile"
	"github.com/xiwan/harness-factory/internal/tools"
)

var version = "0.2.2"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		os.Exit(0)
	}

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
				os.Exit(0)
			}
			fmt.Fprintln(os.Stderr, "read error:", err)
			continue
		}

		switch req.Method {
		case "initialize":
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
			newHistory, stopReason, err := currentAgent.Run(params.Prompt, history)
			if err != nil {
				transport.SendError(req.ID, -32000, err.Error())
				continue
			}
			history = newHistory
			transport.SendResult(req.ID, map[string]string{"stopReason": stopReason})

		default:
			transport.SendError(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}
