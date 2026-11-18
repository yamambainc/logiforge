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

// 読むだけの GitHub App は、ID と秘密鍵の両方か、どちらもなし（トークンなし）。personal access token の設定は断る。
func TestGitHubApp(t *testing.T) {
	cfg := github.Config{Repositories: []ports.Repository{{Name: "q", GitForgeRef: "acme/q"}}}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"LOGIFORGE_GITHUB_READ_TOKEN", "LOGIFORGE_GITHUB_READ_TOKEN_FILE", "LOGIFORGE_GITHUB_APP_ID", "LOGIFORGE_GITHUB_APP_ID_FILE", "LOGIFORGE_GITHUB_APP_PRIVATE_KEY", "LOGIFORGE_GITHUB_APP_PRIVATE_KEY_FILE"} {
		t.Setenv(n, "")
	}
	if app, err := githubApp(cfg); err != nil || app != nil {
		t.Fatalf("どちらもないとき：%v %v", app, err)
	}
	t.Setenv("LOGIFORGE_GITHUB_APP_ID", "42")
	if _, err := githubApp(cfg); err == nil {
		t.Error("秘密鍵がないのに、誤りにならない")
	}
	t.Setenv("LOGIFORGE_GITHUB_APP_PRIVATE_KEY_FILE", p)
	if app, err := githubApp(cfg); err != nil || app == nil {
		t.Fatalf("ID と秘密鍵のファイルから作れない：%v", err)
	}
	t.Setenv("LOGIFORGE_GITHUB_READ_TOKEN", "github_pat_x")
	if _, err := githubApp(cfg); err == nil {
		t.Error("LOGIFORGE_GITHUB_READ_TOKEN を断らない")
	}
}
