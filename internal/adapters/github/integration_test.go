package github

// 実際の GitHub での結合テスト。環境変数がなければ飛ばす。
// design/adapters/github.md と design/backend/verify.md の「確かめること」を確かめる。書くのは、いつも主体のトークンである。
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
	"github.com/yamambainc/logiforge/internal/adapters/snapstore"
	"github.com/yamambainc/logiforge/internal/backend"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
	"github.com/yamambainc/logiforge/internal/worker"
)

type live struct {
	t                 *testing.T
	repo, token, read string
	app               *App
	cfg               Config
	keys              pullreq.Keyring
	a                 *pullreq.Adapter
	auth              *Auth
	asUser, asReader  context.Context
}

func liveEnv(t *testing.T) *live {
	for _, n := range []string{"GITHUB_TEST_REPO", "GITHUB_TEST_TOKEN"} {
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
	if l.keys, err = pullreq.KeyringFromSecrets([]string{"k1"}, []string{"github-integration-test"}); err != nil {
		t.Fatal(err)
	}
	if l.a, l.auth, err = New(l.cfg, l.keys); err != nil {
		t.Fatal(err)
	}
	l.asUser = ports.WithToken(context.Background(), l.token)
	l.asReader = ports.WithToken(context.Background(), l.read)
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

// verifier は、cmd/logiforge の verifier と同じく、worker の判定をバックエンドの規則で、読むだけのトークンで行う。
type verifier struct {
	b    *backend.Backend
	src  *WorkerSource
	keys pullreq.Keyring
}

func (v verifier) VerifyMerge(ctx context.Context, r ports.Repository, sha string, parents []string, prevVersion string, files tree.Files, broken *store.Broken) (ports.MergeVerdict, error) {
	tok, err := v.src.ReadToken(ctx, r.GitForgeRef)
	if err != nil {
		return ports.MergeVerdict{}, err
	}
	return v.b.VerifyMerge(ports.WithToken(ctx, tok), r.Name, r.GitForgeRef, sha, parents, prevVersion, files, broken)
}

func (v verifier) VerifyInit(ctx context.Context, r ports.Repository, kind, parent, migratedFrom, sig string, added map[string]string) (bool, error) {
	id, err := v.src.RepoID(ctx, r.GitForgeRef)
	if err != nil {
		return false, err
	}
	var fs []pullreq.InitFile
	for p, s := range added {
		fs = append(fs, pullreq.InitFile{Path: p, SHA256: s})
	}
	return v.keys.VerifyInit(pullreq.InitScope{Kind: kind, RepoID: id, Directory: r.Directory(), Parent: parent, MigratedFrom: migratedFrom}, fs, sig) == nil, nil
}

// worker は、読むだけのトークン（なければトークンなし）で、repository を取り込む worker と、その置き場を作る。
func (l *live) worker() (*worker.Worker, *store.Dir, verifier) {
	src, err := NewWorkerSource(l.cfg, l.app)
	if err != nil {
		l.t.Fatal(err)
	}
	st := &store.Dir{Root: l.t.TempDir()}
	pub := l.keys.VerifyOnly()
	forge, _, err := New(l.cfg, pub)
	if err != nil {
		l.t.Fatal(err)
	}
	forge.Log = snapstore.RecordLog{Store: st}
	v := verifier{b: &backend.Backend{Forge: forge, Snap: &snapstore.Adapter{Store: st}}, src: src, keys: pub}
	w := worker.New(worker.Config{Dir: l.t.TempDir(), GitCredentials: src.GitEnv, Verifier: v, Records: snapstore.LogSyncer{Source: src, Keys: pub, Store: st}}, src, st)
	return w, st, v
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
	head := l.must(dir, "rev-parse", "HEAD")
	l.must(dir, "push", "--quiet", "--force", "origin", name)
	ar, err := l.a.CreateApplyRequest(l.asUser, l.repo, ports.NewApplyRequest{ID: "apr_" + name, Branch: name, Description: "試験", FirstRevision: head, Opener: "agent"})
	if err != nil {
		l.t.Fatal(err)
	}
	return ar
}

func (l *live) review(ar ports.ApplyRequest, kind ports.ReviewKind, session string) {
	l.t.Helper()
	if err := l.a.AddReview(l.asUser, l.repo, ar.ID, ports.Review{ID: "rev_" + session + fmt.Sprint(time.Now().UnixNano()), Kind: kind, Revision: ar.HeadRevision,
		Reviewer: "reviewer", Session: session, PromptRevision: "p1"}); err != nil {
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

// verdict は、取り込んだ main のコミットについて、壊れていない前提で、反映かを判定し直す。
// 試験の repository は、前の回の「反映によらないコミット」を含んで壊れているので、壊れていることの記録は渡さない。
func (l *live) verdict(v verifier, st *store.Dir, dir, sha string) ports.MergeVerdict {
	l.t.Helper()
	m, err := store.GetMain(context.Background(), st, "q", sha)
	if err != nil {
		l.t.Fatalf("%s を取り込んでいない：%v", sha, err)
	}
	files, err := m.SnapshotTree.Files()
	if err != nil {
		l.t.Fatal(err)
	}
	parents := strings.Fields(l.must(dir, "rev-list", "--parents", "-n", "1", sha))[1:]
	// 1 つ前の版は、コミットの記録の parent である（ディレクトリの repository では、git の親とは限らない）。
	c, err := store.GetCommit(context.Background(), st, "q", sha)
	if err != nil || c.Parent == nil {
		l.t.Fatalf("%s のコミットの記録：%v", sha, err)
	}
	got, err := v.VerifyMerge(context.Background(), ports.Repository{Name: "q", GitForgeRef: l.repo}, sha, parents, *c.Parent, files, nil)
	if err != nil {
		l.t.Fatal(err)
	}
	return got
}

// 用意から、reviewer の merge、人の merge（承認済みと、approve のあとに request_changes が付いたもの）までを、実際の GitHub で確かめる。
func TestGitHubLive(t *testing.T) {
	l := liveEnv(t)
	l.setup()
	dir := t.TempDir()
	l.must(dir, "clone", "--quiet", "https://github.com/"+l.repo+".git", ".")
	stamp := fmt.Sprint(time.Now().Unix())

	// 用意のコミット：根に Logiforge-Init:、2 つ目に署名。worker は、どちらも反映によるものとする。
	w, st, _ := l.worker()
	l.sync(w)
	all := strings.Fields(l.must(dir, "rev-list", "--first-parent", "--reverse", "origin/main"))
	for i, sha := range all[:min(2, len(all))] {
		c, err := store.GetCommit(context.Background(), st, "q", sha)
		if err != nil || len(c.Warnings) != 0 {
			t.Fatalf("用意の %d 番目のコミット %s に警告：%+v %v", i+1, sha, c.Warnings, err)
		}
	}
	t.Log("確かめた：worker は、根のコミットと、署名の合う 2 つ目のコミットを、用意のコミットとする")

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
	w, st, v := l.worker()
	l.sync(w)
	if got := l.verdict(v, st, dir, shaA); !got.Authorized || got.ApplyRequest != a.ID {
		t.Errorf("reviewer の merge：%+v", got)
	}
	if got := l.verdict(v, st, dir, shaB); !got.Authorized || got.ApplyRequest != b.ID {
		t.Errorf("人の merge のコミット（承認済み）：%+v", got)
	}
	if got := l.verdict(v, st, dir, shaC); got.Authorized || !strings.Contains(got.Detail, "approve でない") {
		t.Errorf("request_changes のあとの人の merge：%+v", got)
	}
	t.Log("確かめた：GET /commits/{sha}/pulls で merge した pull request を引ける。reviewer の merge と、承認済みの人の merge（merge のコミット）は反映、request_changes のあとの人の squash merge は反映でない")
	c3, _ := store.GetCommit(context.Background(), st, "q", shaC)
	if !hasWarning(c3, snapshot.RuleNoApplyRequest) {
		t.Errorf("request_changes のあとの merge に、警告がない：%+v", c3.Warnings)
	}
	if br, _ := store.GetBroken(context.Background(), st, "q"); br == nil {
		t.Error("壊れた状態にならない")
	}

	// 記録の控え：worker が見たあとに request_changes の記録を消しても、控えに残り、判定は変わらない。
	logged, _ := store.GetRecords(context.Background(), st, mustRepoID(t, l), c.Number)
	if len(logged) < 3 {
		t.Fatalf("控えに記録がない：%+v", logged)
	}

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

func mustRepoID(t *testing.T, l *live) string {
	id, err := api{&forge{cfg: l.cfg, c: newClient("", nil)}}.RepoID(l.asReader, l.repo)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func hasWarning(c snapshot.Commit, rule tree.Rule) bool {
	for _, w := range c.Warnings {
		if w.Rule == rule {
			return true
		}
	}
	return false
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
	// REST：RepoID と記録のコメントを読める。
	if _, err := src.RepoID(ctx, l.repo); err != nil {
		t.Fatalf("App のトークンで RepoID を読めない：%v", err)
	}
	if _, err := src.CommentsSince(ctx, l.repo, time.Time{}); err != nil {
		t.Fatalf("App のトークンでコメントを読めない：%v", err)
	}
	t.Log("確かめた：installation token で、git（ls-remote）と REST（repository、コメント）を読める")

	// 書けない：issue を作ろうとすると断られる。
	tok, err := src.ReadToken(ctx, l.repo)
	if err != nil {
		t.Fatal(err)
	}
	rp, _ := repoPath(l.repo)
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
	anon, err := NewWorkerSource(l.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = anon.CommentsSince(ctx, l.repo, time.Time{})
	switch {
	case repo.Private && err == nil:
		t.Fatal("private をトークンなしで読めた")
	case repo.Private:
		t.Logf("確かめた：private のリポジトリは、トークンなしでは読めない（%v）", err)
	}
}
