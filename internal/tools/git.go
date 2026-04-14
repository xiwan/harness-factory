package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type GitTool struct{}

func NewGitTool() *GitTool { return &GitTool{} }

func (g *GitTool) Name() string { return "git" }

func (g *GitTool) Operations() []Operation {
	return []Operation{
		{Name: "status", Description: "Show git status", Parameters: []ParamDef{}},
		{Name: "diff", Description: "Show git diff", Parameters: []ParamDef{
			{Name: "args", Type: "string", Description: "Additional diff arguments (e.g. --cached, file path)", Required: false},
		}},
		{Name: "log", Description: "Show git log", Parameters: []ParamDef{
			{Name: "args", Type: "string", Description: "Log arguments (e.g. -10 --oneline)", Required: false},
		}},
		{Name: "show", Description: "Show a git object", Parameters: []ParamDef{
			{Name: "ref", Type: "string", Description: "Git ref to show (e.g. HEAD, commit hash)", Required: true},
		}},
		{Name: "commit", Description: "Create a git commit", Parameters: []ParamDef{
			{Name: "message", Type: "string", Description: "Commit message", Required: true},
		}},
		{Name: "push", Description: "Push to remote", Parameters: []ParamDef{
			{Name: "args", Type: "string", Description: "Push arguments", Required: false},
		}},
	}
}

func (g *GitTool) Execute(op string, params json.RawMessage, cwd string) (string, error) {
	var p struct {
		Args    string `json:"args"`
		Ref     string `json:"ref"`
		Message string `json:"message"`
	}
	json.Unmarshal(params, &p)

	var args []string
	switch op {
	case "status":
		args = []string{"status", "--short"}
	case "diff":
		extra, err := safeSplitArgs(p.Args)
		if err != nil {
			return "", err
		}
		args = append([]string{"diff"}, extra...)
	case "log":
		extra, err := safeSplitArgs(p.Args)
		if err != nil {
			return "", err
		}
		args = append([]string{"log"}, extra...)
		if p.Args == "" {
			args = append(args, "-20", "--oneline")
		}
	case "show":
		args = []string{"show", p.Ref}
	case "commit":
		args = []string{"commit", "-m", p.Message}
	case "push":
		extra, err := safeSplitArgs(p.Args)
		if err != nil {
			return "", err
		}
		args = append([]string{"push"}, extra...)
	default:
		return "", fmt.Errorf("git: unknown op %s", op)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", op, err, string(out))
	}
	return string(out), nil
}

func splitArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// dangerousGitFlags that could enable code execution or exfiltration.
var dangerousGitFlags = []string{
	"--exec", "--upload-pack", "--receive-pack",
	"--config", "-c", "--work-tree", "--git-dir",
}

// safeSplitArgs splits args and rejects dangerous git flags.
func safeSplitArgs(s string) ([]string, error) {
	args := splitArgs(s)
	for _, a := range args {
		lower := strings.ToLower(a)
		for _, bad := range dangerousGitFlags {
			if lower == bad || strings.HasPrefix(lower, bad+"=") {
				return nil, fmt.Errorf("blocked git flag %q", a)
			}
		}
	}
	return args, nil
}
