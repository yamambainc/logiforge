package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/github"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/settings"
	"github.com/yamambainc/logiforge/internal/store"
)

func TestEnvFile(t *testing.T) {
	const name = "LOGIFORGE_TEST_SECRET"
	p := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(p, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(name, "")
	t.Setenv(name+"_FILE", "")
	if _, err := env(name); err == nil {
		t.Error("どちらもないのに、誤りにならない")
	}

	t.Setenv(name, "direct")
	if v, err := env(name); err != nil || v != "direct" {
		t.Errorf("環境変数から読めない：%q %v", v, err)
	}

	t.Setenv(name+"_FILE", p)
	if _, err := env(name); err == nil {
		t.Error("両方あるのに、誤りにならない")
	}

	t.Setenv(name, "")
	if v, err := env(name); err != nil || v != "s3cret" {
		t.Errorf("ファイルから読めない（末尾の改行は除く）：%q %v", v, err)
	}

	t.Setenv(name+"_FILE", filepath.Join(t.TempDir(), "none"))
	if _, err := env(name); err == nil {
		t.Error("ファイルがないのに、誤りにならない")
	}

	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(name+"_FILE", empty)
	if _, err := env(name); err == nil {
		t.Error("ファイルが空なのに、誤りにならない")
	}
}

// GitHub App は、ID と秘密鍵の両方か、どちらもなし（トークンなし）。
func TestGitHubApp(t *testing.T) {
	cfg := github.Config{Repositories: []ports.Repository{{Name: "q", GitForgeRef: "acme/q"}}}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if app, err := githubApp(cfg, settings.GitHubConfig{}); err != nil || app != nil {
		t.Fatalf("どちらもないとき：%v %v", app, err)
	}
	if _, err := githubApp(cfg, settings.GitHubConfig{AppID: "42"}); err == nil {
		t.Error("秘密鍵がないのに、誤りにならない")
	}
	if app, err := githubApp(cfg, settings.GitHubConfig{AppID: "42", AppPrivateKey: pemKey}); err != nil || app == nil {
		t.Fatalf("ID と秘密鍵から作れない：%v", err)
	}
}

// worker には、バックエンドの鍵を渡さない。渡されたら、起動しない。
func TestWorkerRefusesBackendKey(t *testing.T) {
	t.Setenv(envDB, filepath.Join(t.TempDir(), "s.db"))
	t.Setenv(envSharedKey, settings.NewKey())
	t.Setenv(envBackendKey, settings.NewKey())
	if _, _, err := openSettings(false); err == nil {
		t.Fatal("worker が、バックエンドの鍵を受け取った")
	}
}

// settings load は、JSON の設定を設定の DB に書く。秘密は暗号化し、worker の鍵では、バックエンドだけの秘密を読めない。
func TestSettingsLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envDB, filepath.Join(dir, "s.db"))
	bk, sk := settings.NewKey(), settings.NewKey()
	t.Setenv(envBackendKey, bk)
	t.Setenv(envSharedKey, sk)
	p := filepath.Join(dir, "c.json")
	if err := os.WriteFile(p, []byte(`{"gitforge":"github","github":{"app_id":"42","app_private_key":"pem","client_id":"i","client_secret":"s"},"repositories":[{"name":"q","gitforge_ref":"acme/q"}],"record_keys":[{"id":"k1","secret":"0123456789abcdef0123456789abcdef-test"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSettings(t.Context(), []string{"load", p}); err != nil {
		t.Fatal(err)
	}
	if err := runSettings(t.Context(), []string{"load", filepath.Join(dir, "none.json")}); err == nil {
		t.Fatal("ないファイルを読めた")
	}
	t.Setenv(envBackendKey, "")
	db, keys, err := openSettings(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, err := db.Load(t.Context(), keys)
	if err != nil {
		t.Fatal(err)
	}
	if c.GitHub.ClientID != "i" || c.GitHub.ClientSecret != "" || c.GitHub.AppPrivateKey != "" || c.Ready() != nil {
		t.Fatalf("worker が読んだ設定：%+v %v", c, c.Ready())
	}
}

// 2 つの鍵が同じなら、バックエンドは起動しない。
func TestSameKeysRefused(t *testing.T) {
	t.Setenv(envDB, filepath.Join(t.TempDir(), "s.db"))
	k := settings.NewKey()
	t.Setenv(envSharedKey, k)
	t.Setenv(envBackendKey, k)
	if _, _, err := openSettings(true); err == nil {
		t.Fatal("同じ 2 つの鍵で、バックエンドを開けた")
	}
}

// バックエンドは、設定がそろうまで setup_required を返し、設定が書かれたら、起動し直さずに次の要求から動く。
func TestLiveServerReloads(t *testing.T) {
	dir := t.TempDir()
	db, err := settings.Open(filepath.Join(dir, "s.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bk, _ := settings.ParseKey(settings.NewKey())
	sk, _ := settings.ParseKey(settings.NewKey())
	keys := settings.Keys{Backend: bk, Shared: sk}
	l := &liveServer{db: db, keys: keys, store: &store.Dir{Root: dir}}
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	if rec := get("/healthz"); rec.Code != 200 {
		t.Fatalf("セットアップの前の healthz：%d", rec.Code)
	}
	if rec := get("/v1/repositories"); rec.Code != 503 || !strings.Contains(rec.Body.String(), "setup_required") {
		t.Fatalf("セットアップの前：%d %s", rec.Code, rec.Body)
	}
	c := &settings.Config{GitForge: settings.GitForgeForgejo, Forgejo: settings.ForgejoConfig{URL: "http://127.0.0.1:1", Org: "o", WorkerLogin: "r", WorkerToken: "t"},
		RecordKeys: []settings.RecordKey{{ID: "k1", Secret: "0123456789abcdef0123456789abcdef"}}}
	if err := db.Save(t.Context(), keys, c, ""); err != nil {
		t.Fatal(err)
	}
	if rec := get("/v1/repositories"); strings.Contains(rec.Body.String(), "setup_required") {
		t.Fatalf("設定を書いたあとも setup_required：%d %s", rec.Code, rec.Body)
	}
}

// バックエンドは、worker のための installation token を作って設定の DB に置く。残りが長ければ作り直さず、
// 登録したリポジトリが変われば作り直す。worker は、それを読むだけで、切れそうなら使わない。
func TestMintWorkerTokens(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	minted, ttl := 0, time.Hour
	var lastRepos []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/installation"):
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.Method == "POST" && r.URL.Path == "/app/installations/1/access_tokens":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if p, _ := in["permissions"].(map[string]any); p["contents"] != "read" {
				t.Errorf("Read に絞っていない：%v", in)
			}
			lastRepos, _ = in["repositories"].([]any)
			minted++
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": fmt.Sprintf("ghs_%d", minted), "expires_at": time.Now().Add(ttl).UTC().Format(time.RFC3339)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	db, err := settings.Open(filepath.Join(t.TempDir(), "s.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bk, _ := settings.ParseKey(settings.NewKey())
	sk, _ := settings.ParseKey(settings.NewKey())
	keys := settings.Keys{Backend: bk, Shared: sk}
	c := &settings.Config{GitForge: settings.GitForgeGitHub, GitHub: settings.GitHubConfig{APIURL: srv.URL, AppID: "42", AppPrivateKey: pemKey},
		Repositories: []settings.Repository{{Name: "q", GitForgeRef: "acme/q"}},
		RecordKeys:   []settings.RecordKey{{ID: "k1", Secret: "0123456789abcdef0123456789abcdef"}}}
	if err := db.Save(t.Context(), keys, c, ""); err != nil {
		t.Fatal(err)
	}
	if err := mintWorkerTokens(t.Context(), db, keys, time.Now()); err != nil || minted != 1 {
		t.Fatalf("作らない：%d %v", minted, err)
	}
	// worker は、共有の鍵だけで読む。App の秘密鍵は読めない。
	w, err := db.Load(t.Context(), settings.Keys{Shared: sk})
	if err != nil || w.GitHub.AppPrivateKey != "" {
		t.Fatalf("worker が App の秘密鍵を読めた：%v", err)
	}
	src := dbTokens{db: db, keys: settings.Keys{Shared: sk}}
	if tok, err := src.Token(t.Context(), "acme/q"); err != nil || tok != "ghs_1" {
		t.Fatalf("worker が読めない：%q %v", tok, err)
	}
	if err := mintWorkerTokens(t.Context(), db, keys, time.Now()); err != nil || minted != 1 {
		t.Fatalf("残りが長いのに作り直した：%d %v", minted, err)
	}
	// 残りが 40 分を切ったら、作り直す。
	if err := mintWorkerTokens(t.Context(), db, keys, time.Now().Add(25*time.Minute)); err != nil || minted != 2 {
		t.Fatalf("残りが短いのに作り直さない：%d %v", minted, err)
	}
	// 登録したリポジトリが変わったら、すぐ作り直す。
	c.Repositories = append(c.Repositories, settings.Repository{Name: "r", GitForgeRef: "acme/r"})
	if err := db.Save(t.Context(), keys, c, ""); err != nil {
		t.Fatal(err)
	}
	if err := mintWorkerTokens(t.Context(), db, keys, time.Now()); err != nil || minted != 3 || len(lastRepos) != 2 {
		t.Fatalf("登録が変わっても作り直さない：%d %v %v", minted, lastRepos, err)
	}
	// 切れそうなトークンは、worker が使わない。
	ttl = 10 * time.Minute
	c.Repositories = c.Repositories[:1]
	if err := db.Save(t.Context(), keys, c, ""); err != nil {
		t.Fatal(err)
	}
	if err := mintWorkerTokens(t.Context(), db, keys, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Token(t.Context(), "acme/q"); err == nil {
		t.Fatal("切れそうなトークンを使った")
	}
}
