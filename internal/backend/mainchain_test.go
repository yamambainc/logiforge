package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/snapstore"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

// main が短く書き換えられたあと、main の並びの索引に古い SHA が残っていても、並びにあると判定しない
// （design/backend/snapshot.md の「コミットの記録から引くもの」）。
func TestMainNumberAfterShortRewrite(t *testing.T) {
	ctx := context.Background()
	r := &remote{t: t, dir: t.TempDir()}
	r.git("init", "--quiet", "--initial-branch=main")
	r.write(dH0+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H0, Statement: "根"}))
	c1 := r.commit("用意\n\nLogiforge-Init: 1")
	r.write(dH0+"/background.md", []byte("1\n"))
	c2 := r.commit("反映\n\nLogiforge-Apply-Request: apr_1")
	r.write(dH0+"/background.md", []byte("1\n2\n"))
	c3 := r.commit("反映\n\nLogiforge-Apply-Request: apr_2")

	st := &store.Dir{Root: t.TempDir()}
	w := worker.New(worker.Config{Dir: t.TempDir()}, source{r.dir}, st)
	w.SyncAll(ctx)
	b := &Backend{Snap: &snapstore.Adapter{Store: st}}
	v, err := b.mainAt(ctx, "q", "")
	if err != nil {
		t.Fatal(err)
	}
	for sha, want := range map[string]int{c1: 1, c2: 2, c3: 3} {
		if n, err := b.mainNumber(ctx, v, sha); err != nil || n != want {
			t.Fatalf("%s: %d %v", sha, n, err)
		}
	}

	// main を c2 まで短く書き換える。main/3 には、c3 が残る。
	r.git("reset", "--quiet", "--hard", c2)
	w.SyncAll(ctx)
	if got, _ := store.GetMainIndex(ctx, st, "q", 3); got != c3 {
		t.Fatalf("main/3: %s", got)
	}
	if v, err = b.mainAt(ctx, "q", ""); err != nil || v.revision() != c2 {
		t.Fatalf("latest: %v", err)
	}
	if n, _ := b.mainNumber(ctx, v, c3); n != 0 {
		t.Fatalf("書き換えで外れたコミットが、並びにあると判定された：%d", n)
	}
	if n, _ := b.mainNumber(ctx, v, c2); n != 2 {
		t.Fatalf("c2: %d", n)
	}
}

// syncMain は、テストの worker で、remote を取り込む。
func syncFixture(t *testing.T, f *fixture) {
	t.Helper()
	w := worker.New(worker.Config{Dir: t.TempDir()}, source{f.r.dir}, f.b.Snap.(*snapstore.Adapter).Store.(*store.Dir))
	w.SyncAll(context.Background())
}

// ノードの履歴は、ページに分けて、コミットの記録の previous をたどって読む。最後のページで終わる。
// カーソルが、そのノードを変えたコミットでなければ、validation_failed にする。
func TestNodeHistoryPaging(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	f.r.git("checkout", "--quiet", "main")
	f.r.write(dH2+"/background.md", []byte("1\n"))
	c3 := f.r.commit("反映\n\nLogiforge-Apply-Request: apr_3")
	f.r.write(dH2+"/background.md", []byte("1\n2\n"))
	c4 := f.r.commit("手で直した")
	syncFixture(t, f)

	p1, err := f.b.GetNodeHistory(ctx, f.caller, "q", H2, "", 2)
	if err != nil || len(p1.Items) != 2 || p1.Items[0].Commit != c4 || p1.Items[1].Commit != c3 || p1.NextCursor == nil {
		t.Fatalf("1 ページ目：%+v %v", p1, err)
	}
	p2, err := f.b.GetNodeHistory(ctx, f.caller, "q", H2, *p1.NextCursor, 2)
	if err != nil || len(p2.Items) != 1 || p2.Items[0].Commit != f.c1 || p2.NextCursor != nil {
		t.Fatalf("2 ページ目：%+v %v", p2, err)
	}
	// コミット 2（H1 の検証結果）は、H2 を変えていない。
	if _, err := f.b.GetNodeHistory(ctx, f.caller, "q", H2, "2", 2); code(err) != CodeValidationFailed {
		t.Fatalf("H2 を変えていないコミットのカーソル：%v", err)
	}
}

// 回答済みの人への依頼は、対象ノードを最後に変えた反映のコミットが回答より後なら、作業から外れる。
// 最後の変更が反映によらない push なら、その前の反映のコミットまでたどる。
func TestAnsweredRequestAfterNonApplyPush(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	f.forge.hrs[0].Answer.CreatedAt = time.Now().Add(-time.Hour)
	answered := func() bool {
		w, err := f.b.ListWork(ctx, f.caller, "q", WorkQuery{Kinds: []string{"human_request_answered"}})
		if err != nil {
			t.Fatal(err)
		}
		return len(w.Items) == 1
	}
	f.r.git("checkout", "--quiet", "main")
	// 反映によらない push だけでは、外れない（その前は、反映でない用意のコミットしかない）。
	f.r.write(dH2+"/background.md", []byte("1\n"))
	f.r.commit("手で直した")
	syncFixture(t, f)
	if !answered() {
		t.Fatal("反映によらない push で、作業から外れた")
	}
	// 反映のあとに、反映によらない push があっても、その前の反映までたどって外れる。
	f.r.write(dH2+"/background.md", []byte("1\n2\n"))
	f.r.commit("反映\n\nLogiforge-Apply-Request: apr_3")
	f.r.write(dH2+"/background.md", []byte("1\n2\n3\n"))
	f.r.commit("手で直した 2")
	syncFixture(t, f)
	if answered() {
		t.Fatal("回答のあとの反映があるのに、作業から外れない")
	}
}

// main のコミットの一覧は、新しい順に、カーソル（次に読む number）で分けて返す。
func TestListCommits(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	p, err := f.b.ListCommits(ctx, f.caller, "q", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Number != 2 || p.Items[0].Commit != f.c2 || p.Items[0].ApplyRequestID == nil || *p.Items[0].ApplyRequestID != "apr_1" {
		t.Fatalf("1 ページ目：%+v", p.Items)
	}
	if p.NextCursor == nil || *p.NextCursor != "1" {
		t.Fatalf("カーソル：%v", p.NextCursor)
	}
	p, err = f.b.ListCommits(ctx, f.caller, "q", *p.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Commit != f.c1 || p.Items[0].ApplyRequestID != nil || p.NextCursor != nil {
		t.Fatalf("2 ページ目：%+v %v", p.Items, p.NextCursor)
	}
	if _, err := f.b.ListCommits(ctx, f.caller, "q", "9", 1); code(err) != CodeValidationFailed {
		t.Fatalf("先のカーソル：%v", err)
	}
	// 索引に抜けがあれば（worker が書いている途中）、snapshot_pending。
	st := f.b.Snap.(*snapstore.Adapter).Store.(*store.Dir)
	if err := os.Remove(filepath.Join(st.Root, filepath.FromSlash(store.MainIndexKey("q", 1)))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.ListCommits(ctx, f.caller, "q", "", 10); code(err) != CodeSnapshotPending {
		t.Fatalf("索引の抜け：%v", err)
	}
}
