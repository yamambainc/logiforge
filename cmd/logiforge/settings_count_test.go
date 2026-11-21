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
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yamambainc/logiforge/internal/api"
	"github.com/yamambainc/logiforge/internal/settings"
	"github.com/yamambainc/logiforge/internal/store"
)

// 設定の repository の一覧の、呼び出しの回数のテスト（design/backend/list-reads.md の「呼び出しの回数のテスト」）。
// 登録した repository が 2 つでも 40 個でも、GitHub への要求の数は同じである。prepared は、置き場の latest で決める。
func TestSettingsRepositoriesCallCount(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	installed := true
	gh := fakeSetupGitHub(t, pemKey, &installed)
	defer gh.Close()
	target, _ := url.Parse(gh.URL)
	var calls atomic.Int64
	proxy := httputil.NewSingleHostReverseProxy(target)
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		proxy.ServeHTTP(w, r)
	}))
	defer counting.Close()

	count := func(n int) (int64, []any) {
		db, err := settings.Open(filepath.Join(t.TempDir(), "s.db"), false)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		bk, _ := settings.ParseKey(settings.NewKey())
		sk, _ := settings.ParseKey(settings.NewKey())
		keys := settings.Keys{Backend: bk, Shared: sk}
		c := &settings.Config{GitForge: settings.GitForgeGitHub, WebUIURL: "https://lf.example",
			GitHub: settings.GitHubConfig{APIURL: counting.URL, AppID: "42", Slug: "logiforge-test", AppPrivateKey: pemKey, ClientID: "Iv1.cid", ClientSecret: "csecret"}}
		// q は、alice が書ける（user になる）リポジトリ。ほかは、数を増やすためのもの。
		c.Repositories = append(c.Repositories, settings.Repository{Name: "q", GitForgeRef: "acme/q"})
		for i := 1; i < n; i++ {
			c.Repositories = append(c.Repositories, settings.Repository{Name: fmt.Sprintf("r%d", i), GitForgeRef: fmt.Sprintf("acme/r%d", i)})
		}
		if err := db.Save(t.Context(), keys, c, ""); err != nil {
			t.Fatal(err)
		}
		st := &store.Dir{Root: t.TempDir()}
		if err := store.SetLatest(t.Context(), st, "q", "c0", ""); err != nil {
			t.Fatal(err)
		}
		fs, err := setupForge(c, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		h := api.SettingsHandler(&settingsService{db: db, keys: keys, auth: fs.auth, store: st}, nil)
		before := calls.Load()
		req := httptest.NewRequest("GET", "/v1/settings/repositories", nil)
		req.Header.Set("Authorization", "Bearer ghu_alice")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d 個：%d %s", n, rec.Code, rec.Body.String())
		}
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return calls.Load() - before, out["items"].([]any)
	}
	c2, items2 := count(2)
	c40, items40 := count(40)
	if len(items2) != 2 || len(items40) != 40 {
		t.Fatalf("件数：%d %d", len(items2), len(items40))
	}
	if c2 != c40 {
		t.Fatalf("GitHub への要求の数が、repository の数で変わった：2 個で %d、40 個で %d", c2, c40)
	}
	first := items40[0].(map[string]any)
	second := items40[1].(map[string]any)
	if first["prepared"] != true || second["prepared"] != false {
		t.Fatalf("prepared は latest で決める：%v %v", first, second)
	}
}
