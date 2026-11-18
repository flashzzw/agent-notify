package senpihooks

import (
	"bytes"
	"strings"
	"testing"

	"github.com/flashzzw/agent-notify/internal/notify"
)

func TestParseMessage(t *testing.T) {
	msg, err := ParseMessage(bytes.NewBufferString(`{"event":"permission_required","session_id":"s","cwd":"/p","body":"bash"}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Agent != "omo" || msg.Event != "permission_required" || !strings.Contains(msg.Body, "bash") {
		t.Fatalf("%#v", msg)
	}
}

func TestParseMessageNewEventsFallBackToDefaultBody(t *testing.T) {
	for _, event := range []string{"run_interrupted", "subagent_completed"} {
		msg, err := ParseMessage(bytes.NewBufferString(`{"event":"` + event + `","session_id":"s","cwd":"/p","body":""}`))
		if err != nil {
			t.Fatalf("%s: %v", event, err)
		}
		if msg.Event != event || msg.Title != notify.FormatTitle("omo", event) {
			t.Fatalf("%s: unexpected message %+v", event, msg)
		}
		if msg.Body != notify.DefaultBody(event) {
			t.Fatalf("%s: body = %q, want default body", event, msg.Body)
		}
	}
	// 扩展显式带 body 时保留原值。
	msg, err := ParseMessage(bytes.NewBufferString(`{"event":"subagent_completed","session_id":"s","cwd":"/p","body":"子代理输出摘要"}`))
	if err != nil || msg.Body != "子代理输出摘要" {
		t.Fatalf("explicit body should be kept: %+v err=%v", msg, err)
	}
}
func TestRenderDoesNotContainFocusOrShellInterpolation(t *testing.T) {
	s := string(render("C:/工具/agent-notify.exe", "C:/配置/config.yaml"))
	if strings.Contains(s, "anfocus") || strings.Contains(s, "$(") {
		t.Fatal("unsafe extension")
	}
}
