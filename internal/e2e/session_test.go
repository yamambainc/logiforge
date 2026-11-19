package e2e

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yamambainc/logiforge/internal/forgejotest"
	_ "modernc.org/sqlite"
)

// TestWebUISession は、WebUI のサインインのセッション（design/webui/session.md）を、動く Forgejo で通す。
//   - 交換で、HttpOnly の cookie を付ける。refresh token は返さない。
//   - 期限が近ければ、BE が Forgejo で refresh し、回転した refresh token を置く。新しいアクセストークンで API を使える。
//   - サインアウトで、セッションを消す。そのあとは 401。
func TestWebUISession(t *testing.T) {
	w := setup(t)
	login := w.login("alice")
	code, verifier, err := forgejotest.Code(w.forgejo, login, "pw-"+login+"-12345", w.app, w.api+"/oauth/callback")
	if err != nil {
		t.Fatal(err)
	}
	post := func(path, cookie, body string) (*http.Response, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("POST", w.api+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", w.api)
		if cookie != "" {
			req.Header.Set("Cookie", "__Host-lf_session="+cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if strings.Contains(string(b), "refresh") {
			t.Fatalf("refresh token を返した：%s", b)
		}
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		return res, v
	}
	b, _ := json.Marshal(map[string]string{"code": code, "code_verifier": verifier})
	res, tok := post("/v1/oauth/token", "", string(b))
	var cookie string
	for _, c := range res.Cookies() {
		if c.Name == "__Host-lf_session" && c.HttpOnly && c.Secure && c.SameSite == http.SameSiteStrictMode {
			cookie = c.Value
		}
	}
	if res.StatusCode != 200 || cookie == "" {
		t.Fatalf("交換：%d %v %v", res.StatusCode, tok, res.Header.Values("Set-Cookie"))
	}
	at1 := tok["access_token"].(string)
	// 残りが長いので、同じトークンを返す
	if res, v := post("/v1/oauth/session", cookie, "{}"); res.StatusCode != 200 || v["access_token"] != at1 {
		t.Fatalf("session：%d %v", res.StatusCode, v)
	}
	// 期限が近いことにする（設定の DB のアクセストークンの期限を、過去にする）
	db, err := sql.Open("sqlite", "file:"+w.dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expire := func() {
		t.Helper()
		if _, err := db.Exec(`UPDATE webui_sessions SET access_expires_at = '2000-01-01T00:00:00Z'`); err != nil {
			t.Fatal(err)
		}
	}
	expire()
	res, v := post("/v1/oauth/session", cookie, "{}")
	at2, _ := v["access_token"].(string)
	if res.StatusCode != 200 || at2 == "" {
		t.Fatalf("refresh：%d %v", res.StatusCode, v)
	}
	// Forgejo は、同じ秒の refresh では、同じアクセストークンを返すことがある。そのため、トークンが変わったかは見ない。
	// もう 1 度 refresh できる。Forgejo の refresh token は 1 回しか使えないので、回転したものを置いていなければ断られる
	expire()
	if res, v := post("/v1/oauth/session", cookie, "{}"); res.StatusCode != 200 || v["access_token"] == nil {
		t.Fatalf("2 回目の refresh：%d %v", res.StatusCode, v)
	}
	// 新しいアクセストークンで、Logiforge の API を使える（user）
	w.tokens["alice-refreshed"] = at2
	if st, b := w.call("alice-refreshed", "GET", "/settings", nil); st != 200 {
		t.Fatalf("新しいトークンで設定を読めない：%d %s", st, b)
	}
	// ほかのオリジンからは、もらえない
	req, _ := http.NewRequest("POST", w.api+"/v1/oauth/session", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Cookie", "__Host-lf_session="+cookie)
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 403 {
		t.Fatalf("ほかのオリジン：%v %v", r, err)
	}
	// サインアウトで消す。そのあとは 401
	if res, _ := post("/v1/oauth/signout", cookie, "{}"); res.StatusCode != 204 {
		t.Fatalf("サインアウト：%d", res.StatusCode)
	}
	if res, _ := post("/v1/oauth/session", cookie, "{}"); res.StatusCode != 401 {
		t.Fatalf("サインアウトのあと：%d", res.StatusCode)
	}
}
