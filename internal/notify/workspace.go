package notify

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspace returns a displayable workspace path.
//
// Hook stdin payloads on Windows sometimes deliver paths whose non-ASCII
// segments were replaced with '?' (classic ANSI round-trip loss). When the
// payload path is empty or corrupted, fall back to agent-provided env vars
// and the process working directory (Go uses wide Win32 APIs, so Getwd is
// reliable for CJK paths).
func ResolveWorkspace(fromPayload string) string {
	if isUsableWorkspace(fromPayload) {
		return filepath.Clean(fromPayload)
	}
	for _, key := range []string{
		"GROK_WORKSPACE_ROOT",
		"CLAUDE_PROJECT_DIR",
		"PWD",
	} {
		if v := os.Getenv(key); isUsableWorkspace(v) {
			return filepath.Clean(v)
		}
	}
	if wd, err := os.Getwd(); err == nil && isUsableWorkspace(wd) {
		return filepath.Clean(wd)
	}
	// Last resort: keep the original payload even if imperfect.
	return strings.TrimSpace(fromPayload)
}

func isUsableWorkspace(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	// '?' is not a legal path character on Windows; its presence almost always
	// means the original CJK characters were lost during an ANSI conversion.
	if strings.Contains(p, "?") {
		return false
	}
	return true
}

// projectBase returns the last segment of a workspace path, for toast
// attribution and template {project}. Example: E:\学习ai编程项目\agent-notify → agent-notify
func projectBase(ws string) string {
	ws = strings.TrimSpace(ws)
	if ws == "" {
		return ""
	}
	normalized := strings.ReplaceAll(filepath.ToSlash(ws), `\`, "/")
	normalized = strings.TrimRight(normalized, "/")
	if normalized == "" || strings.HasSuffix(normalized, ":") {
		// Bare drive roots like "C:" carry no project segment.
		return ""
	}
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		return normalized[idx+1:]
	}
	return normalized
}
