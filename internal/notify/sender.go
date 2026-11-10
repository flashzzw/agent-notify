package notify

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Runner func(context.Context, string, io.Reader, ...string) error

func DefaultRunner(ctx context.Context, name string, stdin io.Reader, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	prepareCommand(cmd)
	// PowerShell may print payloads on failure; report status only.
	return cmd.Run()
}
func IsWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}
func powershellPath() string {
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		return p
	}
	if IsWSL() {
		p := "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return "powershell.exe"
}

// SystemStatus checks prerequisites without sending a notification.
func SystemStatus() error {
	if runtime.GOOS != "windows" && !IsWSL() {
		return fmt.Errorf("系统通知仅支持 Windows 10/11 或 WSL")
	}
	if _, err := exec.LookPath(powershellPath()); err != nil {
		return fmt.Errorf("未找到 powershell.exe；WSL 请启用 Windows 互操作和 Windows PATH")
	}
	return nil
}
func NewSystemSender(run Runner) Sender {
	if runtime.GOOS == "windows" || IsWSL() {
		return NewWindowsSender(run)
	}
	return NewUnsupportedSender(runtime.GOOS)
}
