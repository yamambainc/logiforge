package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// remote は、テスト用の、作業ツリーつきのリモートのリポジトリである。
type remote struct {
	t   *testing.T
	dir string
}

func newRemote(t *testing.T) *remote {
	r := &remote{t: t, dir: t.TempDir()}
	r.git("init", "--quiet", "--initial-branch=main")
	return r
}

func (r *remote) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *remote) write(p string, content []byte) {
	r.t.Helper()
	full := filepath.Join(r.dir, p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *remote) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "--quiet", "--allow-empty", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// squash は、branch の先頭の木を、main に 1 つのコミットとして反映する（GitForge の squash merge の代わり）。
func (r *remote) squash(branch, ar string) string {
	r.t.Helper()
	head := r.git("rev-parse", branch)
	r.git("checkout", "--quiet", "main")
	r.git("merge", "--quiet", "--squash", branch)
	return r.commit("反映\n\nLogiforge-Apply-Request: " + ar + "\nLogiforge-Version: 1 " + head)
}

type source struct{ dir string }

func (s source) ListRepositories(context.Context) ([]ports.Repository, error) {
	return []ports.Repository{{Name: "q", GitForgeRef: "o/q"}}, nil
}
func (s source) GitURL(string) string { return s.dir }
func (s source) GitForgeRef(_ context.Context, name string) (string, error) {
	return "o/" + name, nil
}

const (
	h0 = "tree/h-0000000000"
	s1 = h0 + "/s-1111111111"
	h1 = s1 + "/h-3333333333"
	h2 = s1 + "/h-4444444444"
)

func setupRemote(t *testing.T) *remote {
	r := newRemote(t)
	r.write(h0+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-0000000000", Statement: "根"}))
	r.write(s1+"/story.md", tree.MarshalStory(tree.StoryContent{ID: "s-1111111111", Title: "t", Summary: "s"}))
	r.write(h1+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-3333333333", Statement: "子1"}))
	r.write(h2+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-4444444444", Statement: "子2"}))
	r.commit("用意\n\nLogiforge-Init: 1")
	return r
}

func newWorker(t *testing.T, r *remote) (*Worker, *store.Dir) {
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC) }}, source{r.dir}, st)
	return w, st
}

func syncRepo(t *testing.T, w *Worker) {
	t.Helper()
	if err := w.Sync(context.Background(), ports.Repository{Name: "q", GitForgeRef: "o/q"}); err != nil {
		t.Fatal(err)
	}
}

func rules(vs []tree.Violation) []tree.Rule {
	var out []tree.Rule
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	return out
}

