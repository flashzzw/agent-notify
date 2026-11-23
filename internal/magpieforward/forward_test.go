package magpieforward

import (
	"context"
	"encoding/json"
	"io"
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

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakePlugin 冒充插件的本地服务：校验口令并记下收到的事件。
type fakePlugin struct {
	server *httptest.Server
	events []map[string]any
	paths  []string
}

func newFakePlugin(t *testing.T) *fakePlugin {
	t.Helper()
	p := &fakePlugin{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-agent-notify-token") != testToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var event map[string]any
		if err := json.Unmarshal(body, &event); err != nil {
			t.Error(err)
		}
		p.events = append(p.events, event)
		p.paths = append(p.paths, r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakePlugin) port(t *testing.T) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(p.server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	return port
}

func writeEndpoint(t *testing.T, path string, port int, token string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"port": port, "token": token, "pid": 1234, "startedAt": "ignored"})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSendPostsEventToEndpointFromConfiguredFile(t *testing.T) {
	testutil.IsolateHome(t)
	plugin := newFakePlugin(t)
	file := filepath.Join(t.TempDir(), "windows-home", ".agent-notify", "magpie-endpoint.json")
	writeEndpoint(t, file, plugin.port(t), testToken)

	// 配置端口故意写错：端点文件里的端口才是插件实际监听的。
	cfg := config.MagpieConfig{Enabled: true, Port: 1, EndpointFile: file}
	event := EventFromMessage(notify.Message{Agent: "claude_code", Event: "run_completed", SessionID: "s1", Workspace: "/home/u/proj", Detail: "  改好了  ", Model: "m"})
	if err := Send(context.Background(), cfg, event); err != nil {
		t.Fatal(err)
	}
	if len(plugin.events) != 1 || plugin.paths[0] != "/event" {
		t.Fatalf("plugin received %v at %v", plugin.events, plugin.paths)
	}
	got := plugin.events[0]
	for key, want := range map[string]any{"v": float64(1), "agent": "claude", "event": "run_completed", "session": "s1", "cwd": "/home/u/proj", "text": "改好了", "model": "m", "origin": Origin()} {
		if got[key] != want {
			t.Fatalf("%s = %v, want %v (event %v)", key, got[key], want, got)
		}
	}
	if at, ok := got["at"].(float64); !ok || at <= 0 {
		t.Fatalf("at = %v, want a millisecond timestamp", got["at"])
	}
	if _, present := got["tool"]; present {
		t.Fatalf("empty fields should be omitted: %v", got)
	}
}

func TestResolveFallsBackToHomeEndpointAndConfiguredPort(t *testing.T) {
	home := testutil.IsolateHome(t)
	if _, err := Resolve(config.MagpieConfig{Port: 3497}); err != ErrNoEndpoint {
		t.Fatalf("err = %v, want ErrNoEndpoint", err)
	}
	// 配置的端点文件不存在时退回本机默认位置；文件里端口无效时用配置端口。
	writeEndpoint(t, filepath.Join(home, ".agent-notify", "magpie-endpoint.json"), 0, testToken)
	endpoint, err := Resolve(config.MagpieConfig{Port: 4321, EndpointFile: filepath.Join(home, "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Port != 4321 || endpoint.Token != testToken || !strings.HasSuffix(endpoint.File, "magpie-endpoint.json") {
		t.Fatalf("endpoint = %+v", endpoint)
	}
}

func TestSendErrorsNeverContainToken(t *testing.T) {
	testutil.IsolateHome(t)
	plugin := newFakePlugin(t)
	dir := t.TempDir()
	event := EventFromMessage(notify.Message{Agent: "codex", Event: "run_completed"})

	// 口令过期：插件返回 401。
	stale := filepath.Join(dir, "stale.json")
	staleToken := strings.Repeat("f", 64)
	writeEndpoint(t, stale, plugin.port(t), staleToken)
	err := Send(context.Background(), config.MagpieConfig{EndpointFile: stale}, event)
	if err == nil || !strings.Contains(err.Error(), "口令") || strings.Contains(err.Error(), staleToken) {
		t.Fatalf("401 error = %v", err)
	}

	// 插件没在运行：连接被拒绝。
	closed := newFakePlugin(t)
	port := closed.port(t)
	closed.server.Close()
	down := filepath.Join(dir, "down.json")
	writeEndpoint(t, down, port, testToken)
	err = Send(context.Background(), config.MagpieConfig{EndpointFile: down}, event)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("connection error = %v", err)
	}
	if err := Reachable(context.Background(), Endpoint{Port: port}); err == nil {
		t.Fatal("Reachable should fail for a closed port")
	}
	if err := Reachable(context.Background(), Endpoint{Port: plugin.port(t)}); err != nil {
		t.Fatalf("Reachable: %v", err)
	}
}

func TestEventTextIsCappedNotCutToSummaryLength(t *testing.T) {
	long := strings.Repeat("字", 5000)
	event := EventFromMessage(notify.Message{Agent: "omo", Event: "run_completed", Detail: long})
	if n := len([]rune(event.Text)); n != 4000 {
		t.Fatalf("text has %d runes, want the 4000 cap", n)
	}
	if event.Agent != "omo" {
		t.Fatalf("agent = %q", event.Agent)
	}
}
