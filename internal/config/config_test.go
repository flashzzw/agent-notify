package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

// 升级守卫：白名单随版本扩展,默认选择集保持首期集合,
// 保证已有用户与新装用户升级后不会突然收到更多通知。
func TestEventsWhitelistGrowsButDefaultsDoNot(t *testing.T) {
	if !strings.Contains(strings.Join(Events("claude"), ","), "subagent_completed") {
		t.Fatalf("claude whitelist should include subagent_completed: %v", Events("claude"))
	}
	if strings.Contains(strings.Join(Events("claude"), ","), "run_interrupted") {
		t.Fatalf("claude has no interrupt hook; run_interrupted must stay off the whitelist")
	}
	if !strings.Contains(strings.Join(Events("codex"), ","), "run_interrupted") {
		t.Fatalf("codex whitelist should include run_interrupted: %v", Events("codex"))
	}
	cfg := Default()
	for _, id := range Agents {
		_, n, _ := cfg.Target(id)
		for _, event := range n.Events {
			if event == "subagent_completed" || event == "run_interrupted" {
				t.Fatalf("Default() enabled %s for %s; new events must be opt-in", event, id)
			}
		}
	}
}

func TestTemplateValidationRoundTripAndRejection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.Behavior.TitleTemplate = "{agentName} · {event}"
	cfg.Behavior.BodyTemplate = "[{session}] {message}"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Behavior.TitleTemplate != "{agentName} · {event}" || got.Behavior.BodyTemplate != "[{session}] {message}" {
		t.Fatalf("templates did not round-trip: %+v", got.Behavior)
	}

	before, _ := os.ReadFile(path)
	cfg.Behavior.TitleTemplate = "标题\n带换行"
	if err := Save(path, cfg); err == nil {
		t.Fatal("newline in title template accepted")
	}
	cfg.Behavior.TitleTemplate = strings.Repeat("长", 101)
	if err := Save(path, cfg); err == nil {
		t.Fatal("oversized title template accepted")
	}
	cfg.Behavior.TitleTemplate = "合法 {unknown_placeholder} 标题"
	cfg.Behavior.BodyTemplate = "正文\n多行 {message}"
	if err := Save(path, cfg); err != nil {
		t.Fatalf("unknown placeholders and body newlines should stay legal: %v", err)
	}
	// Re-baseline after the legal save, then confirm rejected saves never touch disk.
	before, _ = os.ReadFile(path)
	cfg.Behavior.BodyTemplate = strings.Repeat("长", 501)
	if err := Save(path, cfg); err == nil {
		t.Fatal("oversized body template accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid config overwrote file")
	}
}

func TestMagpieForwardingDefaultsAndValidation(t *testing.T) {
	cfg := Default()
	if cfg.Magpie.Enabled || cfg.Magpie.Port != DefaultMagpiePort {
		t.Fatalf("defaults = %+v, want disabled on port %d", cfg.Magpie, DefaultMagpiePort)
	}
	// 旧配置没有 magpie 段，或只写了 enabled：端口继承默认值。
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 3\nmagpie:\n  enabled: true\n  endpoint_file: /mnt/c/Users/u/.agent-notify/magpie-endpoint.json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Magpie.Enabled || loaded.Magpie.Port != DefaultMagpiePort {
		t.Fatalf("loaded = %+v", loaded.Magpie)
	}
	redacted, err := loaded.Redacted()
	if err != nil || !strings.Contains(string(redacted), "magpie-endpoint.json") {
		t.Fatalf("endpoint_file should stay visible in diagnostics: %s err=%v", redacted, err)
	}
	for _, port := range []int{0, 65536} {
		bad := Default()
		bad.Magpie.Port = port
		if err := bad.Validate(); err == nil {
			t.Fatalf("port %d should be rejected", port)
		}
	}
}

func TestToolFailedIsOptInForClaudeOnly(t *testing.T) {
	if !slices.Contains(Events("claude"), "tool_failed") {
		t.Fatal("claude should offer tool_failed")
	}
	cfg := Default()
	_, claude, _ := cfg.Target("claude")
	if slices.Contains(claude.Events, "tool_failed") {
		t.Fatal("tool_failed must not be subscribed by default")
	}
	for _, id := range Agents {
		if slices.Contains(Events(id), "turn_started") {
			t.Fatalf("%s: turn_started is internal and must not be selectable", id)
		}
	}
	if slices.Contains(Events("codex"), "tool_failed") {
		t.Fatal("codex has no tool failure hook")
	}
}
