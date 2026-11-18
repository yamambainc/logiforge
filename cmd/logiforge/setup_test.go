package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/api"
	"github.com/yamambainc/logiforge/internal/settings"
	"github.com/yamambainc/logiforge/internal/store"
)

// fakeSetupGitHub は、セットアップで呼ぶ GitHub の API だけを持つ。
func fakeSetupGitHub(t *testing.T, pemKey string, installed *bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		switch p := r.URL.Path; {
		case r.Method == "POST" && p == "/app-manifests/good-code/conversions":
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42, "slug": "logiforge-test", "client_id": "Iv1.cid",
				"client_secret": "csecret", "pem": pemKey, "html_url": "https://github.com/apps/logiforge-test"})
		case r.Method == "POST" && strings.HasPrefix(p, "/app-manifests/"):
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case r.Method == "GET" && (p == "/repos/acme/q/installation" || p == "/repos/acme/r/installation"):
			if !*installed {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == "POST" && p == "/app/installations/1/access_tokens":
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_read", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
		case r.Method == "GET" && (p == "/repos/acme/q" || p == "/repos/acme/r") && tok == "ghs_read":
			_, _ = w.Write([]byte(`{"id":7,"full_name":"acme/q"}`))
		case r.Method == "POST" && p == "/applications/Iv1.cid/token":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["access_token"] != "ghu_alice" && in["access_token"] != "ghu_bob" {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"token":"x"}`))
		case p == "/user" && (tok == "ghu_alice" || tok == "ghu_bob" || tok == "gho_cli"):
			_ = json.NewEncoder(w).Encode(map[string]string{"login": strings.TrimPrefix(tok, "ghu_")})
		case p == "/user/installations" && tok == "gho_cli":
			_, _ = w.Write([]byte(`{"total_count":0,"installations":[]}`))
		case p == "/user/installations" && (tok == "ghu_alice" || tok == "ghu_bob"):
			_, _ = w.Write([]byte(`{"total_count":1,"installations":[{"id":1}]}`))
		case p == "/user/installations/1/repositories" && (tok == "ghu_alice" || tok == "ghu_bob"):
			push := tok == "ghu_alice"
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 1, "repositories": []map[string]any{
				{"full_name": "acme/q", "permissions": map[string]bool{"push": push, "pull": true}}}})
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
}

// GitHub を使う構成のセットアップを、API で最後まで通す。コード、構成、manifest、conversion、repository、サインインした主体で終える。
func TestSetupGitHub(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	installed := false
	gh := fakeSetupGitHub(t, pemKey, &installed)
	defer gh.Close()

	dir := t.TempDir()
	db, err := settings.Open(filepath.Join(dir, "s.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bk, _ := settings.ParseKey(settings.NewKey())
	sk, _ := settings.ParseKey(settings.NewKey())
	keys := settings.Keys{Backend: bk, Shared: sk}
	// GitHub の API の URL だけは、偽の GitHub に向ける（settings load で入れるのと同じ）。
	if err := db.Put(t.Context(), keys, map[string]string{"github.api_url": gh.URL}, ""); err != nil {
		t.Fatal(err)
	}
	code, err := db.NewSetupCode(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	st := &store.Dir{Root: filepath.Join(dir, "store")}
	l := &liveServer{db: db, keys: keys, store: st}
	l.setup = api.SetupHandler(&setupService{db: db, keys: keys, store: st}, nil)

	type resp struct {
		code int
		body map[string]any
	}
	call := func(method, path, setupCode, auth string, body any) resp {
		t.Helper()
		var rd *strings.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		} else {
			rd = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, rd)
		req.RemoteAddr = "192.0.2.1:1234"
		if setupCode != "" {
			req.Header.Set("Logiforge-Setup-Code", setupCode)
		}
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return resp{rec.Code, out}
	}
	want := func(r resp, code int, what string) {
		t.Helper()
		if r.code != code {
			t.Fatalf("%s：%d %v（want %d）", what, r.code, r.body, code)
		}
	}

	r := call("GET", "/v1/setup", "", "", nil)
	want(r, 200, "getSetup")
	if r.body["done"] != false || r.body["step"] != "gitforge" {
		t.Fatalf("最初の状態：%v", r.body)
	}
	want(call("POST", "/v1/setup/code", "WRONG", "", nil), 401, "違うコード")
	want(call("POST", "/v1/setup/code", code, "", nil), 204, "コード")
	// ほかの API は、setup_required。
	want(call("GET", "/v1/repositories", "", "", nil), 503, "セットアップの前の API")
	want(call("GET", "/v1/settings", "", "", nil), 503, "セットアップの前の設定の API")

	r = call("PUT", "/v1/setup/gitforge", code, "", map[string]string{"gitforge": "github", "webui_url": "https://lf.example"})
	want(r, 200, "構成")
	if r.body["step"] != "github_app" {
		t.Fatalf("構成のあと：%v", r.body)
	}
	r = call("POST", "/v1/setup/github/manifest", code, "", map[string]any{"owner": "acme", "owner_kind": "organization", "webui_url": "https://lf.example", "public": false})
	want(r, 200, "manifest")
	if r.body["post_url"] != "https://github.com/organizations/acme/settings/apps/new" {
		t.Fatalf("送り先：%v", r.body["post_url"])
	}
	m := r.body["manifest"].(map[string]any)
	if m["redirect_url"] != "https://lf.example/setup/github/callback" || m["hook_attributes"].(map[string]any)["active"] != false {
		t.Fatalf("manifest：%v", m)
	}
	want(call("POST", "/v1/setup/github/conversion", code, "", map[string]string{"code": "expired"}), 422, "切れた code")
	r = call("POST", "/v1/setup/github/conversion", code, "", map[string]string{"code": "good-code"})
	want(r, 200, "conversion")
	if r.body["install_url"] != "https://github.com/apps/logiforge-test/installations/new" {
		t.Fatalf("install の URL：%v", r.body)
	}
	// App の秘密鍵と client secret は、worker の鍵では読めない。
	if c, _ := db.Load(t.Context(), settings.Keys{Shared: sk}); c.GitHub.AppPrivateKey != "" || c.GitHub.ClientSecret != "" || c.GitHub.AppID != "42" {
		t.Fatalf("worker から見た App：%+v", c.GitHub)
	}
	// install の前は、repository を読めない。
	r = call("PUT", "/v1/setup/repository", code, "", map[string]string{"name": "q", "gitforge_ref": "acme/q"})
	want(r, 422, "install の前の repository")
	if !strings.Contains(r.body["detail"].(string), "/installations/new") {
		t.Fatalf("install の URL を示さない：%v", r.body)
	}
	installed = true
	r = call("PUT", "/v1/setup/repository", code, "", map[string]string{"name": "q", "gitforge_ref": "acme/q"})
	want(r, 200, "repository")
	if r.body["step"] != "signin" {
		t.Fatalf("repository のあと：%v", r.body)
	}
	// サインインの中継は、セットアップの途中でも動く。
	r = call("GET", "/v1/oauth/config", "", "", nil)
	want(r, 200, "サインインの設定")
	if r.body["redirect_uri"] != "https://lf.example/oauth/callback" {
		t.Fatalf("redirect_uri：%v", r.body)
	}
	want(call("POST", "/v1/setup/finish", code, "", nil), 401, "トークンなしの finish")
	want(call("POST", "/v1/setup/finish", code, "gho_cli", nil), 401, "App のものでないトークン")
	want(call("POST", "/v1/setup/finish", code, "ghu_bob", nil), 403, "Write のない人")
	r = call("POST", "/v1/setup/finish", code, "ghu_alice", nil)
	want(r, 200, "finish")
	if r.body["done"] != true {
		t.Fatalf("終わらない：%v", r.body)
	}
	// 終わったあとは、コードは使えず、getSetup は done を返す。
	want(call("POST", "/v1/setup/code", code, "", nil), 409, "終わったあとのコード")
	if r := call("GET", "/v1/setup", "", "", nil); r.body["done"] != true {
		t.Fatalf("終わったあとの getSetup：%v", r.body)
	}
	// 終わったら、設定の API が使える（セットアップの前は setup_required）。
	want(call("GET", "/v1/settings", "", "ghu_alice", nil), 200, "セットアップのあとの getSettings")
	c, err := db.Load(t.Context(), keys)
	if err != nil || len(c.RecordKeys) != 1 || c.Ready() != nil {
		t.Fatalf("記録の鍵：%v %v", c, err)
	}
	if _, err := db.NewSetupCode(t.Context()); err != settings.ErrSetupDone {
		t.Fatalf("終わったあとにコードを作った：%v", err)
	}
}

// セットアップのコードの API は、送り元ごとに 1 分に 10 回まで。
func TestSetupRateLimit(t *testing.T) {
	db, err := settings.Open(filepath.Join(t.TempDir(), "s.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := api.SetupHandler(&setupService{db: db}, nil)
	code, _ := db.NewSetupCode(t.Context())
	send := func(c string) int {
		req := httptest.NewRequest("POST", "/v1/setup/code", nil)
		req.RemoteAddr = "192.0.2.9:1"
		req.Header.Set("Logiforge-Setup-Code", c)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := range 10 {
		if s := send("WRONG"); s != 401 {
			t.Fatalf("%d 回目：%d", i, s)
		}
	}
	if s := send(code); s != 401 {
		t.Fatalf("上限を越えても通った：%d", s)
	}
}

// 置き場に、すでに取り込んだものがあれば、セットアップを終えない。
func TestStoreUsed(t *testing.T) {
	dir := t.TempDir()
	st := &store.Dir{Root: dir}
	if used, err := storeUsed(st); used || err != nil {
		t.Fatalf("空の置き場：%v %v", used, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "keep", "q"), 0o755); err != nil {
		t.Fatal(err)
	}
	if used, err := storeUsed(st); !used || err != nil {
		t.Fatalf("keep/ がある置き場：%v %v", used, err)
	}
}
