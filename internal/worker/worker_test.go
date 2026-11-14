package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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
	// m1 は、latest にならなかったので、記録だけがあり、木はない。
	c1, err := store.GetCommit(ctx, st, "q", m1)
	if err != nil || len(c1.Warnings) != 0 || c1.Number != 2 {
		t.Fatalf("m1: %+v %v", c1, err)
	}
	if ok, _ := store.HasTree(ctx, st, "q", m1); ok {
		t.Fatal("latest でないコミットに、木を作った")
	}
	// 「木を作る」で、あとから作れる。history とノードの変更の一覧は、m1 までである。
	if err := w.BuildTree(ctx, "q", m1); err != nil {
		t.Fatal(err)
	}
	s1m, err := store.GetMain(ctx, st, "q", m1)
	if err != nil || len(s1m.Tree.History) != 2 || len(s1m.Tree.Nodes["h-3333333333"].Changes) != 2 {
		t.Fatalf("m1 の木：%+v %v", s1m, err)
	}
	if err := w.BuildTree(ctx, "q", strings.Repeat("b", 40)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("記録のないコミット：%v", err)
	}
	s2m, err := store.GetMain(ctx, st, "q", m2)
	if err != nil {
		t.Fatal(err)
	}
	if got := rules(s2m.Commit.Warnings); len(got) != 1 || got[0] != snapshot.RuleOverlap || s2m.Commit.Warnings[0].Node != "h-3333333333" {
		t.Fatalf("重なりの警告：%v", s2m.Commit.Warnings)
	}
	if !s2m.Tree.History[2].WorkerWarned {
		t.Fatalf("history: %+v", s2m.Tree.History)
	}

	// 何度動かしても、結果は同じ。
	before, _ := st.Get(ctx, store.CommitKey("q", m2))
	syncRepo(t, w)
	after, _ := st.Get(ctx, store.CommitKey("q", m2))
	if !bytes.Equal(before, after) {
		t.Fatal("2 回目の巡回で、スナップショットが変わった")
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

func TestBranchHistoryAndInternalAPI(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	r.git("checkout", "--quiet", "-b", "feature/a")
	r.write(h2+"/background.md", []byte("一\n"))
	v1 := r.commit("v1")
	w, st := newWorker(t, r)
	srv := httptest.NewServer(w.Handler("secret"))
	defer srv.Close()

	post := func(key, path string, body any) *http.Response {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(b))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// ヘルスチェックは、鍵なしで 200 を返す。
	if res, err := http.Get(srv.URL + PathHealthz); err != nil || res.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", res, err)
	}
	// 巡回も取り込みも、間隔の数倍のあいだ進まなければ、503 を返す。
	w.progress.Store(time.Now().Add(-stallFactor*w.cfg.Interval - time.Second).UnixNano())
	if res, err := http.Get(srv.URL + PathHealthz); err != nil || res.StatusCode != 503 {
		t.Fatalf("止まっているのに healthz が 503 でない：%v %v", res, err)
	}
	w.touch()
	if res := post("", PathWake, WakeRequest{"q"}); res.StatusCode != 401 {
		t.Fatalf("鍵がないのに受け付けた：%d", res.StatusCode)
	}
	if res := post("wrong", PathReadBranch, ReadBranchRequest{"q", "feature/a"}); res.StatusCode != 401 {
		t.Fatalf("鍵が違うのに受け付けた：%d", res.StatusCode)
	}
	res := post("secret", PathReadBranch, ReadBranchRequest{"q", "feature/a"})
	var got BranchResult
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != 200 || got.Head != v1 {
		t.Fatalf("read-branch: %d %+v %v", res.StatusCode, got, err)
	}
	if _, err := store.GetBranch(ctx, st, "q", v1); err != nil {
		t.Fatal(err)
	}
	// 「ブランチを読む」は、base の main のスナップショットも作る。
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != r.git("rev-parse", "main") {
		t.Fatalf("read-branch のあとの latest：%q", latest)
	}
	if res := post("secret", PathReadBranch, ReadBranchRequest{"q", "nothing"}); res.StatusCode != 404 {
		t.Fatalf("ないブランチ：%d", res.StatusCode)
	}
	if res := post("secret", PathWake, WakeRequest{"q"}); res.StatusCode != 202 {
		t.Fatalf("wake: %d", res.StatusCode)
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

// fetch のあと、「消えた」を書く前に止まっても、次の巡回で、消えたことを記録する。
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

// 前の形の版の置き場があっても、worker は今の版の接頭辞に作り直して進む。
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

// 「ブランチを読む」の中の main の取り込みが、上限までに終わらなければ、ErrSnapshotPending を返し、取り込みは裏で続ける。
func TestReadBranchPendingKeepsIngesting(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	for i := 0; i < 30; i++ {
		r.write(h2+"/background.md", []byte(fmt.Sprintf("%d\n", i)))
		r.commit(fmt.Sprintf("反映 %d\n\nLogiforge-Apply-Request: apr_%d", i, i))
	}
	mainSHA := r.git("rev-parse", "main")
	r.git("checkout", "--quiet", "-b", "b")
	r.write(h1+"/background.md", []byte("b\n"))
	r.commit("b")
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir(), ReadBranchWait: time.Nanosecond}, source{r.dir}, st)
	if _, err := w.ReadBranch(ctx, "q", "b"); !errors.Is(err, ErrSnapshotPending) {
		t.Fatalf("got %v", err)
	}
	// 裏の取り込みの最中の、2 つ目の「ブランチを読む」も、ロックを待ちきれずに、上限で ErrSnapshotPending を返す。
	w.cfg.ReadBranchWait = 20 * time.Millisecond
	start := time.Now()
	if _, err := w.ReadBranch(ctx, "q", "b"); !errors.Is(err, ErrSnapshotPending) || time.Since(start) > 2*time.Second {
		t.Fatalf("2 つ目：%v %v", err, time.Since(start))
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if latest, _ := store.GetLatest(ctx, st, "q"); latest == mainSHA {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("裏の取り込みが終わらない")
		}
		time.Sleep(50 * time.Millisecond)
	}
	w.cfg.ReadBranchWait = time.Minute
	if res, err := w.ReadBranch(ctx, "q", "b"); err != nil || res.Base != mainSHA {
		t.Fatalf("取り込みのあと：%+v %v", res, err)
	}
}

