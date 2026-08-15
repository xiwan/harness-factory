package test

import (
	"archive/zip"
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

func packParams(name, path string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"name": name, "path": path})
	return b
}

func TestArtifactPackHappyPath(t *testing.T) {
	cwd := t.TempDir()
	ev := filepath.Join(cwd, "qa-evidence", "attempt-1")
	if err := os.MkdirAll(ev, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(ev, "trace.json"), []byte(`{"ok":true}`), 0644)
	os.WriteFile(filepath.Join(ev, "final.png"), []byte("\x89PNG-bytes"), 0644)
	os.WriteFile(filepath.Join(cwd, "qa-evidence", "qa-report.md"), []byte("# r"), 0644)

	a := tools.NewArtifactTool()
	out, err := a.Execute("pack", packParams("evidence.zip", "qa-evidence"), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "evidence.zip") || !strings.Contains(out, "3 files") {
		t.Errorf("unexpected output: %s", out)
	}
	zr, err := zip.OpenReader(filepath.Join(cwd, "outputs", "evidence.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"attempt-1/trace.json", "attempt-1/final.png", "qa-report.md"} {
		if !names[want] {
			t.Errorf("zip missing entry %q, got %v", want, names)
		}
	}
}

func TestArtifactPackRejectEscape(t *testing.T) {
	cwd := t.TempDir()
	a := tools.NewArtifactTool()
	cases := []string{"..", "../..", "/etc", "../outside"}
	for _, path := range cases {
		if _, err := a.Execute("pack", packParams("out.zip", path), cwd); err == nil {
			t.Errorf("expected rejection for path %q", path)
		}
	}
	// symlinked dir pointing outside cwd must be rejected as pack target
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cwd, "sneaky")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := a.Execute("pack", packParams("out.zip", "sneaky"), cwd); err == nil {
		t.Error("expected rejection for symlinked dir escape")
	}
}

func TestArtifactPackSkipsSymlinks(t *testing.T) {
	cwd := t.TempDir()
	ev := filepath.Join(cwd, "ev")
	os.MkdirAll(ev, 0755)
	os.WriteFile(filepath.Join(ev, "real.txt"), []byte("x"), 0644)
	victim := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(victim, []byte("SECRET"), 0644)
	if err := os.Symlink(victim, filepath.Join(ev, "leak.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	os.Symlink(filepath.Dir(victim), filepath.Join(ev, "leakdir"))

	a := tools.NewArtifactTool()
	if _, err := a.Execute("pack", packParams("ev.zip", "ev"), cwd); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(filepath.Join(cwd, "outputs", "ev.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "real.txt" {
		t.Errorf("want only real.txt, got %v", zr.File)
	}
}

func TestArtifactPackRejectBadName(t *testing.T) {
	cwd := t.TempDir()
	os.MkdirAll(filepath.Join(cwd, "ev"), 0755)
	a := tools.NewArtifactTool()
	cases := []string{"out.tar", "out.md", "noext", "../out.zip", "sub/out.zip", "out zip.zip", ""}
	for _, name := range cases {
		if _, err := a.Execute("pack", packParams(name, "ev"), cwd); err == nil {
			t.Errorf("expected rejection for name %q", name)
		}
	}
}

func TestArtifactPackSizeCap(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	cwd := t.TempDir()
	ev := filepath.Join(cwd, "ev")
	os.MkdirAll(ev, 0755)
	// two 51MiB files exceed the 100MiB uncompressed cap
	big := make([]byte, 51<<20)
	os.WriteFile(filepath.Join(ev, "a.bin"), big, 0644)
	os.WriteFile(filepath.Join(ev, "b.bin"), big, 0644)
	a := tools.NewArtifactTool()
	if _, err := a.Execute("pack", packParams("big.zip", "ev"), cwd); err == nil {
		t.Error("expected size cap rejection")
	}
	// failed pack must not leave a partial zip behind
	if _, err := os.Stat(filepath.Join(cwd, "outputs", "big.zip")); err == nil {
		t.Error("partial zip left behind after cap failure")
	}
}

func TestArtifactPackCountsTowardCap(t *testing.T) {
	cwd := t.TempDir()
	os.MkdirAll(filepath.Join(cwd, "ev"), 0755)
	os.WriteFile(filepath.Join(cwd, "ev", "x.txt"), []byte("x"), 0644)
	a := tools.NewArtifactTool()
	if _, err := a.Execute("pack", packParams("ev.zip", "ev"), cwd); err != nil {
		t.Fatal(err)
	}
	out, err := a.Execute("list", json.RawMessage(`{}`), cwd)
	if err != nil || !strings.Contains(out, "ev.zip") {
		t.Errorf("list should contain ev.zip: %q err=%v", out, err)
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