func TestSyncMainAndOverlap(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	base := r.git("rev-parse", "main")

	// 2 つのブランチが、どちらも H1 を対象にする（検証結果と背景情報）。
	r.git("checkout", "--quiet", "-b", "x", base)
	art := []byte("data")
	r.write(tree.ArtifactPath(tree.SHA256Hex(art)), art)
	r.write(h1+"/verdicts/v-7777777777.md", tree.MarshalVerdict(tree.VerdictContent{ID: "v-7777777777", Value: true, Report: "r\n",
		Artifacts: []tree.ArtifactRef{{Kind: "file", Name: "d", SHA256: tree.SHA256Hex(art)}}}))
	r.commit("x")
	r.git("checkout", "--quiet", "-b", "y", base)
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("y")

	w, st := newWorker(t, r)
	syncRepo(t, w)
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != base {
		t.Fatalf("latest: %s", latest)
	}
	for _, b := range []string{"x", "y"} {
		h, _, _ := store.GetBranchHistory(ctx, st, "q", b)
		if len(h.Entries) != 1 || h.Entries[0].Base != base {
			t.Fatalf("%s: %+v", b, h)
		}
		br, err := store.GetBranch(ctx, st, "q", h.Entries[0].Head)
		if err != nil || len(br.Diff.Entries) == 0 {
			t.Fatalf("branch snapshot: %+v %v", br, err)
		}
	}
	if _, err := st.Get(ctx, store.ArtifactKey("q", tree.SHA256Hex(art))); err != nil {
		t.Fatalf("ブランチの artifact を置いていない：%v", err)
	}

	m1 := r.squash("x", "apr_x")
	m2 := r.squash("y", "apr_y")
	syncRepo(t, w)
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != m2 {
		t.Fatalf("latest: %s", latest)
	}
	// m1 も、スナップショットツリーと main の並びの索引がある。
	c1, err := store.GetCommit(ctx, st, "q", m1)
	if err != nil || len(c1.Warnings) != 0 || c1.Number != 2 {
		t.Fatalf("m1: %+v %v", c1, err)
	}
	if got, _ := store.GetMainIndex(ctx, st, "q", 2); got != m1 {
		t.Fatalf("main の並びの索引 2：%s", got)
	}
	s1m, err := store.GetMain(ctx, st, "q", m1)
	if err != nil || s1m.SnapshotTree.Nodes["h-3333333333"].LastChanged.Number != 2 {
		t.Fatalf("m1 のスナップショットツリー：%+v %v", s1m, err)
	}
	s2m, err := store.GetMain(ctx, st, "q", m2)
	if err != nil {
		t.Fatal(err)
	}
	if got := rules(s2m.Commit.Warnings); len(got) != 1 || got[0] != snapshot.RuleOverlap || s2m.Commit.Warnings[0].Node != "h-3333333333" {
		t.Fatalf("重なりの警告：%v", s2m.Commit.Warnings)
	}
	if w := s2m.SnapshotTree.Warned; len(w) != 1 || w[0].Number != 3 || !w[0].WorkerWarned {
		t.Fatalf("warned: %+v", w)
	}

	// 何度動かしても、結果は同じ。
	before, _ := st.Get(ctx, store.CommitKey("q", m2))
	syncRepo(t, w)
	after, _ := st.Get(ctx, store.CommitKey("q", m2))
	if !bytes.Equal(before, after) {
		t.Fatal("2 回目の見回りで、スナップショットが変わった")
	}

	// ブランチを消すと、履歴に消えたことを足す。
	r.git("branch", "-D", "x")
	syncRepo(t, w)
	if h, _, _ := store.GetBranchHistory(ctx, st, "q", "x"); len(h.Entries) != 2 || !h.Entries[1].Deleted {
		t.Fatalf("消えたブランチ：%+v", h)
	}

	// main が書き換えられたら、latest を上書きし、警告する。
	r.git("checkout", "--quiet", "main")
	r.git("reset", "--quiet", "--hard", base)
	r.write("README.md", []byte("x"))
	m3 := r.commit("書き換え")
	syncRepo(t, w)
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != m3 {
		t.Fatalf("latest: %s", latest)
	}
	s3m, _ := store.GetMain(ctx, st, "q", m3)
	if got := rules(s3m.Commit.Warnings); !contains(got, RuleMainRewritten) || !contains(got, snapshot.RuleNoApplyRequest) {
		t.Fatalf("書き換えの警告：%v", got)
	}
	// 書き換えのあとの新しいコミットで、main の並びの索引が上書きされる。
	if s3m.Commit.Number != 2 {
		t.Fatalf("m3 の番号：%d", s3m.Commit.Number)
	}
	if got, _ := store.GetMainIndex(ctx, st, "q", 2); got != m3 {
		t.Fatalf("main の並びの索引 2：%s（m1 は %s）", got, m1)
	}
	w.SyncAll(ctx)
	if names, _ := store.GetRepositories(ctx, st); len(names) != 1 || names[0] != "q" {
		t.Fatalf("repositories: %v", names)
	}
}

func contains(rs []tree.Rule, r tree.Rule) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

