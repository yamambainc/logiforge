package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWebUISignIn は、ブラウザで WebUI を開き、Forgejo の OAuth2 でサインインして、repository の一覧を出すまでを通す。
// LOGIFORGE_E2E_CHROME（Chrome のパス）と、ビルドした webui/dist/index.html がなければ飛ばす。
// ブラウザの操作は webui/e2e/run.mjs（puppeteer-core）が行う。スクリーンショットは LOGIFORGE_E2E_OUT に書く。
func TestWebUISignIn(t *testing.T) {
	chrome := os.Getenv("LOGIFORGE_E2E_CHROME")
	if chrome == "" {
		t.Skip("LOGIFORGE_E2E_CHROME がない")
	}
	if _, err := os.Stat("../../webui/dist/index.html"); err != nil {
		t.Skip("webui/dist がない（webui/ で npm run build）")
	}
	w := setupWith(t, func(w *world, serveURL string) []string {
		// WebUI のサインインに使う OAuth2 のアプリケーション（confidential client）を、admin で登録する
		var app struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		w.forgejoCall(w.admin, "POST", "/user/applications/oauth2", map[string]any{
			"name": "webui-" + w.org, "redirect_uris": []string{serveURL + "/oauth/callback"}, "confidential_client": true,
		}, &app, 201)
		return []string{"LOGIFORGE_OAUTH_CLIENT_ID=" + app.ClientID, "LOGIFORGE_OAUTH_CLIENT_SECRET=" + app.ClientSecret,
			"LOGIFORGE_OAUTH_REDIRECT_URI=" + serveURL + "/oauth/callback"}
	})
	// 利用者が repository を作り、スナップショットツリーが出るまで待つ
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "webui", "statement": "新しい導線で、継続率が上がる"}); st != 201 {
		t.Fatalf("repository の作成：%d %s", st, b)
	}
	w.eventually("スナップショットツリーが出る", 60*time.Second, func() bool {
		st, _ := w.call("alice", "GET", "/repositories/webui/tree", nil)
		return st == 200
	})
	// root_statement を返す
	_, b := w.call("alice", "GET", "/repositories/webui", nil)
	var repo struct {
		RootStatement *string `json:"root_statement"`
	}
	_ = json.Unmarshal(b, &repo)
	if repo.RootStatement == nil || *repo.RootStatement != "新しい導線で、継続率が上がる" {
		t.Fatalf("root_statement：%s", b)
	}
	// WebUI を配っている（CSP 付き）
	res, err := http.Get(w.api + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("WebUI の CSP がない：%v", res.Header)
	}

	// organization に入っていない Forgejo のユーザー（サインインはできるが、Logiforge の主体ではない）
	outsider := w.org + "outsider"
	w.forgejoCall(w.admin, "POST", "/admin/users", map[string]any{"username": outsider, "email": outsider + "@example.com", "password": "pw-" + outsider + "-12345", "must_change_password": false}, nil, 201)

	out := os.Getenv("LOGIFORGE_E2E_OUT")
	if out == "" {
		out = t.TempDir()
	}
	login := w.login("alice")
	cmd := exec.Command("node", "e2e/run.mjs")
	cmd.Dir = filepath.Join("..", "..", "webui")
	cmd.Env = append(os.Environ(), "BASE="+w.api, "LOGIN="+login, "PASSWORD=pw-"+login+"-12345", "CHROME="+chrome, "OUT="+out, "REPO=webui",
		"OUTSIDER_LOGIN="+outsider, "OUTSIDER_PASSWORD=pw-"+outsider+"-12345")
	b, err = cmd.CombinedOutput()
	t.Logf("run.mjs:\n%s", b)
	if err != nil {
		t.Fatalf("ブラウザの E2E が失敗した：%v（スクリーンショットは %s）", err, out)
	}
}
