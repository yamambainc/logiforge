package e2e

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/settings"
)

// 空の設定の DB で起動し、Forgejo を使う構成の初回のセットアップを、セットアップの API で最後まで通す
// （design/settings/setup.md）。コードはログから読み、最後に、WebUI のサインインのトークンで alice が利用者になる。
func TestE2ESetupForgejo(t *testing.T) {
	var want settings.Config
	w := setupWith(t, func(_ *world, _ string, c *settings.Config) {
		// 設定の DB には何も書かない。入れる値だけを覚える。
		want = *c
		*c = settings.Config{}
	})
	var code string
	re := regexp.MustCompile(`setup_code=(\S+)`)
	w.eventually("ログにセットアップのコードが出る", 20*time.Second, func() bool {
		if m := re.FindStringSubmatch(w.logs["serve"].String()); m != nil {
			code = m[1]
			return true
		}
		return false
	})
	setup := func(method, path string, in any, extra ...string) (int, map[string]any) {
		t.Helper()
		st, b := w.call("", method, path, in, append([]string{"Logiforge-Setup-Code", code}, extra...)...)
		out := map[string]any{}
		_ = json.Unmarshal(b, &out)
		return st, out
	}
	if st, b := w.call("", "GET", "/repositories", nil); st != 503 || !strings.Contains(string(b), "setup_required") {
		t.Fatalf("セットアップの前：%d %s", st, b)
	}
	if st, _ := w.call("", "POST", "/setup/code", nil, "Logiforge-Setup-Code", "WRONG"); st != 401 {
		t.Fatalf("違うコード：%d", st)
	}
	if st, out := setup("POST", "/setup/code", nil); st != 204 {
		t.Fatalf("コード：%d %v", st, out)
	}
	if st, out := setup("PUT", "/setup/gitforge", map[string]string{"gitforge": "forgejo", "webui_url": want.WebUIURL}); st != 200 || out["step"] != "forgejo" {
		t.Fatalf("構成：%d %v", st, out)
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
	if st, _ := setup("POST", "/setup/code", nil); st != 409 {
		t.Fatalf("終わったあとのコード：%d", st)
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
}
