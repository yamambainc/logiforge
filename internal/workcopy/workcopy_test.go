package workcopy

import (
	"os"
	"path/filepath"
	"testing"
)

// 起動のときに、前のプロセスが残した古い複製を消す。
func TestNewCacheRemovesOld(t *testing.T) {
	base := t.TempDir()
	old := filepath.Join(base, "workcopy-old", "q.git")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(base, "keep")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := NewCache(base, nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Fatalf("古い複製が残っている：%v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("workcopy- でないものまで消した：%v", err)
	}
	if _, err := os.Stat(c.Dir); err != nil {
		t.Fatalf("新しい複製の置き場がない：%v", err)
	}
}
