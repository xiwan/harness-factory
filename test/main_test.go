package test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	out, err := exec.Command("go", "run", "../cmd/harness-factory", "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "0.1.0") {
		t.Fatalf("expected version 0.1.0, got %s", string(out))
	}
}
