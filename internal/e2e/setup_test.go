package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/settings"
)

// 空の設定の DB で起動し、Forgejo を使う構成の初回のセットアップを、セットアップの API で最後まで通す
// （design/settings/setup.md）。だれでも最初の段から進められ、最後に、WebUI のサインインのトークンで alice が利用者になる。
func TestE2ESetupForgejo(t *testing.T) {
	var want settings.Config
	w := setupWith(t, func(_ *world, _ string, c *settings.Config) {
		// 設定の DB には何も書かない。入れる値だけを覚える。
		want = *c
		*c = settings.Config{}
	})
	// token は、セットアップを始めたこのテスト（ブラウザのかわり）のトークンである。
	var token string
	setup := func(method, path string, in any, extra ...string) (int, map[string]any) {
		t.Helper()
		if token != "" {
			extra = append([]string{"Logiforge-Setup-Token", token}, extra...)
		}
		st, b := w.call("", method, path, in, extra...)
		out := map[string]any{}
		_ = json.Unmarshal(b, &out)
		return st, out
	}
	if st, b := w.call("", "GET", "/repositories", nil); st != 503 || !strings.Contains(string(b), "setup_required") {
		t.Fatalf("セットアップの前：%d %s", st, b)
	}
	st, out := setup("PUT", "/setup/gitforge", map[string]string{"gitforge": "forgejo", "webui_url": want.WebUIURL})
	token, _ = out["setup_token"].(string)
	if st != 200 || out["step"] != "forgejo" || token == "" {
		t.Fatalf("構成：%d %v", st, out)
	}
	// ほかのブラウザは、Forgejo のつなぎ方を差し替えられない。
	if st, b := w.call("", "PUT", "/setup/forgejo", map[string]string{"url": "http://evil.example"}, "Logiforge-Setup-Token", "other"); st != 409 || !strings.Contains(string(b), "setup_in_progress") {
		t.Fatalf("ほかのブラウザの Forgejo：%d %s", st, b)
	}
	f := want.Forgejo
	in := map[string]string{"url": f.URL, "organization": f.Org, "client_id": f.OAuthClientID, "client_secret": f.OAuthClientSecret,
		"worker_user": f.WorkerLogin}
	// 書けるトークンは、worker のトークンとして断る。
	in["worker_token"] = w.tokens["agent"]
	in["worker_user"] = w.login("agent")
	if st, out := setup("PUT", "/setup/forgejo", in); st != 422 {
		t.Fatalf("書ける worker のトークン：%d %v", st, out)
	}
	in["worker_token"], in["worker_user"] = f.WorkerToken, f.WorkerLogin
	if st, out := setup("PUT", "/setup/forgejo", in); st != 200 || out["step"] != "signin" {
		t.Fatalf("Forgejo：%d %v", st, out)
	}
	// サインインの中継は、セットアップの途中でも動く。
	if st, b := w.call("", "GET", "/oauth/config", nil); st != 200 || !strings.Contains(string(b), want.WebUIURL+"/oauth/callback") {
		t.Fatalf("サインインの設定：%d %s", st, b)
	}
	// personal access token は、WebUI のサインインのものでない。
	if st, out := setup("POST", "/setup/finish", nil, "Authorization", "Bearer "+w.tokens["agent"]); st != 401 {
		t.Fatalf("personal access token の finish：%d %v", st, out)
	}
	if st, out := setup("POST", "/setup/finish", nil, "Authorization", "Bearer "+w.tokens["alice"]); st != 200 || out["done"] != true {
		t.Fatalf("finish：%d %v", st, out)
	}
	if st, _ := setup("PUT", "/setup/gitforge", map[string]string{"gitforge": "forgejo"}); st != 409 {
		t.Fatalf("終わったあとのセットアップ：%d", st)
	}
	// 終わったら、起動し直さずに、ふつうの API が動く。alice は repository を用意できる。
	w.eventually("repository を用意できる", 30*time.Second, func() bool {
		st, _ := w.call("alice", "POST", "/repositories", map[string]string{"name": "q", "statement": "セットアップのあとの問い"})
		return st == 201 || st == 200
	})
	w.eventually("worker が取り込む", 30*time.Second, func() bool {
		st, _ := w.call("alice", "GET", "/repositories/q", nil)
		return st == 200
	})
	// 設定の API：WebUI のサインインの user だけが使える。書ける worker のトークンには置き換えない。
	if st, b := w.call("agent", "GET", "/settings", nil); st != 403 {
		t.Fatalf("personal access token の getSettings：%d %s", st, b)
	}
	if st, b := w.call("alice", "GET", "/settings", nil); st != 200 || strings.Contains(string(b), f.WorkerToken) || strings.Contains(string(b), f.OAuthClientSecret) {
		t.Fatalf("getSettings：%d %s", st, b)
	}
	if st, b := w.call("alice", "PATCH", "/settings/gitforge", map[string]any{"forgejo": map[string]string{"worker_user": w.login("agent"), "worker_token": w.tokens["agent"]}}); st != 422 {
		t.Fatalf("書ける worker のトークンに置き換えた：%d %s", st, b)
	}
	if st, b := w.call("alice", "PATCH", "/settings/gitforge", map[string]any{"intervals": map[string]any{"patrol_seconds": 2}}); st != 200 {
		t.Fatalf("間隔：%d %s", st, b)
	}
}