func TestOverlapUnchecked(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	r.write(h2+"/background.md", []byte("x\n"))
	sha := r.commit("反映\n\nLogiforge-Apply-Request: apr_1\nLogiforge-Version: 1 " + strings.Repeat("a", 40))
	w, st := newWorker(t, r)
	syncRepo(t, w)
	m, _ := store.GetMain(ctx, st, "q", sha)
	if got := rules(m.Commit.Warnings); len(got) != 1 || got[0] != snapshot.RuleOverlapUnchecked {
		t.Fatalf("warnings: %v", got)
	}
}

func TestBranchHistory(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	r.git("checkout", "--quiet", "-b", "feature/a")
	r.write(h2+"/background.md", []byte("一\n"))
	v1 := r.commit("v1")
	w, st := newWorker(t, r)
	syncRepo(t, w)
	if _, err := store.GetBranch(ctx, st, "q", v1); err != nil {
		t.Fatal(err)
	}

	// 先頭が変わるたびに、履歴に足す。同じ先頭に戻っても足す（A → B → A）。
	r.write(h2+"/background.md", []byte("一\n二\n"))
	v2 := r.commit("v2")
	syncRepo(t, w)
	r.git("reset", "--quiet", "--hard", v1)
	syncRepo(t, w)
	syncRepo(t, w)
	h, _, _ := store.GetBranchHistory(ctx, st, "q", "feature/a")
	var heads []string
	for _, e := range h.Entries {
		heads = append(heads, e.Head)
	}
	if want := []string{v1, v2, v1}; strings.Join(heads, ",") != strings.Join(want, ",") {
		t.Fatalf("履歴：%v", heads)
	}
}

// fetch のあと、「消えた」を書く前に止まっても、次の見回りで、消えたことを記録する。
func TestDeletedBranchSurvivesStop(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	r.git("checkout", "--quiet", "-b", "x")
	r.write(h2+"/background.md", []byte("x\n"))
	r.commit("x")
	r.git("checkout", "--quiet", "main")
	w, st := newWorker(t, r)
	syncRepo(t, w)
	r.git("branch", "-D", "x")
	// worker が fetch したところで止まった：手元の ref は、すでに消えている。
	g, err := w.repo(ctx, "q")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Fetch(ctx, r.dir); err != nil {
		t.Fatal(err)
	}
	syncRepo(t, w)
	if h, _, _ := store.GetBranchHistory(ctx, st, "q", "x"); len(h.Entries) != 2 || !h.Entries[1].Deleted {
		t.Fatalf("消えたことを記録していない：%+v", h)
	}
}

// 前のフォーマットバージョンの置き場があっても、worker は今のフォーマットバージョンの接頭辞に作り直して進む。
func TestOldFormatStoreIsIgnored(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	w, st := newWorker(t, r)
	if err := st.Put(ctx, "v1/q/latest", []byte("garbage\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(ctx, "q/latest", []byte("garbage\n")); err != nil {
		t.Fatal(err)
	}
	syncRepo(t, w)
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != r.git("rev-parse", "main") {
		t.Fatalf("latest: %q", latest)
	}
}

func TestSecondWorkerDoesNotStart(t *testing.T) {
	dir := t.TempDir()
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("2 つ目の worker が起動した：%v", err)
	}
	release()
	// 置き場の根を共有する worker も、起動しない。
	storeDir := t.TempDir()
	r1, err := LockAll(t.TempDir(), storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockAll(t.TempDir(), storeDir); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("置き場を共有する worker が起動した：%v", err)
	}
	r1()
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("止まったあとに起動できない：%v", err)
	}
	again()
}

