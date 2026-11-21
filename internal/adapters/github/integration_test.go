package github

// 実際の GitHub での結合テスト。環境変数がなければ飛ばす。
// design/adapters/github.md と design/backend/verify/README.md の「反映によるコミット」を確かめる。書くのは、いつも主体のトークンである。
//
//	GITHUB_TEST_REPO          試験の repository（owner/name）。空なら、用意から確かめる。用意のあとは、Logiforge の repository として使い続ける
//	GITHUB_TEST_TOKEN         利用者、opener、reviewer、人として使うトークン（repository の Write。gh auth login のトークンでよい）
//	GITHUB_TEST_APP_ID        worker が読むための、読むだけの GitHub App の ID。なければ、トークンなしで読む（public の repository だけ）
//	GITHUB_TEST_APP_KEY_FILE  その App の秘密鍵（PEM）のファイル

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
	"github.com/yamambainc/logiforge/internal/worker"
)

type live struct {
	t                 *testing.T
	repo, token, read string
	app               *App
	cfg               Config
	a                 *pullreq.Adapter
	auth              *Auth
	asUser, asReader  context.Context
	// asReviewer は、review を出す主体（opener と別のアカウント）。GitHub は、作者自身の approve を拒む。
	asReviewer context.Context
}

func liveEnv(t *testing.T) *live {
	for _, n := range []string{"GITHUB_TEST_REPO", "GITHUB_TEST_TOKEN", "GITHUB_TEST_REVIEWER_TOKEN"} {
		if os.Getenv(n) == "" {
			t.Skipf("%s がない（実際の GitHub での結合テスト）", n)
		}
	}
	l := &live{t: t, repo: os.Getenv("GITHUB_TEST_REPO"), token: os.Getenv("GITHUB_TEST_TOKEN")}
	l.cfg = Config{Repositories: []ports.Repository{{Name: "q", GitForgeRef: l.repo}}, MergeWait: 30 * time.Second}
	var err error
	if id := os.Getenv("GITHUB_TEST_APP_ID"); id != "" {
		key, err := os.ReadFile(os.Getenv("GITHUB_TEST_APP_KEY_FILE"))
		if err != nil {
			t.Fatal(err)
		}
		if l.app, err = NewApp(l.cfg, id, key); err != nil {
			t.Fatal(err)
		}
		if l.read, err = l.app.Token(context.Background(), l.repo); err != nil {
			t.Fatal(err)
		}
	}
	if l.a, l.auth, err = New(l.cfg); err != nil {
		t.Fatal(err)
	}
	l.asUser = ports.WithToken(context.Background(), l.token)
	l.asReader = ports.WithToken(context.Background(), l.read)
	l.asReviewer = ports.WithToken(context.Background(), os.Getenv("GITHUB_TEST_REVIEWER_TOKEN"))
	return l
}