// 裏の取り込みは、Run の ctx が終われば止まる。
func TestBackgroundIngestStopsWithRun(t *testing.T) {
	r := setupRemote(t)
	for i := 0; i < 30; i++ {
		r.write(h2+"/background.md", []byte(fmt.Sprintf("%d\n", i)))
		r.commit(fmt.Sprintf("反映 %d\n\nLogiforge-Apply-Request: apr_%d", i, i))
	}
	r.git("checkout", "--quiet", "-b", "b")
	r.write(h1+"/background.md", []byte("b\n"))
	r.commit("b")
	st := &store.Dir{Root: t.TempDir()}
	w := New(Config{Dir: t.TempDir(), Interval: time.Hour, ReadBranchWait: time.Nanosecond}, source{r.dir}, st)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond) // Run が ctx を持つまで
	// Run の最初の巡回が、ロックを持っているかもしれない。その間は、すぐに ErrSnapshotPending になる。
	if _, err := w.ReadBranch(context.Background(), "q", "b"); err != nil && !errors.Is(err, ErrSnapshotPending) {
		t.Fatal(err)
	}
	cancel()
	<-done
	// 止めたあとは、ロックが放される（裏の取り込みも止まっている）。
	deadline := time.Now().Add(10 * time.Second)
	for {
		if unlock := w.tryLock("q", time.Now()); unlock != nil {
			unlock()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run を止めても、裏の取り込みがロックを放さない")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// 初めての取り込み（記録を順に作り、最後に木を 1 回作る）と、ふだんの取り込み（1 コミットずつ前の木に重ねる）で、
// コミットの記録と今の main の木が同じになる。違うのは、「新しく破れた不変条件」の付け方だけである。
// 初めての取り込みでは、今の木で破れているもの全体を、最後のコミットの警告にする。
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
	for _, sha := range shas {
		a, err1 := store.GetCommit(ctx, allSt, "q", sha)
		b, err2 := store.GetCommit(ctx, incSt, "q", sha)
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		// 「新しく破れた不変条件」は、下で別に確かめる。
		notBroken := func(v tree.Violation) bool { return v.Rule == tree.RuleOutsideTree }
		a.Warnings = slices.DeleteFunc(a.Warnings, notBroken)
		b.Warnings = slices.DeleteFunc(b.Warnings, notBroken)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%s の記録が違う：\n初めて：%+v\nふだん：%+v", sha, a, b)
		}
	}
	latest := shas[len(shas)-1]
	a, _ := store.GetMain(ctx, allSt, "q", latest)
	b, _ := store.GetMain(ctx, incSt, "q", latest)
	if a == nil || b == nil {
		t.Fatal("今の main の木がない")
	}
	if !b.Tree.History[len(b.Tree.History)-1].NewlyBroken || !a.Tree.History[len(a.Tree.History)-1].NewlyBroken {
		t.Fatalf("新しく破れた不変条件の印：ふだん %+v、初めて %+v", b.Tree.History, a.Tree.History)
	}
	if c, _ := store.GetCommit(ctx, allSt, "q", latest); !contains(rules(c.Warnings), tree.RuleOutsideTree) || !strings.Contains(c.Warnings[len(c.Warnings)-1].Detail, "まとめて取り込んだ") {
		t.Fatalf("最後のコミットの、まとめて取り込んだときの警告：%+v", c.Warnings)
	}
	if !reflect.DeepEqual(a.Tree, b.Tree) {
		t.Fatalf("今の main の木が違う：\n初めて：%+v\nふだん：%+v", a.Tree, b.Tree)
	}
	if !contains(rules(a.Tree.Broken), tree.RuleOutsideTree) {
		t.Fatalf("今の main の木の Broken：%v", a.Tree.Broken)
	}
	// 重なりと inactive 化の子孫は、初めての取り込みでも求める。
	if c, _ := store.GetCommit(ctx, allSt, "q", shas[1]); !contains(rules(c.Warnings), snapshot.RuleOverlap) {
		t.Fatalf("重なりの警告：%v", c.Warnings)
	}
	if rs := a.Tree.Nodes["h-6666666666"].Changes; len(rs) != 2 || rs[1].Revision != shas[3] {
		t.Fatalf("inactive 化した子孫の変更の一覧：%v", rs)
	}
}

