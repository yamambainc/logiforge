package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
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

// フォーマットバージョンを上げても、消えたブランチの版を読めるように、古い版の下のブランチのスナップショットを、版の外に移す。
func TestMoveLegacyBranchSnapshots(t *testing.T) {
	ctx := context.Background()
	d := &Dir{Root: t.TempDir()}
	head, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	// v4 の形：コミットの記録と差分を、版の下に分けて置いていた。
	c, _ := snapshot.Encode(&snapshot.CommitDoc{FormatVersion: 4, Commit: snapshot.Commit{Revision: head, Parent: &base, Kind: snapshot.KindBranch,
		Paths: []snapshot.PathChange{{Path: "tree/x.md", Status: snapshot.Added}}}})
	df, _ := snapshot.Encode(&snapshot.DiffDoc{FormatVersion: 4, Diff: snapshot.Diff{Base: base, Entries: []snapshot.DiffEntry{{Path: "tree/x.md", Status: snapshot.Added}}}})
	for k, v := range map[string][]byte{"v4/q/commits/" + head: c, "v4/q/snapshots/" + head: df} {
		if err := d.Put(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := PutBranchIndex(ctx, d, "q", []string{"gone"}, nil); err != nil {
		t.Fatal(err)
	}
	h := BranchHistory{Branch: "gone", Entries: []BranchEntry{{Head: head, Base: base}, {Deleted: true}}}
	if err := PutBranchHistory(ctx, d, "q", h, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := GetBranch(ctx, d, "q", head); !errors.Is(err, ErrNotFound) {
		t.Fatalf("移す前：%v", err)
	}
	if n, err := MoveLegacyBranchSnapshots(ctx, d, "q"); err != nil || n != 1 {
		t.Fatalf("移す：%d %v", n, err)
	}
	br, err := GetBranch(ctx, d, "q", head)
	if err != nil || br.Diff.Base != base || len(br.Diff.Entries) != 1 {
		t.Fatalf("移したあと：%+v %v", br, err)
	}
	if cm, err := GetCommit(ctx, d, "q", head); err != nil || cm.Kind != snapshot.KindBranch {
		t.Fatalf("コミットの記録：%+v %v", cm, err)
	}
	if n, err := MoveLegacyBranchSnapshots(ctx, d, "q"); err != nil || n != 0 {
		t.Fatalf("2 回目：%d %v", n, err)
	}
	// 古いフォーマットバージョンのまま置いたものも読める。
	old, _ := snapshot.Encode(&snapshot.Branch{FormatVersion: 4, Commit: snapshot.Commit{Revision: base, Kind: snapshot.KindBranch}, Diff: snapshot.Diff{Base: head}})
	if err := d.Put(ctx, BranchSnapshotKey("q", base), old); err != nil {
		t.Fatal(err)
	}
	if br, err := GetBranch(ctx, d, "q", base); err != nil || br.FormatVersion != 4 {
		t.Fatalf("古いフォーマットバージョン：%+v %v", br, err)
	}
}
