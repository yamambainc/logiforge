package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
)

// checkedSource は、反映かを GitForge の API で確かめる source の代わりである。merged に入れたコミットだけを、書き込み主体が merge したものとする。
type checkedSource struct {
	source
	merged map[string]bool
	err    error
}

func (s *checkedSource) MergedByWriter(_ context.Context, ref, sha string) (bool, error) {
	if ref != "o/q" {
		return false, errors.New("ref が違う：" + ref)
	}
	return s.merged[sha], s.err
}

// 反映によらないコミットで壊れ、戻す反映で直る。trailer だけを偽ったコミットは、反映によるものにならない。
func TestBrokenAndRevert(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	src := &checkedSource{source: source{r.dir}, merged: map[string]bool{}}
	st := &store.Dir{Root: t.TempDir()}
	// merge の直後の待ち（mergeLookupGrace）を過ぎた時刻にする。
	w := New(Config{Dir: t.TempDir(), Now: func() time.Time { return time.Now().Add(time.Hour) }}, src, st)
	sync := func() {
		t.Helper()
		if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
			t.Fatal(err)
		}
	}
	r.git("checkout", "--quiet", "-b", "a1")
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("背景")
	good := r.squash("a1", "apr_1")
	src.merged[good] = true
	sync()
	if br, _ := store.GetBroken(ctx, st, "q"); br != nil {
		t.Fatalf("反映だけなのに壊れた：%+v", br)
	}

	// trailer を偽って、main に直接置く。
	r.write(h2+"/background.md", []byte("偽\n"))
	bad := r.commit("偽の反映\n\nLogiforge-Apply-Request: apr_x")
	sync()
	br, _ := store.GetBroken(ctx, st, "q")
	if br == nil || br.LastGood != good || br.First != bad || br.Reason != string(snapshot.RuleNoApplyRequest) {
		t.Fatalf("壊れた状態：%+v", br)
	}
	c, _ := store.GetCommit(ctx, st, "q", bad)
	if len(c.Warnings) == 0 || c.Warnings[0].Rule != snapshot.RuleNoApplyRequest {
		t.Fatalf("警告：%+v", c.Warnings)
	}

	// 壊れたあとの反映（直すためでない）は、壊れたまま。
	r.git("checkout", "--quiet", "-b", "a2")
	r.write(h1+"/background.md", []byte("背景\n足す\n"))
	r.commit("足す")
	more := r.squash("a2", "apr_2")
	src.merged[more] = true
	sync()
	if br, _ := store.GetBroken(ctx, st, "q"); br == nil || br.LastGood != good {
		t.Fatalf("直すためでない反映で直った：%+v", br)
	}

	// 戻す反映：木の全体を、最後に正しかった時点（good）にする。
	r.git("checkout", "--quiet", "-b", "fix", good)
	r.git("checkout", "--quiet", "main")
	r.git("checkout", "--quiet", "-B", "fix")
	r.git("checkout", "--quiet", good, "--", ".")
	r.git("rm", "--quiet", "--cached", "-r", "--ignore-unmatch", h2+"/background.md")
	r.git("clean", "-fdq")
	r.commit("戻す\n\nLogiforge-Session-Id: s")
	r.git("checkout", "--quiet", "main")
	r.git("merge", "--quiet", "--squash", "fix")
	fixed := r.commit("反映\n\nLogiforge-Apply-Request: apr_fix\nLogiforge-Version: 1 " + r.git("rev-parse", "fix") + "\nLogiforge-Repair: revert")
	src.merged[fixed] = true
	sync()
	if br, _ := store.GetBroken(ctx, st, "q"); br != nil {
		t.Fatalf("戻したのに直らない：%+v", br)
	}
}

// main の書き換えは、壊れた状態にする。最後に正しかった時点は、書き換えの前の latest。
func TestBrokenByRewrite(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	src := &checkedSource{source: source{r.dir}, merged: map[string]bool{}}
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir()}, src, st)
	root := r.git("rev-parse", "HEAD")
	r.git("checkout", "--quiet", "-b", "a1")
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("背景")
	c2 := r.squash("a1", "apr_1")
	src.merged[c2] = true
	if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
		t.Fatal(err)
	}
	r.git("reset", "--quiet", "--hard", root)
	if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
		t.Fatal(err)
	}
	br, _ := store.GetBroken(ctx, st, "q")
	if br == nil || br.LastGood != c2 || br.Reason != string(RuleMainRewritten) {
		t.Fatalf("書き換え：%+v", br)
	}
}

// 判定できないコミットは、取り込まない（latest を進めない）。
func TestBrokenCheckUnavailable(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	src := &checkedSource{source: source{r.dir}, merged: map[string]bool{}}
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir()}, src, st)
	if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
		t.Fatal(err)
	}
	root, _ := store.GetLatest(ctx, st, "q")
	r.write(h1+"/background.md", []byte("x\n"))
	r.commit("x")
	src.err = errors.New("GitForge が止まっている")
	if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err == nil {
		t.Fatal("判定できないのに取り込んだ")
	}
	if l, _ := store.GetLatest(ctx, st, "q"); l != root {
		t.Fatalf("latest が進んだ：%s", l)
	}
}

// merge の直後、GitForge が merge のコミットから pull request を引けないうちは、反映の記録のあるコミットを、反映によらないと決めない。
func TestBrokenWaitsForMergeLookup(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	src := &checkedSource{source: source{r.dir}, merged: map[string]bool{}}
	st := &store.Dir{Root: t.TempDir()}
	now := time.Now()
	w := New(Config{Dir: t.TempDir(), Now: func() time.Time { return now }}, src, st)
	r.git("checkout", "--quiet", "-b", "a1")
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("背景")
	c := r.squash("a1", "apr_1")
	if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err == nil {
		t.Fatal("引けないうちに、決めた")
	}
	src.merged[c] = true
	if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
		t.Fatal(err)
	}
	if br, _ := store.GetBroken(ctx, st, "q"); br != nil {
		t.Fatalf("壊れた：%+v", br)
	}
}
