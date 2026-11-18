package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/api"
	"github.com/yamambainc/logiforge/internal/settings"
)

// 設定の API（GitHub を使う構成）。WebUI の user だけが使え、送った項目だけを変え、秘密を返さない。
func TestSettingsAPI(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	installed := true
	gh := fakeSetupGitHub(t, pemKey, &installed)
	defer gh.Close()
	db, err := settings.Open(filepath.Join(t.TempDir(), "s.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bk, _ := settings.ParseKey(settings.NewKey())
	sk, _ := settings.ParseKey(settings.NewKey())
	keys := settings.Keys{Backend: bk, Shared: sk}
	c := &settings.Config{GitForge: settings.GitForgeGitHub, WebUIURL: "https://lf.example",
		GitHub:       settings.GitHubConfig{APIURL: gh.URL, AppID: "42", Slug: "logiforge-test", AppPrivateKey: pemKey, ClientID: "Iv1.cid", ClientSecret: "csecret"},
		Repositories: []settings.Repository{{Name: "q", GitForgeRef: "acme/q"}},
		RecordKeys:   []settings.RecordKey{{ID: "k1", Secret: "0123456789abcdef0123456789abcdef"}}}
	if err := db.Save(t.Context(), keys, c, ""); err != nil {
		t.Fatal(err)
	}
	load := func() *settings.Config {
		c, err := db.Load(t.Context(), keys)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	fs, err := setupForge(load(), pullreq.Keyring{}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := api.SettingsHandler(&settingsService{db: db, keys: keys, auth: fs.auth}, nil)
	call := func(tok, method, path string, body any) (int, map[string]any, string) {
		t.Helper()
		var rd *strings.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		} else {
			rd = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out, rec.Body.String()
	}
	// CLI のトークンと、Write のない人は、読めない。
	for _, tok := range []string{"gho_cli", "ghu_bob"} {
		if st, _, b := call(tok, "GET", "/v1/settings", nil); st != 403 {
			t.Fatalf("%s：%d %s", tok, st, b)
		}
	}
	st, out, b := call("ghu_alice", "GET", "/v1/settings", nil)
	if st != 200 || strings.Contains(b, "csecret") || strings.Contains(b, "PRIVATE KEY") {
		t.Fatalf("getSettings：%d %s", st, b)
	}
	if g := out["github"].(map[string]any); g["client_secret"].(map[string]any)["set"] != true || g["app_id"] != float64(42) {
		t.Fatalf("github：%v", g)
	}
	// 送った項目だけを変える。送らなかった秘密は消さない。
	if st, _, b := call("ghu_alice", "PATCH", "/v1/settings/gitforge", map[string]any{"github": map[string]any{"client_id": "Iv1.cid"}, "intervals": map[string]any{"patrol_seconds": 5}}); st != 200 {
		t.Fatalf("PATCH：%d %s", st, b)
	}
	if c := load(); c.GitHub.ClientSecret != "csecret" || c.GitHub.AppPrivateKey == "" || c.Intervals.Patrol != "5s" {
		t.Fatalf("PATCH のあと：%+v %+v", c.GitHub.ClientSecret, c.Intervals)
	}
	if _, out, _ := call("ghu_alice", "GET", "/v1/settings", nil); out["updated_by"] != "alice" {
		t.Fatalf("最後に変えた人：%v", out["updated_by"])
	}
	for what, body := range map[string]any{
		"git の時間の上限が 20 分以上": map[string]any{"intervals": map[string]any{"git_timeout_seconds": 1200}},
		"構成の違う設定":            map[string]any{"forgejo": map[string]any{"url": "https://f.example"}},
		"秘密を空にする":            map[string]any{"github": map[string]any{"client_secret": ""}},
		"PEM でない秘密鍵":         map[string]any{"github": map[string]any{"private_key": "nope"}},
	} {
		if st, _, b := call("ghu_alice", "PATCH", "/v1/settings/gitforge", body); st != 422 {
			t.Fatalf("%s：%d %s", what, st, b)
		}
	}
	// repository の登録と外す。
	if st, _, b := call("ghu_alice", "DELETE", "/v1/settings/repositories/q", nil); st != 422 {
		t.Fatalf("最後の repository を外せた：%d %s", st, b)
	}
	if st, _, b := call("ghu_alice", "PUT", "/v1/settings/repositories/r", map[string]string{"gitforge_ref": "acme/r"}); st != 200 {
		t.Fatalf("登録：%d %s", st, b)
	}
	if st, _, b := call("ghu_alice", "PUT", "/v1/settings/repositories/r", map[string]string{"gitforge_ref": "acme/q", "directory": "x"}); st != 422 {
		t.Fatalf("登録した組を変えられた：%d %s", st, b)
	}
	// App が読めない（install で選んでいない）リポジトリは、install の URL を示して断る。
	if st, _, b := call("ghu_alice", "PUT", "/v1/settings/repositories/s", map[string]string{"gitforge_ref": "acme/t"}); st != 422 || !strings.Contains(b, "/installations/new") {
		t.Fatalf("install していない：%d %s", st, b)
	}
	if st, _, b := call("ghu_alice", "DELETE", "/v1/settings/repositories/r", nil); st != 204 {
		t.Fatalf("外す：%d %s", st, b)
	}
	if st, _, b := call("ghu_alice", "DELETE", "/v1/settings/repositories/r", nil); st != 404 {
		t.Fatalf("2 回外す：%d %s", st, b)
	}
	if err := db.RegisterRepository(t.Context(), settings.Repository{Name: "r", GitForgeRef: "acme/other"}, "x"); err == nil {
		t.Fatal("外した名前を、別の組で登録し直せた")
	}
	if st, _, b := call("ghu_alice", "PUT", "/v1/settings/repositories/r", map[string]string{"gitforge_ref": "acme/r"}); st != 200 {
		t.Fatalf("同じ組で登録し直す：%d %s", st, b)
	}
	if st, out, _ := call("ghu_alice", "GET", "/v1/settings/repositories", nil); st != 200 || len(out["items"].([]any)) != 2 {
		t.Fatalf("一覧：%v", out)
	}
	// 記録の鍵：新しい鍵を書くときの鍵にし、古い鍵は残す。
	st, out, b = call("ghu_alice", "POST", "/v1/settings/record-keys", nil)
	if st != 201 || out["primary"] != true {
		t.Fatalf("鍵を作る：%d %s", st, b)
	}
	if _, out, b := call("ghu_alice", "GET", "/v1/settings/record-keys", nil); len(out["items"].([]any)) != 2 || strings.Contains(b, "private") {
		t.Fatalf("鍵の一覧：%s", b)
	}
	if c := load(); c.RecordKeys[0].ID == "k1" || len(c.RecordKeys) != 2 {
		t.Fatalf("書くときの鍵：%v", c.RecordKeys[0].ID)
	}
}