// git は、利用者のトークンで git を動かす。
func (l *live) git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-c", "credential.helper=", "-c",
		`credential.https://github.com.helper=!f() { echo username=x-access-token; echo password="$LF_USER_TOKEN"; }; f`,
		"-c", "user.name=logiforge-test", "-c", "user.email=logiforge-test@example.com"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LF_USER_TOKEN="+l.token, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "HOME="+l.t.TempDir())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (l *live) must(dir string, args ...string) string {
	l.t.Helper()
	out, err := l.git(dir, args...)
	if err != nil {
		l.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// mergeLookup は、cmd/logiforge の lookup と同じく、merge の pull request を、読むだけのトークンで探す。
type mergeLookup struct {
	forge ports.GitForge
	src   *WorkerSource
}

func (v mergeLookup) MergedApplyRequest(ctx context.Context, r ports.Repository, sha string, committed time.Time) (string, string, error) {
	tok, err := v.src.ReadToken(ctx, r.GitForgeRef)
	if err != nil {
		return "", "", err
	}
	ar, err := v.forge.MergedApplyRequest(ports.WithToken(ctx, tok), r.GitForgeRef, sha, committed)
	if err != nil {
		return "", "", err
	}
	return ar.ID, ar.HeadRevision, nil
}

// worker は、読むだけのトークン（なければトークンなし）で、repository を取り込む worker と、その置き場を作る。
func (l *live) worker() (*worker.Worker, *store.Dir) {
	src, err := NewWorkerSource(l.cfg, l.app)
	if err != nil {
		l.t.Fatal(err)
	}
	st := &store.Dir{Root: l.t.TempDir()}
	forge, _, err := New(l.cfg)
	if err != nil {
		l.t.Fatal(err)
	}
	w := worker.New(worker.Config{Dir: l.t.TempDir(), GitCredentials: src.GitEnv, Lookup: mergeLookup{forge: forge, src: src}}, src, st)
	return w, st
}

// sync は、worker で取り込む。merge のすぐあとは、GitHub が merge のコミットから pull request を引けないことがあるので、やり直す。
func (l *live) sync(w *worker.Worker) {
	l.t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := w.Sync(context.Background(), ports.Repository{Name: "q", GitForgeRef: l.repo})
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			l.t.Fatalf("worker が取り込めない：%v", err)
		}
		l.t.Logf("取り込みのやり直し：%v", err)
		time.Sleep(5 * time.Second)
	}
}

// root は、clone した repository の、根の仮説の識別子を返す。
func (l *live) root(dir string) string {
	es, err := os.ReadDir(filepath.Join(dir, "tree"))
	if err != nil || len(es) != 1 {
		l.t.Fatalf("根の仮説がない：%v %v", es, err)
	}
	return es[0].Name()
}

// 空の repository に、利用者のトークンで用意のコミットを置く。根のコミットと、署名の付いた 2 つ目のコミットになる。
// 守る設定は、用意のあとで掛ける（2 つ目のコミットは、main への直接の更新だから）。
func (l *live) setup() {
	if _, err := l.a.MainHead(l.asUser, l.repo); !errors.Is(err, ports.ErrNotFound) {
		l.t.Logf("%s は用意済み（用意は、空のときに 1 回だけ確かめる）", l.repo)
		return
	}
	const id = "h-0000000000"
	files := []ports.File{
		{Path: "tree/" + id + "/hypothesis.md", Content: tree.MarshalHypothesis(tree.HypothesisContent{ID: id, Statement: "GitHub で、だれが merge しても中身で決まるか"})},
		{Path: "tree/" + id + "/background.md", Content: []byte("試験の背景\n")},
	}
	if err := l.a.SetupRepository(l.asUser, l.repo, files, "repository を用意した\n\nLogiforge-Init: 1"); err != nil {
		l.t.Fatalf("用意：%v", err)
	}
	if err := l.a.SetupRepository(l.asUser, l.repo, files, "x"); !errors.Is(err, ports.ErrMainExists) {
		l.t.Fatalf("2 回目の用意：%v", err)
	}
	l.t.Log("確かめた：空の repository に、利用者のトークンで、contents API の根のコミットと、Git Data API の 2 つ目のコミットを置ける")
}

// branch は、main から分けたブランチで、根の背景情報に 1 行足し、push して、apply request にする。
func (l *live) branch(dir, name, line string) ports.ApplyRequest {
	l.t.Helper()
	l.must(dir, "fetch", "--quiet", "origin")
	l.must(dir, "switch", "--quiet", "-C", name, "origin/main")
	bg := filepath.Join(dir, "tree", l.root(dir), "background.md")
	old, _ := os.ReadFile(bg)
	if err := os.WriteFile(bg, append(old, []byte(line+"\n")...), 0o644); err != nil {
		l.t.Fatal(err)
	}
	l.must(dir, "add", "-A")
	l.must(dir, "commit", "--quiet", "-m", line, "--trailer", "Logiforge-Session-Id: s-open")
	l.must(dir, "push", "--quiet", "--force", "origin", name)
	ar, err := l.a.CreateApplyRequest(l.asUser, l.repo, ports.NewApplyRequest{Branch: name, Description: "試験", Opener: "agent"})
	if err != nil {
		l.t.Fatal(err)
	}
	return ar
}

