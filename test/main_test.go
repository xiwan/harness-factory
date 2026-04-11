package test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	out, err := exec.Command("go", "run", "../cmd/harness-factory", "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "0.2.0") {
		t.Fatalf("expected version 0.2.0, got %s", string(out))
	}
}

func TestACPProtocol(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read"]}},"orchestration":"free","resources":{"max_turns":5},"agent":{"model":"test","system_prompt":"test"},"litellm_url":"http://localhost:4000"}}}`,
		`{"jsonrpc":"2.0","id":99,"method":"bogus"}`,
	}, "\n")

	cmd := exec.Command("go", "run", "../cmd/harness-factory")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses, got %d: %s", len(lines), string(out))
	}

	// Check initialize
	var resp1 map[string]any
	json.Unmarshal([]byte(lines[0]), &resp1)
	result1 := resp1["result"].(map[string]any)
	info := result1["agentInfo"].(map[string]any)
	if info["name"] != "harness-factory" {
		t.Errorf("initialize: expected name harness-factory, got %v", info["name"])
	}

	// Check ping
	var resp2 map[string]any
	json.Unmarshal([]byte(lines[1]), &resp2)
	if resp2["result"] != "pong" {
		t.Errorf("ping: expected pong, got %v", resp2["result"])
	}

	// Check session/new
	var resp3 map[string]any
	json.Unmarshal([]byte(lines[2]), &resp3)
	result3 := resp3["result"].(map[string]any)
	if _, ok := result3["sessionId"]; !ok {
		t.Error("session/new: missing sessionId")
	}

	// Check unknown method
	var resp4 map[string]any
	json.Unmarshal([]byte(lines[3]), &resp4)
	if resp4["error"] == nil {
		t.Error("bogus method: expected error response")
	}
}

func TestPromptWithoutSession(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"prompt":"hello"}}`
	cmd := exec.Command("go", "run", "../cmd/harness-factory")
	cmd.Stdin = strings.NewReader(input)
	out, _ := cmd.Output()

	var resp map[string]any
	json.Unmarshal(out, &resp)
	if resp["error"] == nil {
		t.Error("expected error when prompting without session")
	}
}
