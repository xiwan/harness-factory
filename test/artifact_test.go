package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiwan/harness-factory/internal/tools"
)

func artifactParams(name, content string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"name": name, "content": content})
	return b
}

func TestArtifactWriteHappyPath(t *testing.T) {
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	out, err := a.Execute("write", artifactParams("result.md", "hello"), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "result.md") {
		t.Errorf("unexpected output: %s", out)
	}
	data, err := os.ReadFile(filepath.Join(cwd, "outputs", "result.md"))
	if err != nil || string(data) != "hello" {
		t.Errorf("file not written: %v %q", err, data)
	}
	// Mode should be 0600
	info, _ := os.Stat(filepath.Join(cwd, "outputs", "result.md"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestArtifactRejectPathComponents(t *testing.T) {
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	cases := []string{
		"../evil.md",
		"../../etc/passwd",
		"sub/result.md",
		`sub\result.md`,
		"/abs/result.md",
		"..md",
	}
	for _, name := range cases {
		_, err := a.Execute("write", artifactParams(name, "x"), cwd)
		if err == nil {
			t.Errorf("expected rejection for %q", name)
		}
	}
}

func TestArtifactRejectBadCharset(t *testing.T) {
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	cases := []string{"result file.md", "result;rm.md", "résult.md", "result\n.md", ""}
	for _, name := range cases {
		_, err := a.Execute("write", artifactParams(name, "x"), cwd)
		if err == nil {
			t.Errorf("expected rejection for %q", name)
		}
	}
}

func TestArtifactRejectBadExtension(t *testing.T) {
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	cases := []string{"script.sh", "payload.py", "tool.exe", "noext", "result.md.sh"}
	for _, name := range cases {
		_, err := a.Execute("write", artifactParams(name, "x"), cwd)
		if err == nil {
			t.Errorf("expected rejection for %q", name)
		}
	}
}

func TestArtifactRejectOversize(t *testing.T) {
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	big := strings.Repeat("x", (1<<20)+1) // 1MiB + 1 byte
	_, err := a.Execute("write", artifactParams("big.txt", big), cwd)
	if err == nil {
		t.Error("expected oversize rejection")
	}
}

func TestArtifactSymlinkEscape(t *testing.T) {
	cwd := t.TempDir()
	outDir := filepath.Join(cwd, "outputs")
	_ = os.MkdirAll(outDir, 0700)
	// target outside cwd
	victim := filepath.Join(t.TempDir(), "victim.md")
	if err := os.WriteFile(victim, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	// plant a symlink at outputs/evil.md -> victim
	if err := os.Symlink(victim, filepath.Join(outDir, "evil.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	a := tools.NewArtifactTool()
	_, err := a.Execute("write", artifactParams("evil.md", "overwritten"), cwd)
	if err == nil {
		t.Fatal("expected symlink write to fail with O_NOFOLLOW")
	}
	// victim must be untouched
	data, _ := os.ReadFile(victim)
	if string(data) != "original" {
		t.Errorf("symlink escaped: victim = %q", data)
	}
}

func TestArtifactReadAndList(t *testing.T) {
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	a.Execute("write", artifactParams("a.md", "AA"), cwd)
	a.Execute("write", artifactParams("b.json", `{"x":1}`), cwd)

	out, err := a.Execute("list", json.RawMessage(`{}`), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.md") || !strings.Contains(out, "b.json") {
		t.Errorf("list missing entries: %s", out)
	}

	out, err = a.Execute("read", artifactParams("a.md", ""), cwd)
	if err != nil || out != "AA" {
		t.Errorf("read got %q err=%v", out, err)
	}
}

func TestArtifactFileCountCap(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	for i := 0; i < 100; i++ {
		name := "f" + stringN(i) + ".txt"
		if _, err := a.Execute("write", artifactParams(name, "x"), cwd); err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}
	_, err := a.Execute("write", artifactParams("overflow.txt", "x"), cwd)
	if err == nil {
		t.Error("expected file-count cap rejection")
	}
}

func stringN(i int) string {
	// simple decimal encode (avoids importing strconv)
	if i == 0 {
		return "0"
	}
	var out []byte
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	return string(out)
}
