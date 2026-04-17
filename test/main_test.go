package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	expected, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatal("cannot read VERSION file:", err)
	}
	ver := strings.TrimSpace(string(expected))

	out, err := exec.Command("go", "run", "../cmd/harness-factory", "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != ver {
		t.Fatalf("expected version %s, got %s", ver, strings.TrimSpace(string(out)))
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
	if resp2["result"] == nil {
		t.Error("ping: expected result")
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

// TestSessionNewResolvedModel verifies R1/R3 — session/new response exposes resolvedModel,
// alias is expanded to full ID, full ID passes through unchanged, auto picks a registry model.
func TestSessionNewResolvedModel(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expect   string // empty = just assert non-empty and bedrock/ prefix
		contains string
	}{
		{
			name:   "alias expanded to full id",
			input:  `{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read"]}},"agent":{"model":"claude-sonnet"}}}}`,
			expect: "bedrock/anthropic.claude-sonnet-4-6",
		},
		{
			name:   "full id passthrough",
			input:  `{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read"]}},"agent":{"model":"bedrock/custom-model-xyz"}}}}`,
			expect: "bedrock/custom-model-xyz",
		},
		{
			name:     "auto picks from registry",
			input:    `{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","profile":{"tools":{"fs":{"permissions":["read"]}},"agent":{"model":"auto"}}}}`,
			contains: "bedrock/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("go", "run", "../cmd/harness-factory")
			cmd.Stdin = strings.NewReader(tc.input)
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var resp map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &resp); err != nil {
				t.Fatalf("parse response: %v\n%s", err, out)
			}
			result, ok := resp["result"].(map[string]any)
			if !ok {
				t.Fatalf("no result: %v", resp)
			}
			activated, ok := result["activated"].(map[string]any)
			if !ok {
				t.Fatalf("no activated: %v", result)
			}
			got, _ := activated["resolvedModel"].(string)
			if got == "" {
				t.Fatalf("resolvedModel empty: %v", activated)
			}
			if tc.expect != "" && got != tc.expect {
				t.Errorf("resolvedModel = %q, want %q", got, tc.expect)
			}
			if tc.contains != "" && !strings.Contains(got, tc.contains) {
				t.Errorf("resolvedModel = %q, want contains %q", got, tc.contains)
			}
		})
	}
}
