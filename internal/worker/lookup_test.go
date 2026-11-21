package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/gitx"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

// fakeLookup は、merge の pull request を探す GitForge の代わりである。merged に入れたコミットだけが、pull request を持つ。
// ほかのコミットは、まだ見つからない（ports.ErrNotFound）とする。
type fakeLookup struct {
	merged map[string]string
	err    error
}

func (l *fakeLookup) MergedApplyRequest(_ context.Context, r ports.Repository, sha string, _ time.Time) (string, string, error) {
	if r.GitForgeRef != "o/q" {
		return "", "", errors.New("ref が違う：" + r.GitForgeRef)
	}
	if l.err != nil {
		return "", "", l.err
	}
	if id, ok := l.merged[sha]; ok {
		return id, "", nil
	}
	return "", "", ports.ErrNotFound
}

// force push で main を書き換えても、新しい並びをそのまま受け入れる。警告にしない（design/backend/invalid-main.md の「main はいつも正しい」）。
func TestForcePushAccepted(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir()}, source{r.dir}, st)
	sync := func() {
		t.Helper()
		if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
			t.Fatal(err)
		}
	}
	root := r.git("rev-parse", "HEAD")
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("背景")
	sync()
	// 取り込み終えたコミットまで短くする。latest は、そのコミットに戻る。
	r.git("reset", "--quiet", "--hard", root)
	sync()
	if l, _ := store.GetLatest(ctx, st, "q"); l != root {
		t.Fatalf("短くした main：latest %s", l)
	}
	// 別の並びに書き換える。新しいコミットを、そのまま取り込む。
	r.write(h2+"/background.md", []byte("別\n"))
	c := r.commit("別")
	sync()
	m, err := store.GetMain(ctx, st, "q", c)
	if err != nil || len(m.Commit.Warnings) != 0 || !m.SnapshotTree.Valid || m.SnapshotTree.ValidCommit != c {
		t.Fatalf("書き換えたあと：%+v %v", m, err)
	}
}

// どの apply request の反映かは、merge の pull request を探して置く。見つからなければ、初めて見てから待つ時間のあいだは、やり直す。
// 待つ時間は、worker が初めて見た時刻から数える。コミットの時刻（未来にもできる）は使わない。過ぎたら、反映の apply request のないコミットとして置く。
func TestMergeLookupWaits(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	l := &fakeLookup{merged: map[string]string{}}
	st := &store.Dir{Root: t.TempDir()}
	now := time.Now()
	w := New(Config{Dir: t.TempDir(), Lookup: l, Now: func() time.Time { return now }}, source{r.dir}, st)
	sync := func() error { return w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}) }
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	r.git("checkout", "--quiet", "-b", "a1")
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("背景")
	c := r.squash("a1", "apr_1")
	if err := sync(); err == nil {
		t.Fatal("見つからないうちに、取り込んだ")
	}
	l.merged[c] = "7"
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetCommit(ctx, st, "q", c); got.ApplyRequest != "7" {
		t.Fatalf("反映の apply request：%+v", got)
	}

	// コミットの時刻を未来にした、直接の push。初めて見てから待つ時間を過ぎれば、反映の apply request のないコミットとして置く。
	r.write(h2+"/background.md", []byte("直接\n"))
	r.git("add", "-A")
	r.git("-c", "user.name=t", "commit", "--quiet", "-m", "直接", "--date", "2099-01-01T00:00:00Z")
	d := r.git("rev-parse", "HEAD")
	if err := sync(); err == nil {
		t.Fatal("初めて見たときに、取り込んだ")
	}
	now = now.Add(mergeLookupGrace + time.Second)
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetCommit(ctx, st, "q", d); err != nil || got.ApplyRequest != "" {
		t.Fatalf("直接の push：%+v %v", got, err)
	}

	// GitForge が答えないときは、取り込まない（latest を進めない）。
	r.write(h2+"/background.md", []byte("直接\n二\n"))
	r.commit("二")
	l.err = errors.New("GitForge が止まっている")
	if err := sync(); err == nil {
		t.Fatal("探せないのに取り込んだ")
	}
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != d {
		t.Fatalf("latest が進んだ：%s", latest)
	}
}

