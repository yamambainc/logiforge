package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

// checkedSource は、反映かの判定の代わりである。merged に入れたコミットだけを、反映によるものとする（種類は kinds）。
// ほかのコミットは、merge した pull request が見つからないとする。
type checkedSource struct {
	source
	merged map[string]bool
	kinds  map[string]string
	err    error
}

func (s *checkedSource) VerifyMerge(_ context.Context, r ports.Repository, sha string, _ []string, _ string, _ time.Time, _ tree.Files, _ *store.Broken) (ports.MergeVerdict, error) {
	if r.GitForgeRef != "o/q" {
		return ports.MergeVerdict{}, errors.New("ref が違う：" + r.GitForgeRef)
	}
	if s.err != nil {
		return ports.MergeVerdict{}, s.err
	}
	if s.merged[sha] {
		return ports.MergeVerdict{Authorized: true, Kind: s.kinds[sha]}, nil
	}
	return ports.MergeVerdict{Pending: true, Detail: "merge した pull request が見つからない"}, nil
}

func checked(dir string) *checkedSource {
	return &checkedSource{source: source{dir}, merged: map[string]bool{}, kinds: map[string]string{}}
}

// 反映によらないコミットで壊れ、直すための apply request の反映で直る。trailer だけを偽ったコミットは、反映によるものにならない。
func TestBrokenAndRevert(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	src := checked(r.dir)
	st := &store.Dir{Root: t.TempDir()}
	// 時計は、読むたびに merge の直後の待ち（mergeLookupGrace）より進める。trailer を偽ったコミットは、初めて見た見回りではやり直しになり、
	// 次の見回りで決まる。
	clock := time.Now()
	w := New(Config{Dir: t.TempDir(), Verifier: src, Now: func() time.Time { clock = clock.Add(2 * mergeLookupGrace); return clock }}, src, st)
	sync := func() {
		t.Helper()
		if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err == nil {
			return
		}
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
	src.merged[fixed], src.kinds[fixed] = true, "repair.revert"
	sync()
	if br, _ := store.GetBroken(ctx, st, "q"); br != nil {
		t.Fatalf("戻したのに直らない：%+v", br)
	}
}

// main の書き換えは、壊れた状態にする。最後に正しかった時点は、書き換えの前の latest。
func TestBrokenByRewrite(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	src := checked(r.dir)
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir(), Verifier: src}, src, st)
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
	src := checked(r.dir)
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir(), Verifier: src}, src, st)
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

// merge の直後、GitForge が merge のコミットから pull request を引けないうちは、反映によらないと決めない。
// 待つ時間は、worker が初めて見た時刻から数える。コミットの時刻（未来にもできる）は使わない。
func TestBrokenWaitsForMergeLookup(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	src := checked(r.dir)
	st := &store.Dir{Root: t.TempDir()}
	now := time.Now()
	w := New(Config{Dir: t.TempDir(), Verifier: src, Now: func() time.Time { return now }}, src, st)
	sync := func() error { return w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}) }
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	r.git("checkout", "--quiet", "-b", "a1")
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("背景")
	c := r.squash("a1", "apr_1")
	if err := sync(); err == nil {
		t.Fatal("引けないうちに、決めた")
	}
	src.merged[c] = true
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	if br, _ := store.GetBroken(ctx, st, "q"); br != nil {
		t.Fatalf("壊れた：%+v", br)
	}

	// コミットの時刻を未来にした、trailer を偽ったコミット。初めて見てから待つ時間を過ぎれば、壊れた状態にする。
	r.write(h2+"/background.md", []byte("偽\n"))
	r.git("add", "-A")
	cmd := []string{"-c", "user.name=t", "commit", "--quiet", "-m", "偽\n\nLogiforge-Apply-Request: apr_x", "--date", "2099-01-01T00:00:00Z"}
	r.git(cmd...)
	if err := sync(); err == nil {
		t.Fatal("初めて見たときに、決めた")
	}
	now = now.Add(mergeLookupGrace + time.Second)
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	if br, _ := store.GetBroken(ctx, st, "q"); br == nil {
		t.Fatal("未来の時刻のコミットで、待ちが終わらない")
	}
}

// 根のコミットは、位置と trailer Logiforge-Init: で用意のコミットと決める（design/backend/verify/init.md）。trailer がなければ、取り込まない。
func TestRootNeedsInitTrailer(t *testing.T) {
	ctx := context.Background()
	for msg, ok := range map[string]bool{"用意\n\nLogiforge-Init: 1": true, "用意": false} {
		r := newRemote(t)
		r.write(h0+"/hypothesis.md", []byte("x"))
		r.commit(msg)
		src := checked(r.dir)
		st := &store.Dir{Root: t.TempDir()}
		w := New(Config{Dir: t.TempDir(), Verifier: src}, src, st)
		err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"})
		if latest, _ := store.GetLatest(ctx, st, "q"); (err == nil) != ok || (latest != "") != ok {
			t.Errorf("メッセージ %q：err %v latest %q", msg, err, latest)
		}
	}
}
