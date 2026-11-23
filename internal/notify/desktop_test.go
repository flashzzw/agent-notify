package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"image/png"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestDesktopNotificationEncodesUntrustedTextWithoutClickAction(t *testing.T) {
	var script string
	var request desktopTestRequest
	sender := NewWindowsSender(func(_ context.Context, name string, input io.Reader, args ...string) error {
		if !strings.EqualFold(filepath.Base(name), "powershell.exe") {
			t.Fatalf("unexpected runner: %s", name)
		}
		raw, err := base64.StdEncoding.DecodeString(args[len(args)-1])
		if err != nil {
			t.Fatal(err)
		}
		units := make([]uint16, len(raw)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(raw[2*i:])
		}
		script = string(utf16.Decode(units))
		request = readDesktopTestRequest(t, input)
		return nil
	})
	body := "中文 <xml> ' $([System.IO.File]::Delete('oops'))"
	if err := sender.Send(context.Background(), Message{Agent: "codex", Title: "测试", Body: body}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(script, body) || strings.Contains(script, "anfocus") || strings.Contains(script, "ActivationArguments") {
		t.Fatalf("unsafe notification script: %s", script)
	}
	if !strings.Contains(script, "FromBase64String") || !strings.Contains(script, "CreateToastNotifier") {
		t.Fatal("missing encoded toast delivery")
	}
	var toast struct {
		Texts []string `xml:"visual>binding>text"`
	}
	if err := xml.Unmarshal([]byte(request.XML), &toast); err != nil {
		t.Fatal(err)
	}
	if len(toast.Texts) != 2 || toast.Texts[0] != "测试" || toast.Texts[1] != body {
		t.Fatalf("notification text did not survive encoding: %v", toast.Texts)
	}
	assertToastHasNoImage(t, request.XML)
}

// The toast body is text only: the agent is identified by the small icon Windows
// draws beside the source name, not by a logo placed in front of the text.
// User text is XML-escaped, so a literal "<image" can only be an element.
func assertToastHasNoImage(t *testing.T, toastXML string) {
	t.Helper()
	if strings.Contains(toastXML, "<image") {
		t.Fatalf("notification carries an image, want text only: %s", toastXML)
	}
}

func TestDesktopNotificationShowsProjectAttributionAndNoTimestamp(t *testing.T) {
	var request desktopTestRequest
	sender := NewWindowsSender(func(_ context.Context, _ string, input io.Reader, _ ...string) error {
		request = readDesktopTestRequest(t, input)
		return nil
	})
	msg := Message{
		Agent:     "claude_code",
		Workspace: "/home/u/学习ai编程项目/agent-notify",
		Title:     "Claude Code 运行完成",
		Body:      "任务已完成，请查看结果",
	}
	if err := sender.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	var toast struct {
		Texts []struct {
			Text      string `xml:",chardata"`
			Placement string `xml:"placement,attr"`
		} `xml:"visual>binding>text"`
	}
	if err := xml.Unmarshal([]byte(request.XML), &toast); err != nil {
		t.Fatal(err)
	}
	if len(toast.Texts) != 3 {
		t.Fatalf("notification texts = %v, want title, body, attribution", toast.Texts)
	}
	if toast.Texts[0].Text != msg.Title || toast.Texts[0].Placement != "" {
		t.Fatalf("title = %+v, want %q", toast.Texts[0], msg.Title)
	}
	// The body must be byte-identical: no workspace line, no hand-rolled timestamp.
	if toast.Texts[1].Text != msg.Body {
		t.Fatalf("body = %q, want %q exactly (no extra lines)", toast.Texts[1].Text, msg.Body)
	}
	if toast.Texts[2].Placement != "attribution" || toast.Texts[2].Text != "agent-notify" {
		t.Fatalf("attribution = %+v, want project name agent-notify", toast.Texts[2])
	}
}

func TestDesktopNotificationOmitsAttributionWithoutWorkspace(t *testing.T) {
	var request desktopTestRequest
	sender := NewWindowsSender(func(_ context.Context, _ string, input io.Reader, _ ...string) error {
		request = readDesktopTestRequest(t, input)
		return nil
	})
	if err := sender.Send(context.Background(), Message{Agent: "codex", Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	var toast struct {
		Texts []struct {
			Placement string `xml:"placement,attr"`
		} `xml:"visual>binding>text"`
	}
	if err := xml.Unmarshal([]byte(request.XML), &toast); err != nil {
		t.Fatal(err)
	}
	if len(toast.Texts) != 2 || toast.Texts[1].Placement == "attribution" {
		t.Fatalf("texts = %+v, want exactly title+body without attribution", toast.Texts)
	}
}

func TestDesktopNotificationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := NewWindowsSender(func(context.Context, string, io.Reader, ...string) error { called = true; return nil }).Send(ctx, Message{})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

type desktopTestRequest struct {
	AppID, DisplayName, IconFile, XML string
	Icon                              []byte
}

func readDesktopTestRequest(t *testing.T, input io.Reader) desktopTestRequest {
	t.Helper()
	var request desktopTestRequest
	if err := json.NewDecoder(base64.NewDecoder(base64.StdEncoding, input)).Decode(&request); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestDesktopNotificationUsesAgentIdentityAndEmbeddedLogo(t *testing.T) {
	logos := map[string]string{}
	for _, tc := range []struct{ agent, appID, name string }{
		{"codex", "AgentNotify.Desktop.Codex", "Codex Notify"},
		{"claude_code", "AgentNotify.Desktop.Claude", "Claude Notify"},
		{"claude", "AgentNotify.Desktop.Claude", "Claude Notify"},
		{"omo", "AgentNotify.Desktop.OMO", "OMO Notify"},
		{"opencode", "AgentNotify.Desktop.OMO", "OMO Notify"},
		{"unknown'; throw 'injected", "AgentNotify.Desktop", "Agent Notify"},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			sender := NewWindowsSender(func(_ context.Context, _ string, input io.Reader, args ...string) error {
				request := readDesktopTestRequest(t, input)
				if request.AppID != tc.appID || request.DisplayName != tc.name {
					t.Fatalf("identity = %q / %q, want %q / %q", request.AppID, request.DisplayName, tc.appID, tc.name)
				}
				// The logo travels in the request for the notification identity
				// (the small header icon); the toast itself shows none.
				assertToastHasNoImage(t, request.XML)
				if tc.appID == "AgentNotify.Desktop" {
					if len(request.Icon) != 0 || request.IconFile != "" {
						t.Fatal("unknown agent inherited a different agent's logo")
					}
					return nil
				}
				if _, err := png.DecodeConfig(bytes.NewReader(request.Icon)); err != nil {
					t.Fatalf("embedded icon is not a PNG: %v", err)
				}
				if filepath.Base(request.IconFile) != request.IconFile || !strings.HasSuffix(request.IconFile, ".png") {
					t.Fatalf("unsafe icon filename: %q", request.IconFile)
				}
				for appID, logo := range logos {
					if (appID == tc.appID) != (logo == string(request.Icon)) {
						t.Fatalf("logo does not match agent identity %s", tc.appID)
					}
				}
				logos[tc.appID] = string(request.Icon)
				// Windows CreateProcess limits the command line to 32767 characters.
				if len(strings.Join(args, " ")) > 30000 {
					t.Fatal("notification command exceeds the Windows command-line budget")
				}
				return nil
			})
			if err := sender.Send(context.Background(), Message{Agent: tc.agent, Title: strings.Repeat("中", 120), Body: strings.Repeat("<&🙂", 1200)}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