// 溜まったコミットをまとめて取り込んでも、1 つずつ取り込んでも、コミットの記録とスナップショットツリーは同じになる。
// どちらも、古い順に 1 つずつ、すべてのコミットにスナップショットツリーを作るからである。
func TestInitialImportMatchesIncremental(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	inc, incSt := newWorker(t, r)
	syncRepo(t, inc)
	var shas []string
	step := func(sha string) {
		shas = append(shas, sha)
		syncRepo(t, inc)
	}
	base := r.git("rev-parse", "main")
	// 2 つのブランチが、どちらも H1 を対象にする（重なりの警告が出る）。
	r.git("checkout", "--quiet", "-b", "x", base)
	r.write(h1+"/verdicts/v-7777777777.md", tree.MarshalVerdict(tree.VerdictContent{ID: "v-7777777777", Value: true, Report: "r\n"}))
	r.commit("x")
	r.git("checkout", "--quiet", "-b", "y", base)
	r.write(h1+"/background.md", []byte("背景\n"))
	r.commit("y")
	step(r.squash("x", "apr_x"))
	step(r.squash("y", "apr_y"))
	// 新しいストーリーと、inactive 化。
	r.write(h2+"/s-5555555555/story.md", tree.MarshalStory(tree.StoryContent{ID: "s-5555555555", Title: "t2", Summary: "s"}))
	r.write(h2+"/s-5555555555/h-6666666666/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-6666666666", Statement: "孫"}))
	step(r.commit("反映\n\nLogiforge-Apply-Request: apr_z"))
	r.write(h2+"/s-5555555555/inactivated.md", []byte("理由\n"))
	step(r.commit("反映\n\nLogiforge-Apply-Request: apr_w"))
	// 木の外のファイル（不変条件が破れる）と、反映の記録のないコミット。
	r.write("README.md", []byte("x"))
	step(r.commit("手で直した"))

	all, allSt := newWorker(t, r)
	syncRepo(t, all)
	for i, sha := range shas {
		a, err1 := store.GetMain(ctx, allSt, "q", sha)
		b, err2 := store.GetMain(ctx, incSt, "q", sha)
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%d 番目の %s が違う：\nまとめて：%+v\n1 つずつ：%+v", i, sha, a, b)
		}
		if got, _ := store.GetMainIndex(ctx, allSt, "q", a.Commit.Number); got != sha {
			t.Fatalf("main の並びの索引 %d：%s", a.Commit.Number, got)
		}
	}
	// 新しく破れた不変条件は、破ったコミットにだけ付く。
	latest := shas[len(shas)-1]
	for _, sha := range shas {
		c, _ := store.GetCommit(ctx, allSt, "q", sha)
		if got := contains(rules(c.Warnings), tree.RuleOutsideTree); got != (sha == latest) {
			t.Fatalf("%s の警告：%v", sha, c.Warnings)
		}
	}
	m, _ := store.GetMain(ctx, allSt, "q", latest)
	// 3 は重なり、4 と 5 は Logiforge-Version: がないので overlap_unchecked、6 は反映の記録がなく、新しく破った。
	if w := m.SnapshotTree.Warned; len(w) != 4 || w[0].Number != 3 || !w[0].WorkerWarned || w[3].Number != 6 || !w[3].NewlyBroken || !w[3].NoApplyRequest {
		t.Fatalf("warned: %+v", w)
	}
	// 重なりと inactive 化の子孫。
	if c, _ := store.GetCommit(ctx, allSt, "q", shas[1]); !contains(rules(c.Warnings), snapshot.RuleOverlap) {
		t.Fatalf("重なりの警告：%v", c.Warnings)
	}
	if lc := m.SnapshotTree.Nodes["h-6666666666"].LastChanged; lc.Revision != shas[3] {
		t.Fatalf("inactive 化した子孫を最後に変えたコミット：%v", lc)
	}
	if n := m.SnapshotTree.Nodes["s-5555555555"]; n.InactivatedAt != 5 {
		t.Fatalf("inactivated_at: %+v", n)
	}
}