// 前の latest の木があり、新しいコミットが多いとき（まとめての取り込み）は、始めの木と比べて新しく破れたものだけを、
// 最後のコミットの警告にする。始めの木から破れていたものは、警告にしない。
func TestBulkImportNewlyBroken(t *testing.T) {
	ctx := context.Background()
	r := setupRemote(t)
	r.write("README.md", []byte("前から破れている"))
	r.commit("手で直した")
	w, st := newWorker(t, r)
	syncRepo(t, w)
	var first, last string
	for i := 0; i <= incrementalLimit; i++ {
		r.write(fmt.Sprintf("%s/background.md", h2), []byte(fmt.Sprintf("背景 %d\n", i)))
		last = r.commit("反映\n\nLogiforge-Apply-Request: apr_" + fmt.Sprint(i))
		if first == "" {
			first = last
		}
	}
	r.write("NOTES.md", []byte("新しく破れた"))
	last = r.commit("反映\n\nLogiforge-Apply-Request: apr_last")
	syncRepo(t, w)
	c, err := store.GetCommit(ctx, st, "q", last)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range c.Warnings {
		if v.Rule == tree.RuleOutsideTree {
			got = append(got, v.Node)
			if !strings.Contains(v.Detail, "まとめて取り込んだ "+first+".."+last) {
				t.Fatalf("detail: %q", v.Detail)
			}
		}
	}
	if !reflect.DeepEqual(got, []string{"NOTES.md"}) {
		t.Fatalf("新しく破れた不変条件：%v", c.Warnings)
	}
	m, _ := store.GetMain(ctx, st, "q", last)
	if !m.Tree.History[len(m.Tree.History)-1].NewlyBroken {
		t.Fatal("history の印がない")
	}
}
