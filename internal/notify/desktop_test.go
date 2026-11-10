package notify

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestDesktopNotificationEncodesUntrustedTextWithoutClickAction(t *testing.T) {
	var script string
	sender := NewWindowsSender(func(_ context.Context, name string, args ...string) error {
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
}

func TestDesktopNotificationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := NewWindowsSender(func(context.Context, string, ...string) error { called = true; return nil }).Send(ctx, Message{})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
