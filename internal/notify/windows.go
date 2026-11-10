package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"github.com/flashzzw/agent-notify/internal/common"
	"strings"
	"time"
	"unicode/utf16"
)

// WindowsSender uses Windows PowerShell on native Windows and WSL.
// XML-escaped Base64 payloads never become executable code.
type WindowsSender struct{ run Runner }

func NewWindowsSender(run Runner) *WindowsSender {
	if run == nil {
		run = DefaultRunner
	}
	return &WindowsSender{run: run}
}
func (s *WindowsSender) Name() string { return "system" }
func (s *WindowsSender) Send(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var payload bytes.Buffer
	payload.WriteString(`<toast duration="long"><visual><binding template="ToastGeneric"><text>`)
	_ = xml.EscapeText(&payload, []byte(common.TruncateRunes(msg.Title, 120)))
	payload.WriteString(`</text><text>`)
	_ = xml.EscapeText(&payload, []byte(common.TruncateRunes(s.formatBody(msg), 1200)))
	payload.WriteString(`</text></binding></visual><audio src="ms-winsoundevent:Notification.Default"/></toast>`)
	script := strings.ReplaceAll(toastScript, "__PAYLOAD__", base64.StdEncoding.EncodeToString(payload.Bytes()))
	units := utf16.Encode([]rune(script))
	encoded := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(encoded[2*i:], u)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.run(ctx, powershellPath(), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded)); err != nil {
		return fmt.Errorf("Windows 通知发送失败，请运行 doctor 检查 PowerShell 与 WSL 互操作: %w", err)
	}
	return nil
}
func (s *WindowsSender) formatBody(msg Message) string {
	parts := []string{}
	if msg.Workspace != "" {
		parts = append(parts, shortenWorkspace(msg.Workspace))
	}
	if msg.Body != "" {
		parts = append(parts, msg.Body)
	}
	parts = append(parts, time.Now().Format("15:04:05"))
	return strings.Join(parts, "\n")
}

// HKCU registration gives unpackaged apps a stable name. No activation handler is installed.
const toastScript = `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$appID = 'AgentNotify.Desktop'
$key = 'HKCU:\Software\Classes\AppUserModelId\' + $appID
if (-not (Test-Path $key)) { New-Item -Path $key -Force | Out-Null }
New-ItemProperty -Path $key -Name 'DisplayName' -Value 'Agent Notify' -PropertyType String -Force | Out-Null
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('__PAYLOAD__')))
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
$toast.ExpirationTime = [DateTimeOffset]::Now.AddMinutes(5)
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appID)
if ($notifier.Setting -ne [Windows.UI.Notifications.NotificationSetting]::Enabled) { throw 'Windows notifications are disabled for Agent Notify or by policy' }
$notifier.Show($toast)
`
