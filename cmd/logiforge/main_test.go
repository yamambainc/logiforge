package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/yamambainc/logiforge/internal/adapters/github"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/settings"
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
	if err := os.WriteFile(p, []byte(`{"gitforge":"forgejo","forgejo":{"url":"http://f","org":"o","worker_login":"r","worker_token":"t","oauth_client_id":"i","oauth_client_secret":"s"},"record_keys":[{"id":"k1","secret":"x"}]}`), 0o600); err != nil {
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
	if c.Forgejo.WorkerToken != "t" || c.Forgejo.OAuthClientSecret != "" || c.Ready() != nil {
		t.Fatalf("worker が読んだ設定：%+v %v", c, c.Ready())
	}
}