// 根のコミットは、trailer によらず取り込む。木が不変条件を満たさなければ、valid でないだけである。
func TestRootIsJustValidOrNot(t *testing.T) {
	ctx := context.Background()
	for content, valid := range map[string]bool{"": false, "ok": true} {
		r := newRemote(t)
		if content == "ok" {
			r.write(h0+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-0000000000", Statement: "根"}))
		} else {
			r.write("README.md", []byte("x"))
		}
		root := r.commit("最初")
		st := &store.Dir{Root: t.TempDir()}
		w := New(Config{Dir: t.TempDir()}, source{r.dir}, st)
		if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
			t.Fatal(err)
		}
		m, err := store.GetMain(ctx, st, "q", root)
		if err != nil || m.SnapshotTree.Valid != valid {
			t.Errorf("%q：%+v %v", content, m, err)
		}
		if !valid && (m.SnapshotTree.ValidCommit != "" || m.SnapshotTree.InvalidSince != root) {
			t.Errorf("valid でない根：%q %q", m.SnapshotTree.ValidCommit, m.SnapshotTree.InvalidSince)
		}
	}
}

// git がファイルを読めないとき（一時的な誤りなど）は、取り込まずに、次の見回りでやり直す。latest を進めない。
func TestReadFailureRetries(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir()}, source{r.dir}, st)
	sync := func() error { return w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}) }
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	root, _ := store.GetLatest(ctx, st, "q")
	r.write(h1+"/background.md", []byte("背景\n"))
	c := r.commit("背景")
	w.testApplyChanges = func(context.Context, *gitx.Repo, tree.Files, []gitx.Change) (tree.Files, error) {
		return nil, errors.New("git cat-file が止まった")
	}
	if err := sync(); err == nil {
		t.Fatal("読めないのに取り込んだ")
	}
	if l, _ := store.GetLatest(ctx, st, "q"); l != root {
		t.Fatalf("latest が進んだ：%s", l)
	}
	w.testApplyChanges = nil
	if err := sync(); err != nil {
		t.Fatal(err)
	}
	if m, err := store.GetMain(ctx, st, "q", c); err != nil || !m.SnapshotTree.Valid {
		t.Fatalf("やり直したあと：%+v %v", m, err)
	}
}

// 大きさの上限を超えるファイルは、中身を読まない。そのコミットは too_large で valid でなく、
// そのファイルを変えないあとのコミットも、too_large のまま valid でない（形の誤りにも、valid にもならない）。
func TestTooLargeStaysInvalid(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir(), MaxFileSize: 64}, source{r.dir}, st)
	sync := func() {
		t.Helper()
		if err := w.Sync(ctx, ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
			t.Fatal(err)
		}
	}
	sync()
	r.write(h1+"/background.md", []byte(strings.Repeat("長い背景\n", 20)))
	big := r.commit("大きい背景")
	r.write(h2+"/background.md", []byte("小さい\n"))
	after := r.commit("ほかのファイル")
	sync()
	for _, sha := range []string{big, after} {
		m, err := store.GetMain(ctx, st, "q", sha)
		if err != nil || m.SnapshotTree.Valid || !hasViolation(m.SnapshotTree.Violations, tree.RuleTooLarge) || hasViolation(m.SnapshotTree.Violations, tree.RuleParse) {
			t.Fatalf("%s：%+v %v", sha, m.SnapshotTree.Violations, err)
		}
		if m.SnapshotTree.InvalidSince != big {
			t.Fatalf("%s の invalid_since：%s", sha, m.SnapshotTree.InvalidSince)
		}
	}
}

func hasViolation(vs []tree.Violation, rule tree.Rule) bool {
	for _, v := range vs {
		if v.Rule == rule {
			return true
		}
	}
	return false
}
