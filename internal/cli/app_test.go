package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/testutil"
)

func runCLI(t *testing.T, path, input string, args ...string) (string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	args = append([]string{"--config", path}, args...)
	err := Run(context.Background(), args, strings.NewReader(input), &out, &stderr)
	return out.String() + stderr.String(), err
}

func TestInstallHookSendsOnceAndUninstallPreservesUserSettings(t *testing.T) {
	testutil.IsolateHome(t)
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["msgtype"] != "markdown" {
			t.Errorf("unexpected payload: %#v", payload)
		}
		received.Add(1)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()
	for _, id := range []string{"claude", "codex", "omo"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "notify.yaml")
			settings := filepath.Join(dir, "settings.json")
			original := `{"theme":"user-theme","hooks":{"Stop":[{"hooks":[{"type":"command","command":"user-handler"}]}]},"plugin":["oh-my-opencode"]}`
			if id == "omo" {
				settings = filepath.Join(dir, "agent-notify.ts")
			} else if err := os.WriteFile(settings, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AGENT_NOTIFY_WECOM_WEBHOOK", server.URL)
			if out, err := runCLI(t, path, "", "install", "--agent", id, "--settings", settings, "--channels", "wechat-work", "--events", "run_completed"); err != nil {
				t.Fatalf("install: %v %s", err, out)
			}
			hook, payload := "handle-"+id+"-hook", `{"hook_event_name":"Stop","session_id":"example","cwd":"/project"}`
			if id == "omo" {
				payload = `{"event":"run_completed","session_id":"example","cwd":"/project"}`
			}
			before := received.Load()
			for i := 0; i < 2; i++ {
				if out, err := runCLI(t, path, payload, hook); err != nil || out != "" {
					t.Fatalf("hook: %v %s", err, out)
				}
			}
			if got := received.Load() - before; got != 1 {
				t.Fatalf("received %d messages; want one", got)
			}
			if out, err := runCLI(t, path, "", "uninstall", "--agent", id); err != nil {
				t.Fatalf("uninstall: %v %s", err, out)
			}
			data, _ := os.ReadFile(settings)
			if id == "omo" {
				if _, err := os.Stat(settings); err == nil {
					t.Fatalf("OMO extension was not removed")
				}
			} else if !strings.Contains(string(data), "user-handler") || !strings.Contains(string(data), "oh-my-opencode") || strings.Contains(string(data), "handle-"+id+"-hook") {
				t.Fatalf("user settings changed: %s", data)
			}
			if _, err := runCLI(t, path, payload, hook); err != nil {
				t.Fatal(err)
			}
			if received.Load()-before != 1 {
				t.Fatal("disabled integration still sends")
			}
		})
	}
}

func TestConfigShowRedactsWebhookAndHookFailureIsSilent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Default()
	_, n, _ := cfg.Target("claude")
	n.Channels.WechatWork.WebhookURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=never-print-me"
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, path, "", "config", "show")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "never-print-me") {
		t.Fatal("secret exposed")
	}
	if err := os.WriteFile(path, []byte("invalid: [yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, path, "{}", "handle-codex-hook")
	if err != nil || out != "" {
		t.Fatalf("hook error broke caller: %v %s", err, out)
	}
	log, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "agent-notify.log"))
	if len(log) == 0 {
		t.Fatal("failure was not observable")
	}
}

func TestConfigShowRedactsNestedLegacySecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := `legacy_keys:
  webhook_url: https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=map-secret
token_history:
  - array-secret
preferences:
  theme: visible-theme
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, path, "", "config", "show")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"map-secret", "array-secret"} {
		if strings.Contains(out, secret) {
			t.Fatal("config show exposed a nested credential")
		}
	}
	if !strings.Contains(out, "visible-theme") {
		t.Fatal("config show hid a non-sensitive setting")
	}
}
