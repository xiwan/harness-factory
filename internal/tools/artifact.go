package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
)

// ArtifactTool writes data-type files to `<cwd>/outputs/` with a tightened safety envelope:
// - LLM passes only `name` (filename), never a path; the `outputs/` prefix is added here.
// - Filename charset is restricted and extension must be in a data-only whitelist.
// - `O_NOFOLLOW` blocks symlink escape; mode 0600 limits cross-UID leakage.
// - Per-file size and per-process file-count caps bound DoS.
//
// Lifecycle of `outputs/` is the caller's (bridge's) responsibility.
type ArtifactTool struct {
	count int64 // atomic counter of successful writes this process
}

func NewArtifactTool() *ArtifactTool { return &ArtifactTool{} }

const (
	artifactDir      = "outputs"
	artifactMaxSize  = 1 << 20 // 1 MiB
	artifactMaxFiles = 100
	artifactFileMode = 0600
)

var (
	artifactNameRe  = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	artifactAllowed = map[string]bool{
		".md": true, ".txt": true, ".json": true, ".yaml": true,
		".yml": true, ".csv": true, ".log": true, ".html": true,
	}
)

func (*ArtifactTool) Name() string { return "artifact" }

func (*ArtifactTool) Operations() []Operation {
	return []Operation{
		{Name: "write", Description: "Write a data artifact (.md/.txt/.json/.yaml/.yml/.csv/.log/.html) for downstream agents to read. Max 1MB per file, 100 files per session.", Parameters: []ParamDef{
			{Name: "name", Type: "string", Description: "Filename only (no path). Allowed chars: letters, digits, dot, underscore, dash.", Required: true},
			{Name: "content", Type: "string", Description: "File content (UTF-8)", Required: true},
		}},
		{Name: "read", Description: "Read an artifact previously written in this session.", Parameters: []ParamDef{
			{Name: "name", Type: "string", Description: "Filename only", Required: true},
		}},
		{Name: "list", Description: "List artifacts written so far.", Parameters: []ParamDef{}},
	}
}

func (a *ArtifactTool) Execute(op string, params json.RawMessage, cwd string) (string, error) {
	var p struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(params, &p)

	outDir := filepath.Join(cwd, artifactDir)

	switch op {
	case "list":
		if err := os.MkdirAll(outDir, 0700); err != nil {
			return "", err
		}
		entries, err := os.ReadDir(outDir)
		if err != nil {
			return "", err
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		return strings.Join(names, "\n"), nil

	case "read":
		abs, err := artifactResolve(outDir, p.Name)
		if err != nil {
			return "", err
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return "", err
		}
		return string(b), nil

	case "write":
		if len(p.Content) > artifactMaxSize {
			return "", fmt.Errorf("artifact: content %d bytes exceeds max %d", len(p.Content), artifactMaxSize)
		}
		if atomic.LoadInt64(&a.count) >= artifactMaxFiles {
			return "", fmt.Errorf("artifact: file count cap %d reached for this session", artifactMaxFiles)
		}
		abs, err := artifactResolve(outDir, p.Name)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(outDir, 0700); err != nil {
			return "", err
		}
		// O_NOFOLLOW prevents writing through an existing symlink that points outside outDir.
		f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, artifactFileMode)
		if err != nil {
			return "", fmt.Errorf("artifact: open: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString(p.Content); err != nil {
			return "", err
		}
		atomic.AddInt64(&a.count, 1)
		return fmt.Sprintf("written %d bytes to outputs/%s", len(p.Content), p.Name), nil

	default:
		return "", fmt.Errorf("artifact: unknown op %s", op)
	}
}

// artifactResolve validates the filename and returns the absolute path under outDir.
// Rejects path components, absolute paths, traversal, bad charset, and disallowed extensions.
func artifactResolve(outDir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("artifact: name required")
	}
	if strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) {
		return "", fmt.Errorf("artifact: name must be a filename, not a path: %q", name)
	}
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("artifact: name must not contain '..'")
	}
	if !artifactNameRe.MatchString(name) {
		return "", fmt.Errorf("artifact: name %q contains disallowed characters", name)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !artifactAllowed[ext] {
		return "", fmt.Errorf("artifact: extension %q not in allowlist", ext)
	}
	abs := filepath.Clean(filepath.Join(outDir, name))
	cleanDir := filepath.Clean(outDir)
	// Defence in depth: the resolved path must stay strictly under outDir.
	if !strings.HasPrefix(abs, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact: resolved path escapes outputs dir")
	}
	return abs, nil
}
