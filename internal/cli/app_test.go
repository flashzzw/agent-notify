package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
		if payload["msgtype"] != "text" {
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

// magpie 转发的整条 CLI 路径：install --magpie 订阅回合开始并开启转发，
// hook 把事件交给插件且不向 stdout 输出，test magpie 与 doctor 可用，
// 重装不关转发，--magpie=false 才关并摘掉回合开始订阅。
func TestInstallWithMagpieForwardsHookEventsToPlugin(t *testing.T) {
	testutil.IsolateHome(t)
	original := locateWindowsEndpointFile
	locateWindowsEndpointFile = func(context.Context) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { locateWindowsEndpointFile = original })

	const token = "9999999999999999999999999999999999999999999999999999999999999999"
	var events []map[string]any
	plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/event" || r.Header.Get("x-agent-notify-token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		events = append(events, event)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer plugin.Close()
	_, portText, _ := net.SplitHostPort(plugin.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)

	dir := t.TempDir()
	path := filepath.Join(dir, "notify.yaml")
	settings := filepath.Join(dir, "settings.json")
	endpoint := filepath.Join(dir, "magpie-endpoint.json")
	data, _ := json.Marshal(map[string]any{"port": port, "token": token})
	if err := os.WriteFile(endpoint, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Magpie.EndpointFile = endpoint
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	// 转发模式不要求 --channels。
	if out, err := runCLI(t, path, "", "install", "--agent", "claude", "--settings", settings, "--magpie"); err != nil {
		t.Fatalf("install --magpie: %v %s", err, out)
	}
	installed, _ := os.ReadFile(settings)
	if !strings.Contains(string(installed), `"UserPromptSubmit"`) {
		t.Fatalf("turn start not subscribed: %s", installed)
	}

	for _, payload := range []string{
		`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"/project","prompt":"机密提问"}`,
		`{"hook_event_name":"Stop","session_id":"s1","cwd":"/project","last_assistant_message":"改好了"}`,
		`{"hook_event_name":"PostToolUseFailure","session_id":"s1","cwd":"/project","tool_name":"Bash","error":"Exit code 1"}`,
	} {
		// UserPromptSubmit 的 stdout 会被 Claude 注入上下文：hook 不得输出任何内容。
		if out, err := runCLI(t, path, payload, "handle-claude-hook"); err != nil || out != "" {
			t.Fatalf("hook: %v %q", err, out)
		}
	}
	if len(events) != 3 {
		t.Fatalf("plugin received %d events; want 3: %v", len(events), events)
	}
	if events[0]["event"] != "turn_started" || events[1]["event"] != "run_completed" || events[2]["event"] != "tool_failed" {
		t.Fatalf("unexpected events: %v", events)
	}
	if text, _ := events[0]["text"].(string); text != "" {
		t.Fatalf("prompt text must not be forwarded: %q", text)
	}
	if events[1]["text"] != "改好了" || events[1]["agent"] != "claude" || events[2]["tool"] != "Bash" {
		t.Fatalf("unexpected event fields: %v", events[1:])
	}

	out, err := runCLI(t, path, "", "test", "magpie", "--agent", "claude")
	if err != nil || len(events) != 4 || events[3]["event"] != "test" {
		t.Fatalf("test magpie: %v %s events=%v", err, out, events)
	}
	out, err = runCLI(t, path, "", "doctor")
	if err != nil || !strings.Contains(out, "magpie 转发: 已开启") || !strings.Contains(out, "可连接") || strings.Contains(out, token) {
		t.Fatalf("doctor: %v %s", err, out)
	}
	if len(events) != 4 {
		t.Fatal("doctor must not send events")
	}

	// 重装（不带 --magpie）保持转发；--magpie=false 关闭并摘掉回合开始订阅。
	if out, err := runCLI(t, path, "", "install", "--agent", "claude", "--settings", settings); err != nil {
		t.Fatalf("reinstall: %v %s", err, out)
	}
	loaded, err := config.Load(path)
	if err != nil || !loaded.Magpie.Enabled {
		t.Fatalf("reinstall silently disabled forwarding: %+v err=%v", loaded.Magpie, err)
	}
	if _, err := runCLI(t, path, "", "install", "--agent", "claude", "--settings", settings, "--magpie=false"); err == nil {
		t.Fatal("turning forwarding off without any local channel should ask for channels")
	}
	if out, err := runCLI(t, path, "", "install", "--agent", "claude", "--settings", settings, "--magpie=false", "--channels", "system"); err != nil {
		t.Fatalf("install --magpie=false: %v %s", err, out)
	}
	loaded, err = config.Load(path)
	if err != nil || loaded.Magpie.Enabled {
		t.Fatalf("forwarding still enabled: %+v err=%v", loaded.Magpie, err)
	}
	installed, _ = os.ReadFile(settings)
	if strings.Contains(string(installed), `"UserPromptSubmit"`) {
		t.Fatalf("turn start still subscribed after disabling forwarding: %s", installed)
	}
}

func TestDoctorReportsUnreachableMagpiePlugin(t *testing.T) {
	testutil.IsolateHome(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Default()
	cfg.Magpie.Enabled = true
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, path, "", "doctor")
	if err == nil || !strings.Contains(out, "端点文件未找到") {
		t.Fatalf("doctor should flag the missing endpoint: %v %s", err, out)
	}
	if _, err := runCLI(t, path, "", "test", "magpie"); err == nil {
		t.Fatal("test magpie should fail without an endpoint")
	}
}
