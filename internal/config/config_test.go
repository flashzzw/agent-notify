package config

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestSaveRestrictsExistingConfigurationAndBackupPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes do not represent Windows ACLs")
	}
	for _, existingBackup := range []bool{false, true} {
		t.Run(map[bool]string{false: "new backup", true: "existing backup"}[existingBackup], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			backup := path + ".agent-notify.bak"
			original := []byte("legacy_token: test-credential\n")
			if err := os.WriteFile(path, original, 0644); err != nil {
				t.Fatal(err)
			}
			if existingBackup {
				if err := os.WriteFile(backup, original, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := Save(path, Default()); err != nil {
				t.Fatal(err)
			}
			for _, file := range []string{path, backup} {
				info, err := os.Stat(file)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0600 {
					t.Errorf("%s permissions = %o, want 600", filepath.Base(file), info.Mode().Perm())
				}
			}
			data, err := os.ReadFile(backup)
			if err != nil || string(data) != string(original) {
				t.Fatal("original configuration backup was not preserved")
			}
		})
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
