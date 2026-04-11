package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WebTool struct {
	client *http.Client
}

func NewWebTool() *WebTool {
	return &WebTool{client: &http.Client{Timeout: 30 * time.Second}}
}

func (w *WebTool) Name() string { return "web" }

func (w *WebTool) Operations() []Operation {
	return []Operation{
		{Name: "fetch", Description: "Fetch content from a URL", Parameters: []ParamDef{
			{Name: "url", Type: "string", Description: "URL to fetch", Required: true},
		}},
	}
}

func (w *WebTool) Execute(op string, params json.RawMessage, cwd string) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}

	switch op {
	case "fetch":
		resp, err := w.client.Get(p.URL)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(body)), nil
	default:
		return "", fmt.Errorf("web: unknown op %s", op)
	}
}
