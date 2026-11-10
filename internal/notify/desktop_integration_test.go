package notify

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// Opt in on an interactive Windows/WSL desktop: this submits one local toast.
// A fresh identity exercises first delivery, before Windows has stored settings.
func TestDesktopFirstNotificationOnWindows(t *testing.T) {
	testDesktopOnWindows(t, Message{Title: "Agent Notify 回归测试", Body: "验证首次 Windows 通知"}, "")
}

func TestDesktopAgentBrandingOnWindows(t *testing.T) {
	for _, tc := range []struct{ agent, name string }{
		{"codex", "Codex Notify"},
		{"claude_code", "Claude Notify"},
		{"omo", "OMO Notify"},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			testDesktopOnWindows(t, Message{Agent: tc.agent, Title: tc.name + " 图标测试", Body: "验证独立名称与图标"}, tc.name)
		})
	}
}

func testDesktopOnWindows(t *testing.T, msg Message, wantName string) {
	t.Helper()
	if os.Getenv("AGENT_NOTIFY_TEST_WINDOWS") != "1" {
		t.Skip("set AGENT_NOTIFY_TEST_WINDOWS=1 on a Windows/WSL desktop")
	}
	if err := SystemStatus(); err != nil {
		t.Fatal(err)
	}
	appID := fmt.Sprintf("AgentNotify.Test.%d", time.Now().UnixNano())
	assetDir := appID + " 中文 & ' assets"
	quotedAssetDir := strings.ReplaceAll(assetDir, "'", "''")
	t.Cleanup(func() {
		script := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
try { [Windows.UI.Notifications.ToastNotificationManager]::History.Clear('` + appID + `') } catch {}
Remove-Item -LiteralPath 'HKCU:\Software\Classes\AppUserModelId\` + appID + `' -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\` + appID + `' -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) '` + quotedAssetDir + `') -Recurse -Force -ErrorAction SilentlyContinue
`
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive", "-EncodedCommand", encodeTestPowerShell(script)).Run()
	})

	sender := NewWindowsSender(func(ctx context.Context, name string, input io.Reader, args ...string) error {
		raw, err := base64.StdEncoding.DecodeString(args[len(args)-1])
		if err != nil {
			return err
		}
		units := make([]uint16, len(raw)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(raw[2*i:])
		}
		request := readDesktopTestRequest(t, input)
		request.AppID = appID
		data, err := json.Marshal(request)
		if err != nil {
			return err
		}
		script := strings.ReplaceAll(string(utf16.Decode(units)), `'AgentNotify\icons'`, `'`+quotedAssetDir+`\icons'`)
		if wantName != "" {
			script += `
$registration = Get-ItemProperty -LiteralPath $key
if ($registration.DisplayName -ne '` + wantName + `') { throw 'Incorrect notification display name' }
if (-not $registration.IconUri -or -not (Test-Path -LiteralPath $registration.IconUri)) { throw 'Notification icon missing' }
if ([Convert]::ToBase64String([IO.File]::ReadAllBytes($registration.IconUri)) -ne $request.Icon) { throw 'Cached icon differs from embedded artwork' }
if ($null -ne $xml.SelectSingleNode('/toast/visual/binding/image')) { throw 'Notification body should not contain a large logo' }
`
		}
		args[len(args)-1] = encodeTestPowerShell(script)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(data))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, output)
		}
		return nil
	})
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func encodeTestPowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(encoded[2*i:], unit)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}
