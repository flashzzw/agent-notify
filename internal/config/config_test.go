package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyOpenCodeMigratesToOMOWithoutClickToFocus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	old := "version: 2\nagent:\n  opencode:\n    enabled: true\n    install_scope: user\nnotify:\n  opencode:\n    events: [run_completed]\n    channels:\n      system:\n        enabled: true\n        click_to_focus: true\n      wechat_work:\n        enabled: true\n        webhook_url: https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=example\n"
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	agent, n, err := cfg.Target("omo")
	if err != nil {
		t.Fatal(err)
	}
	if !agent.Enabled || !n.Channels.System.Enabled || !strings.Contains(n.Channels.WechatWork.WebhookURL, "key=example") {
		t.Fatalf("migration lost settings: %#v %#v", agent, n)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "click_to_focus") || strings.Contains(string(data), "focus_debug") {
		t.Fatal("removed feature persisted")
	}
	if !strings.Contains(string(data), "omo:") {
		t.Fatal("OMO identity not persisted")
	}
}

func TestEmptyEventSelectionRemainsEmptyAfterSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	_, n, _ := cfg.Target("codex")
	n.Events = []string{}
	n.Channels.System.Enabled = true
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, n, _ = got.Target("codex")
	if len(n.Events) != 0 {
		t.Fatal("empty event selection silently re-enabled")
	}
}

func TestConfigRejectsInvalidTimeoutAndPreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	cfg.Behavior.SendTimeoutSeconds = -1
	if err := Save(path, cfg); err == nil {
		t.Fatal("negative timeout accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid config overwrote file")
	}
}
