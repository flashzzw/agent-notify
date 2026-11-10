package senpihooks

import (
	"bytes"
	"strings"
	"testing"
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
func TestRenderDoesNotContainFocusOrShellInterpolation(t *testing.T) {
	s := string(render("C:/工具/agent-notify.exe", "C:/配置/config.yaml"))
	if strings.Contains(s, "anfocus") || strings.Contains(s, "$(") {
		t.Fatal("unsafe extension")
	}
}
