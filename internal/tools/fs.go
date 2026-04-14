package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FSTool struct{}

func NewFSTool() *FSTool { return &FSTool{} }

func (f *FSTool) Name() string { return "fs" }

func (f *FSTool) Operations() []Operation {
	return []Operation{
		{Name: "read", Description: "Read a file's contents", Parameters: []ParamDef{
			{Name: "path", Type: "string", Description: "File path (relative to cwd)", Required: true},
		}},
		{Name: "write", Description: "Write content to a file", Parameters: []ParamDef{
			{Name: "path", Type: "string", Description: "File path (relative to cwd)", Required: true},
			{Name: "content", Type: "string", Description: "Content to write", Required: true},
		}},
		{Name: "list", Description: "List directory contents", Parameters: []ParamDef{
			{Name: "path", Type: "string", Description: "Directory path (relative to cwd)", Required: true},
		}},
		{Name: "search", Description: "Search for a pattern in files", Parameters: []ParamDef{
			{Name: "path", Type: "string", Description: "Directory to search in", Required: true},
			{Name: "pattern", Type: "string", Description: "Text pattern to search for", Required: true},
		}},
	}
}

func (f *FSTool) Execute(op string, params json.RawMessage, cwd string) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	abs, err := safeResolvePath(cwd, p.Path)
	if err != nil {
		return "", err
	}

	switch op {
	case "read":
		b, err := os.ReadFile(abs)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "write":
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return "", err
		}
		return "ok", os.WriteFile(abs, []byte(p.Content), 0644)
	case "list":
		entries, err := os.ReadDir(abs)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, e := range entries {
			prefix := "  "
			if e.IsDir() {
				prefix = "d "
			}
			lines = append(lines, prefix+e.Name())
		}
		return strings.Join(lines, "\n"), nil
	case "search":
		return searchFiles(abs, p.Pattern)
	default:
		return "", fmt.Errorf("fs: unknown op %s", op)
	}
}

func resolvePath(cwd, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(cwd, p)
}

// safeResolvePath resolves a path and ensures it stays within cwd (jail).
// Returns error if the resolved path escapes cwd.
func safeResolvePath(cwd, p string) (string, error) {
	abs := resolvePath(cwd, p)
	// Clean both to normalize /../ etc.
	cleaned := filepath.Clean(abs)
	cwdClean := filepath.Clean(cwd)
	// Must be within cwd or equal to cwd
	if !strings.HasPrefix(cleaned, cwdClean+string(filepath.Separator)) && cleaned != cwdClean {
		return "", fmt.Errorf("path %q escapes working directory", p)
	}
	return cleaned, nil
}

func searchFiles(dir, pattern string) (string, error) {
	var results []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable
		}
		lines := strings.Split(string(b), "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(pattern)) {
				results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, line))
			}
		}
		return nil
	})
	return strings.Join(results, "\n"), err
}
