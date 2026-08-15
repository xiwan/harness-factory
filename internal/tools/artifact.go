package tools

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
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
// The `pack` op zips a directory from inside cwd into `outputs/` so binary
// evidence (screenshots, videos) reaches downstream without transiting the
// LLM context. The dir must resolve inside cwd; symlinks are never followed.
//
// Lifecycle of `outputs/` is the caller's (bridge's) responsibility.
type ArtifactTool struct {
	count int64 // atomic counter of successful writes this process
}

func NewArtifactTool() *ArtifactTool { return &ArtifactTool{} }

const (
	artifactDir      = "outputs"
	artifactMaxSize  = 1 << 20   // 1 MiB per text artifact
	artifactPackMax  = 100 << 20 // 100 MiB uncompressed total per pack
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
		{Name: "pack", Description: "Zip a directory from the working directory into outputs/ as a single .zip deliverable. Use for binary evidence (screenshots, videos, traces) — file contents never pass through the conversation. Max 100MB uncompressed.", Parameters: []ParamDef{
			{Name: "name", Type: "string", Description: "Output zip filename (must end in .zip). Allowed chars: letters, digits, dot, underscore, dash.", Required: true},
			{Name: "path", Type: "string", Description: "Directory to pack, relative to the working directory (e.g. \"qa-evidence\").", Required: true},
		}},
	}
}

func (a *ArtifactTool) Execute(op string, params json.RawMessage, cwd string) (string, error) {
	var p struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		Path    string `json:"path"`
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

	case "pack":
		if strings.ToLower(filepath.Ext(p.Name)) != ".zip" {
			return "", fmt.Errorf("artifact: pack name must end in .zip, got %q", p.Name)
		}
		if atomic.LoadInt64(&a.count) >= artifactMaxFiles {
			return "", fmt.Errorf("artifact: file count cap %d reached for this session", artifactMaxFiles)
		}
		abs, err := artifactResolveName(outDir, p.Name)
		if err != nil {
			return "", err
		}
		src, err := artifactResolvePackDir(cwd, p.Path)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(outDir, 0700); err != nil {
			return "", err
		}
		total, files, err := artifactPack(src, abs)
		if err != nil {
			os.Remove(abs)
			return "", err
		}
		atomic.AddInt64(&a.count, 1)
		return fmt.Sprintf("packed %d files (%d bytes uncompressed) into outputs/%s", files, total, p.Name), nil

	default:
		return "", fmt.Errorf("artifact: unknown op %s", op)
	}
}

// artifactResolvePackDir validates that the pack source is a directory that
// resolves (through any symlinks) to a location inside cwd.
func artifactResolvePackDir(cwd, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("artifact: pack path required")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("artifact: pack path must be relative to the working directory")
	}
	src, err := filepath.EvalSymlinks(filepath.Join(cwd, rel))
	if err != nil {
		return "", fmt.Errorf("artifact: pack path: %w", err)
	}
	realCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	if src != realCwd && !strings.HasPrefix(src, realCwd+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact: pack path escapes working directory")
	}
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("artifact: pack path must be an existing directory")
	}
	return src, nil
}

// artifactPack zips every regular file under src into dst. Symlinks (file or
// dir) are skipped, never followed. Uncompressed total is capped.
func artifactPack(src, dst string) (total int64, files int, err error) {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, artifactFileMode)
	if err != nil {
		return 0, 0, fmt.Errorf("artifact: open: %w", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil // best-effort, matching loader behavior
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		fi, ferr := d.Info()
		if ferr != nil || !fi.Mode().IsRegular() {
			return nil
		}
		if total+fi.Size() > artifactPackMax {
			return fmt.Errorf("artifact: pack exceeds %d MiB uncompressed cap", artifactPackMax>>20)
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return nil
		}
		w, zerr := zw.Create(filepath.ToSlash(rel))
		if zerr != nil {
			return zerr
		}
		in, oerr := os.Open(p)
		if oerr != nil {
			return nil
		}
		defer in.Close()
		n, cerr := io.Copy(w, in)
		total += n
		files++
		return cerr
	})
	if err != nil {
		zw.Close()
		return 0, 0, err
	}
	return total, files, zw.Close()
}

// artifactResolve validates a text-artifact filename (write/read): name checks
// plus the data-only extension whitelist.
func artifactResolve(outDir, name string) (string, error) {
	abs, err := artifactResolveName(outDir, name)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !artifactAllowed[ext] {
		return "", fmt.Errorf("artifact: extension %q not in allowlist", ext)
	}
	return abs, nil
}

// artifactResolveName validates the filename shape and returns the absolute
// path under outDir. Rejects path components, absolute paths, traversal, and
// bad charset. Extension policy is the caller's (write: text whitelist; pack: .zip).
func artifactResolveName(outDir, name string) (string, error) {
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
	abs := filepath.Clean(filepath.Join(outDir, name))
	cleanDir := filepath.Clean(outDir)
	// Defence in depth: the resolved path must stay strictly under outDir.
	if !strings.HasPrefix(abs, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("artifact: resolved path escapes outputs dir")
	}
	return abs, nil
}