// 取り込みが途中で止まっても、次の見回りで、latest の次のコミットから続ける。
func TestResumeFromLatest(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	r.write("README.md", []byte("前から破れている"))
	before := r.commit("手で直した")
	w, st := newWorker(t, r)
	syncRepo(t, w)
	var shas []string
	for i := 0; i < 5; i++ {
		r.write(h2+"/background.md", []byte(fmt.Sprintf("背景 %d\n", i)))
		shas = append(shas, r.commit("反映\n\nLogiforge-Apply-Request: apr_"+fmt.Sprint(i)))
	}
	r.write("NOTES.md", []byte("新しく破れた"))
	last := r.commit("反映\n\nLogiforge-Apply-Request: apr_last")
	syncRepo(t, w)
	// 3 番目のコミットの記録と索引を置き、スナップショットツリーを置く前に止まった状態にする：
	// 3 番目より先は記録も消し、3 番目はスナップショットツリーだけを消し、latest を 2 番目に戻す。
	for i, sha := range append(shas[2:], last) {
		keys := []string{store.SnapshotKey("q", sha)}
		if i > 0 {
			keys = append(keys, store.CommitKey("q", sha))
		}
		for _, k := range keys {
			if err := os.Remove(filepath.Join(st.Root, filepath.FromSlash(k))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Remove(filepath.Join(st.Root, filepath.FromSlash(store.LatestKey("q")))); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLatest(ctx, st, "q", shas[1], ""); err != nil {
		t.Fatal(err)
	}
	syncRepo(t, w)
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != last {
		t.Fatalf("latest: %s", latest)
	}
	c, err := store.GetCommit(ctx, st, "q", last)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range c.Warnings {
		if v.Rule == tree.RuleOutsideTree {
			got = append(got, v.Node)
		}
	}
	if !reflect.DeepEqual(got, []string{"NOTES.md"}) {
		t.Fatalf("新しく破れた不変条件（README.md は前から破れている）：%v", c.Warnings)
	}
	for _, sha := range append(shas, last) {
		if ok, _ := store.HasSnapshotTree(ctx, st, "q", sha); !ok {
			t.Fatalf("%s のスナップショットツリーがない", sha)
		}
	}
	if c, _ := store.GetCommit(ctx, st, "q", before); !contains(rules(c.Warnings), tree.RuleOutsideTree) {
		t.Fatalf("README.md を足したコミットの警告：%v", c.Warnings)
	}
}

// Run は、見回りの間隔ごとにハートビートを書き、repository の処理を待たずに次の見回りを始める。
// 前の回の処理が終わっていない repository は、その回は飛ばす。
func TestRunHeartbeatAndSkipBusy(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	st := &store.Dir{Root: t.TempDir()}
	dir := t.TempDir()
	w := New(Config{Dir: dir, Interval: 10 * time.Millisecond}, source{r.dir}, st)
	// repository の処理を、ロックを持ったまま止めておく（長い取り込みの代わり）。
	unlock := w.tryLock("q")
	if unlock == nil {
		t.Fatal("ロックを取れない")
	}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { _ = w.Run(rctx); close(done) }()
	time.Sleep(100 * time.Millisecond)
	// 処理中の repository は飛ばすが、ハートビートは書き続ける。
	if err := CheckHeartbeat(dir, 10*time.Millisecond, time.Now()); err != nil {
		t.Fatalf("ハートビート：%v", err)
	}
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != "" {
		t.Fatalf("ロック中なのに取り込んだ：%s", latest)
	}
	unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if latest, _ := store.GetLatest(ctx, st, "q"); latest == r.git("rev-parse", "main") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ロックを放したあとも、取り込まない")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if names, _ := store.GetRepositories(ctx, st); len(names) != 1 || names[0] != "q" {
		t.Fatalf("repositories: %v", names)
	}
	cancel()
	<-done
	// 止まったあと、間隔の 10 倍がたてば、古いとみなす。
	if err := CheckHeartbeat(dir, 10*time.Millisecond, time.Now().Add(time.Second)); err == nil {
		t.Fatal("古いハートビートを、新しいとみなした")
	}
	if err := CheckHeartbeat(t.TempDir(), time.Second, time.Now()); err == nil {
		t.Fatal("ハートビートがないのに、新しいとみなした")
	}
}