func (l *live) review(ar ports.ApplyRequest, kind ports.ReviewKind, session string) {
	l.t.Helper()
	// GitHub は、作者自身の approve を拒む。approve には、opener と別のアカウントのトークンが要る（GITHUB_TEST_REVIEWER_TOKEN）。
	if _, err := l.a.AddReview(l.asReviewer, l.repo, ar.ID, ports.Review{Kind: kind, Revision: ar.HeadRevision,
		Reviewer: "reviewer", PromptRevision: "p1"}); err != nil {
		l.t.Fatal(err)
	}
}

// humanMerge は、Logiforge を通さずに、GitHub の REST で merge する（人が GitHub の画面で merge するのと同じ）。
func (l *live) humanMerge(ar ports.ApplyRequest, method string) string {
	l.t.Helper()
	rp, _ := repoPath(l.repo)
	var res struct {
		SHA string `json:"sha"`
	}
	c := newClient("", nil)
	deadline := time.Now().Add(time.Minute)
	for {
		err := c.do(context.Background(), l.token, http.MethodPut, fmt.Sprintf("%s/pulls/%d/merge", rp, ar.Number), map[string]string{"sha": ar.HeadRevision, "merge_method": method}, &res)
		if err == nil {
			return res.SHA
		}
		if statusOf(err) != http.StatusMethodNotAllowed || time.Now().After(deadline) {
			l.t.Fatalf("人の merge：%v", err)
		}
		time.Sleep(2 * time.Second)
	}
}

// 用意から、reviewer の merge、人の merge（承認済みと、approve のあとに request_changes が付いたもの）までを、実際の GitHub で確かめる。
func TestGitHubLive(t *testing.T) {
	l := liveEnv(t)
	l.setup()
	dir := t.TempDir()
	l.must(dir, "clone", "--quiet", "https://github.com/"+l.repo+".git", ".")
	stamp := fmt.Sprint(time.Now().Unix())

	// 用意のコミット：worker は、そのまま取り込む。
	w, st := l.worker()
	l.sync(w)
	all := strings.Fields(l.must(dir, "rev-list", "--first-parent", "--reverse", "origin/main"))
	for i, sha := range all[:min(2, len(all))] {
		if _, err := store.GetCommit(context.Background(), st, "q", sha); err != nil {
			t.Fatalf("用意の %d 番目のコミット %s を取り込まない：%v", i+1, sha, err)
		}
	}

	// 1. reviewer のトークンでの反映（バックエンドの反映の手順 4 と同じ呼び方）。
	a := l.branch(dir, "lf-reviewer-"+stamp, "reviewer の反映 "+stamp)
	l.review(a, ports.ReviewApprove, "s-rev")
	shaA, err := l.a.Merge(l.asUser, l.repo, a.ID, a.HeadRevision, "反映 "+a.ID, "Logiforge-Apply-Request: "+a.ID)
	if err != nil {
		t.Fatalf("reviewer のトークンで merge できない：%v", err)
	}
	// 2. 承認された apply request を、人が merge のコミットで merge する。
	b := l.branch(dir, "lf-human-"+stamp, "人の merge "+stamp)
	l.review(b, ports.ReviewApprove, "s-rev")
	shaB := l.humanMerge(b, "merge")
	// 3. approve のあとに request_changes が付いた apply request を、人が squash merge する。
	c := l.branch(dir, "lf-changes-"+stamp, "直すよう求められた変更 "+stamp)
	l.review(c, ports.ReviewApprove, "s-rev")
	l.review(c, ports.ReviewRequestChanges, "s-rev2")
	shaC := l.humanMerge(c, "squash")

	l.must(dir, "fetch", "--quiet", "origin")
	w, st = l.worker()
	l.sync(w)
	// どの apply request の反映かは、merge の pull request で決まる。review は見ない（design/backend/invalid-main.md の「どの apply request の反映か」）。
	for sha, ar := range map[string]ports.ApplyRequest{shaA: a, shaB: b, shaC: c} {
		got, err := store.GetCommit(context.Background(), st, "q", sha)
		if err != nil || got.ApplyRequest != ar.ID || got.MergedHead != ar.HeadRevision {
			t.Errorf("%s の反映の apply request：%+v %v（%s）", sha, got, err, ar.ID)
		}
	}
	t.Log("確かめた：閉じた pull request の一覧の merge_commit_sha で、reviewer の merge と、人の merge（merge のコミット、squash）の pull request を引ける")

	// 認証：GitHub の OAuth App のトークンでないので client。読める repository を読める。
	p, err := l.auth.Authenticate(context.Background(), l.token)
	if err != nil || p.Kind != ports.PrincipalClient {
		t.Fatalf("主体：%+v %v", p, err)
	}
	if ok, err := l.auth.CanRead(context.Background(), l.token, "q"); !ok || err != nil {
		t.Fatalf("読めない：%v", err)
	}
	for _, br := range []string{a.Branch, b.Branch, c.Branch} {
		_, _ = l.git(dir, "push", "--quiet", "origin", "--delete", br)
	}
}

