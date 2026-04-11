package permission

import (
	"fmt"
	"strings"

	"github.com/xiwan/harness-factory/internal/profile"
)

// Checker validates tool calls against the active profile.
type Checker struct {
	profile *profile.Profile
}

func NewChecker(p *profile.Profile) *Checker {
	return &Checker{profile: p}
}

// Check returns nil if the tool call is permitted, or an error describing why it's blocked.
func (c *Checker) Check(toolName, operation string) error {
	// Parse tool name: "fs.read" → tool="fs", op="read"
	// Also accept just "fs_read" → tool="fs", op="read"
	tool, op := parseTool(toolName, operation)

	if !c.profile.HasTool(tool) {
		return fmt.Errorf("tool %q not activated in profile", tool)
	}

	if tool == "shell" {
		if !c.profile.ShellAllowed(op) {
			return fmt.Errorf("shell command %q not in allowlist", op)
		}
		return nil
	}

	if !c.profile.HasPermission(tool, op) {
		return fmt.Errorf("%s.%s not permitted, allowed: %v", tool, op, c.profile.Tools[tool].Permissions)
	}
	return nil
}

func parseTool(name, operation string) (string, string) {
	// If operation is provided separately, use it
	if operation != "" {
		return name, operation
	}
	// Try "fs.read" format
	if parts := strings.SplitN(name, ".", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	// Try "fs_read" format
	if parts := strings.SplitN(name, "_", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return name, ""
}
