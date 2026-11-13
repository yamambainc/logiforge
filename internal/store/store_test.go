package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDir(t *testing.T) {
	ctx := context.Background()
	d := &Dir{Root: t.TempDir()}
	if _, err := d.Get(ctx, "q/latest"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := d.PutIf(ctx, "q/latest", []byte("a"), nil); err != nil {
		t.Fatal(err)
	}
	if err := d.PutIf(ctx, "q/latest", []byte("b"), nil); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("ないときだけ書く：%v", err)
	}
	if err := d.PutIf(ctx, "q/latest", []byte("b"), []byte("x")); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("違う値：%v", err)
	}
	if err := d.PutIf(ctx, "q/latest", []byte("b"), []byte("a")); err != nil {
		t.Fatal(err)
	}
	if b, _ := d.Get(ctx, "q/latest"); string(b) != "b" {
		t.Fatalf("got %q", b)
	}
	for _, bad := range []string{"", "/abs", "q/../x", "q//x", "q/./x"} {
		if err := d.Put(ctx, bad, nil); err == nil {
			t.Fatalf("%q を受け付けた", bad)
		}
	}
	// 一時ファイルは残らない。
	entries, _ := os.ReadDir(filepath.Join(d.Root, "q"))
	if len(entries) != 1 {
		t.Fatalf("entries: %v", entries)
	}
}

func TestBranchKeyEscapes(t *testing.T) {
	// a と a/b が、ファイルとディレクトリでぶつからない。
	ctx := context.Background()
	d := &Dir{Root: t.TempDir()}
	for _, name := range []string{"a", "a/b", "../x"} {
		if err := d.Put(ctx, BranchKey("q", name), []byte(name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if b, _ := d.Get(ctx, BranchKey("q", "a/b")); string(b) != "a/b" {
		t.Fatalf("got %q", b)
	}
}