// TestGitHubLiveApp は、読むだけの GitHub App の installation token を確かめる。GITHUB_TEST_APP_ID がなければ飛ばす。
// GITHUB_TEST_REPO を private にすると、トークンなしでは読めないことも確かめる。
func TestGitHubLiveApp(t *testing.T) {
	l := liveEnv(t)
	if l.app == nil {
		t.Skip("GITHUB_TEST_APP_ID がない（読むだけの GitHub App）")
	}
	l.setup()
	ctx := context.Background()
	src, err := NewWorkerSource(l.cfg, l.app)
	if err != nil {
		t.Fatal(err)
	}
	// git：GitEnv の資格情報で ls-remote できる。
	env, err := src.GitEnv(ctx, l.repo)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "ls-remote", src.GitURL(l.repo), "refs/heads/main")
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_ASKPASS=", "HOME="+t.TempDir()), env...)
	if out, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(out), "refs/heads/main") {
		t.Fatalf("App のトークンで git を読めない：%v %s", err, out)
	}
	// REST：repository を読める。
	tok, err := src.ReadToken(ctx, l.repo)
	if err != nil {
		t.Fatal(err)
	}
	rp, _ := repoPath(l.repo)
	if err := newClient("", nil).do(ctx, tok, "GET", rp, nil, nil); err != nil {
		t.Fatalf("App のトークンで repository を読めない：%v", err)
	}
	t.Log("確かめた：installation token で、git（ls-remote）と REST（repository）を読める")

	// 書けない：issue を作ろうとすると断られる。
	err = newClient("", nil).do(ctx, tok, "POST", rp+"/issues", map[string]string{"title": "書けてはいけない"}, nil)
	if s := statusOf(err); s != http.StatusForbidden && s != http.StatusNotFound {
		t.Fatalf("App のトークンで issue を作れた、か違う誤り：%v", err)
	}
	t.Logf("確かめた：installation token では書けない（%v）", err)

	// private なら、トークンなしでは読めない。
	var repo struct {
		Private bool `json:"private"`
	}
	if err := newClient("", nil).do(ctx, l.token, "GET", rp, nil, &repo); err != nil {
		t.Fatal(err)
	}
	err = newClient("", nil).do(ctx, "", "GET", rp, nil, nil)
	switch {
	case repo.Private && err == nil:
		t.Fatal("private をトークンなしで読めた")
	case repo.Private:
		t.Logf("確かめた：private のリポジトリは、トークンなしでは読めない（%v）", err)
	}
}
