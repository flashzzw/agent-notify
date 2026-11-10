package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
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
	identity := identityForAgent(msg.Agent)
	var payload bytes.Buffer
	payload.WriteString(`<toast duration="long"><visual><binding template="ToastGeneric"><text>`)
	_ = xml.EscapeText(&payload, []byte(common.TruncateRunes(msg.Title, 120)))
	payload.WriteString(`</text><text>`)
	_ = xml.EscapeText(&payload, []byte(common.TruncateRunes(s.formatBody(msg), 1200)))
	payload.WriteString(`</text></binding></visual><audio src="ms-winsoundevent:Notification.Default"/></toast>`)
	request, err := json.Marshal(struct {
		desktopIdentity
		XML string
	}{identity, payload.String()})
	if err != nil {
		return fmt.Errorf("encode Windows notification: %w", err)
	}
	// Keep images and message text off the Windows command line (32K limit).
	// Base64 on stdin also avoids PowerShell console code-page conversions.
	input := strings.NewReader(base64.StdEncoding.EncodeToString(request))
	units := utf16.Encode([]rune(toastScript))
	encoded := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(encoded[2*i:], u)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.run(ctx, powershellPath(), input, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded)); err != nil {
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
$request = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd())) | ConvertFrom-Json
$appID = $request.AppID
$iconPath = $null
if ($request.Icon) {
    $iconDir = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentNotify\icons'
    [IO.Directory]::CreateDirectory($iconDir) | Out-Null
    $iconPath = Join-Path $iconDir $request.IconFile
    if (-not [IO.File]::Exists($iconPath)) {
        $temporaryIcon = $iconPath + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
        try {
            [IO.File]::WriteAllBytes($temporaryIcon, [Convert]::FromBase64String($request.Icon))
            try { [IO.File]::Move($temporaryIcon, $iconPath) } catch {
                if (-not [IO.File]::Exists($iconPath)) { throw }
            }
        } finally {
            if ([IO.File]::Exists($temporaryIcon)) { [IO.File]::Delete($temporaryIcon) }
        }
    }
}
$key = 'HKCU:\Software\Classes\AppUserModelId\' + $appID
if (-not (Test-Path $key)) { New-Item -Path $key -Force | Out-Null }
New-ItemProperty -Path $key -Name 'DisplayName' -Value $request.DisplayName -PropertyType String -Force | Out-Null
if ($iconPath) {
    New-ItemProperty -Path $key -Name 'IconUri' -Value $iconPath -PropertyType String -Force | Out-Null
}
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($request.XML)
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
$toast.ExpirationTime = [DateTimeOffset]::Now.AddMinutes(5)
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($appID)
# Before the first toast, Windows may not have settings for this app yet.
# PowerShell's property access hides the resulting E_ELEMENT_NOT_FOUND as null.
$setting = $null
try { $setting = $notifier.get_Setting() } catch {
    if ($_.Exception.InnerException.HResult -ne -2147023728) { throw }
}
if ($null -ne $setting -and $setting -ne [Windows.UI.Notifications.NotificationSetting]::Enabled) { throw 'Windows notifications are disabled for Agent Notify or by policy' }
$notifier.Show($toast)
`
