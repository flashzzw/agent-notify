package agenthooks

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/flashzzw/agent-notify/internal/config"
	"github.com/flashzzw/agent-notify/internal/notify"
	"github.com/flashzzw/agent-notify/internal/testutil"
)

const pluginToken = "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"

// pluginStub 冒充 magpie 插件的本地服务；status 为 0 时接受事件。
type pluginStub struct {
	server *httptest.Server
	status int
	events []map[string]any
}

func newPluginStub(t *testing.T, status int) *pluginStub {
	t.Helper()
	stub := &pluginStub{status: status}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-agent-notify-token") != pluginToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if stub.status != 0 {
			w.WriteHeader(stub.status)
			return
		}
		var event map[string]any
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Error(err)
		}
		stub.events = append(stub.events, event)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// recordingSender 代替真实的 Windows Toast，记录降级时发出的通知。
type recordingSender struct{ sent []notify.Message }

func (s *recordingSender) Name() string { return "system" }
func (s *recordingSender) Send(_ context.Context, msg notify.Message) error {
	s.sent = append(s.sent, msg)
	return nil
}

type forwardFixture struct {
	cfg       config.Config
	statePath string
	logPath   string
	toast     *recordingSender
}

// newForwardFixture 开启转发并把端点文件指向 addr；token 写进端点文件。
func newForwardFixture(t *testing.T, addr, token string, events ...string) *forwardFixture {
	t.Helper()
	testutil.IsolateHome(t)
	dir := t.TempDir()
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	endpoint := filepath.Join(dir, "magpie-endpoint.json")
	data, _ := json.Marshal(map[string]any{"port": port, "token": token})
	if err := os.WriteFile(endpoint, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Magpie.Enabled = true
	cfg.Magpie.EndpointFile = endpoint
	a, n, _ := cfg.Target("claude")
	a.Enabled = true
	n.Events = events
	fixture := &forwardFixture{cfg: cfg, statePath: filepath.Join(dir, "state.json"), logPath: filepath.Join(dir, "agent-notify.log"), toast: &recordingSender{}}
	original := newSystemSender
	newSystemSender = func() (notify.Sender, error) { return fixture.toast, nil }
	t.Cleanup(func() { newSystemSender = original })
	return fixture
}

func (f *forwardFixture) dispatch(t *testing.T, event string) {
	t.Helper()
	msg := notify.Message{Agent: "claude_code", Event: event, SessionID: "s1", Workspace: "/home/u/proj", Title: notify.FormatTitle("claude_code", event), Body: "本机正文", Tool: "Bash", Detail: "原始文本"}
	if err := Dispatch(context.Background(), f.cfg, f.statePath, f.logPath, msg); err != nil {
		t.Fatal(err)
	}
}

func (f *forwardFixture) log() string {
	data, _ := os.ReadFile(f.logPath)
	return string(data)
}

func TestDispatchForwardsToPluginWithoutLocalFiltering(t *testing.T) {
	plugin := newPluginStub(t, 0)
	// 本机只订阅了 run_completed；转发模式下过滤交给插件，其余事件照样上报。
	f := newForwardFixture(t, plugin.server.Listener.Addr().String(), pluginToken, "run_completed")
	for _, event := range []string{"run_completed", "tool_failed", "turn_started"} {
		f.dispatch(t, event)
	}
	if len(plugin.events) != 3 {
		t.Fatalf("plugin received %d events; want 3", len(plugin.events))
	}
	first := plugin.events[0]
	if first["agent"] != "claude" || first["event"] != "run_completed" || first["tool"] != "Bash" || first["text"] != "原始文本" || first["session"] != "s1" {
		t.Fatalf("unexpected forwarded event: %v", first)
	}
	if len(f.toast.sent) != 0 {
		t.Fatalf("a delivered event must not also notify locally: %+v", f.toast.sent)
	}
	if f.log() != "" {
		t.Fatalf("unexpected log: %s", f.log())
	}
}

func TestDispatchSkipsForwardingForDisabledAgent(t *testing.T) {
	plugin := newPluginStub(t, 0)
	f := newForwardFixture(t, plugin.server.Listener.Addr().String(), pluginToken, "run_completed")
	a, _, _ := f.cfg.Target("claude")
	a.Enabled = false
	f.dispatch(t, "run_completed")
	if len(plugin.events) != 0 || len(f.toast.sent) != 0 {
		t.Fatal("a disabled agent must stay silent")
	}
}

func TestDispatchFallsBackToLocalToastWhenTokenRejected(t *testing.T) {
	plugin := newPluginStub(t, 0)
	stale := strings.Repeat("0", 64)
	f := newForwardFixture(t, plugin.server.Listener.Addr().String(), stale, "permission_required")
	f.dispatch(t, "permission_required")
	if len(plugin.events) != 0 {
		t.Fatal("event accepted with a wrong token")
	}
	if len(f.toast.sent) != 1 || f.toast.sent[0].Body != "本机正文" {
		t.Fatalf("fallback toast = %+v; want one with the local body", f.toast.sent)
	}
	log := f.log()
	if !strings.Contains(log, "magpie forward error") || strings.Contains(log, stale) {
		t.Fatalf("log should record the failure without the token: %s", log)
	}
}

func TestDispatchFallsBackWhenPluginUnreachableOnlyForSubscribedNotifications(t *testing.T) {
	down := newPluginStub(t, 0)
	addr := down.server.Listener.Addr().String()
	down.server.Close()
	f := newForwardFixture(t, addr, pluginToken, "permission_required")
	// 未订阅的事件与回合开始都不降级；订阅了的事件降级为本机通知。
	f.dispatch(t, "run_completed")
	f.dispatch(t, "turn_started")
	if len(f.toast.sent) != 0 {
		t.Fatalf("unsubscribed events and turn starts must not fall back: %+v", f.toast.sent)
	}
	f.dispatch(t, "permission_required")
	if len(f.toast.sent) != 1 || f.toast.sent[0].Event != "permission_required" {
		t.Fatalf("fallback toast = %+v", f.toast.sent)
	}
	// 回合开始每次提问都有一条，不记日志；其余失败的转发各记一行。
	if log := f.log(); strings.Count(log, "magpie forward error") != 2 || strings.Contains(log, "turn_started") {
		t.Fatalf("failed forwards should be logged, except turn starts: %s", log)
	}
}

func TestDispatchLogsWhenFallbackUnavailable(t *testing.T) {
	plugin := newPluginStub(t, http.StatusInternalServerError)
	f := newForwardFixture(t, plugin.server.Listener.Addr().String(), pluginToken, "run_completed")
	newSystemSender = func() (notify.Sender, error) { return nil, errors.New("系统通知仅支持 Windows 10/11 或 WSL") }
	f.dispatch(t, "run_completed")
	if !strings.Contains(f.log(), "magpie fallback unavailable") {
		t.Fatalf("log = %s", f.log())
	}
}

func TestDispatchDropsTurnStartedWithoutForwarding(t *testing.T) {
	f := newForwardFixture(t, "127.0.0.1:1", pluginToken, "run_completed", "turn_started")
	f.cfg.Magpie.Enabled = false
	f.cfg.Notify.ClaudeCode.Channels.System.Enabled = false
	f.dispatch(t, "turn_started")
	if len(f.toast.sent) != 0 || f.log() != "" {
		t.Fatalf("turn_started must be dropped silently: toast=%+v log=%s", f.toast.sent, f.log())
	}
}
