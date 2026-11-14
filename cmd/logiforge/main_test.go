package main

import (
	"os"
	"path/filepath"
	"testing"
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
