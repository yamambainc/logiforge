package forgejo

// 動く Forgejo での結合テスト。FORGEJO_URL と FORGEJO_ADMIN_TOKEN がなければ飛ばす。
// Forgejo は scripts/forgejo-test.sh で起動する。
// design/adapters/forgejo.md の「作るときに確かめること」を、ここで確かめる。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/pullreq"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

type env struct {
	t      *testing.T
	url    string
	admin  string
	org    string
	tokens map[string]string
}

func setup(t *testing.T) *env {
	u, admin := os.Getenv("FORGEJO_URL"), os.Getenv("FORGEJO_ADMIN_TOKEN")
	if u == "" || admin == "" {
		t.Skip("FORGEJO_URL と FORGEJO_ADMIN_TOKEN がない（scripts/forgejo-test.sh で Forgejo を起動する）")
	}
	e := &env{t: t, url: u, admin: admin, org: fmt.Sprintf("lf%d", time.Now().UnixNano()%1e9), tokens: map[string]string{}}
	e.call(admin, "POST", "/orgs", map[string]any{"username": e.org, "visibility": "private"}, nil, 201)
	writers := e.team("writers", map[string]any{"permission": "write", "can_create_org_repo": true,
		"units_map": map[string]string{"repo.code": "write", "repo.pulls": "write", "repo.issues": "write"}})
	users := e.team("logiforge-users", map[string]any{"permission": "read",
		"units_map": map[string]string{"repo.code": "write", "repo.pulls": "read", "repo.issues": "read"}})
	clients := e.team("agents", map[string]any{"permission": "read",
		"units_map": map[string]string{"repo.code": "write", "repo.pulls": "read", "repo.issues": "read"}})
	readers := e.team("readers", map[string]any{"permission": "read",
		"units_map": map[string]string{"repo.code": "read"}})
	for name, team := range map[string]int64{"writer": writers, "alice": users, "agent": clients, "outsider": 0, "reader": readers} {
		login := e.org + name
		e.call(admin, "POST", "/admin/users", map[string]any{"username": login, "email": login + "@example.com", "password": "pw-" + login + "-12345", "must_change_password": false}, nil, 201)
		scopes := []string{"all"}
		if name == "reader" {
			scopes = []string{"read:repository", "read:organization"}
		}
		e.tokens[name] = e.token(login, scopes)
		if team != 0 {
			e.call(admin, "PUT", fmt.Sprintf("/teams/%d/members/%s", team, login), nil, nil, 204)
		}
	}
	return e
}

func (e *env) login(name string) string { return e.org + name }

func (e *env) team(name string, body map[string]any) int64 {
	body["name"] = name
	body["includes_all_repositories"] = true
	var out struct {
		ID int64 `json:"id"`
	}
	e.call(e.admin, "POST", "/orgs/"+e.org+"/teams", body, &out, 201)
	return out.ID
}

func (e *env) token(login string, scopes []string) string {
	b, _ := json.Marshal(map[string]any{"name": "t", "scopes": scopes})
	req, _ := http.NewRequest("POST", e.url+"/api/v1/users/"+login+"/tokens", bytes.NewReader(b))
	req.SetBasicAuth(login, "pw-"+login+"-12345")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		SHA1 string `json:"sha1"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || out.SHA1 == "" {
		e.t.Fatalf("token: %v %d", err, res.StatusCode)
	}
	return out.SHA1
}

// call は、API を呼び、状態コードを返す。want があれば、違うときに止める。
func (e *env) call(token, method, path string, in, out any, want ...int) int {
	e.t.Helper()
	var body *bytes.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.url+"/api/v1"+path, body)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	if len(want) > 0 && res.StatusCode != want[0] {
		e.t.Fatalf("%s %s: %d", method, path, res.StatusCode)
	}
	return res.StatusCode
}

// git は、手元の設定から切り離して git を動かす。
func git(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(t, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}

func (e *env) clone(who, repo string) string {
	dir := e.t.TempDir()
	u, _ := url.Parse(e.url)
	u.User = url.UserPassword(e.login(who), e.tokens[who])
	if who == "root" {
		u.User = url.UserPassword("root", e.admin)
	}
	u.Path = "/" + repo + ".git"
	mustGit(e.t, dir, "clone", "-q", u.String(), ".")
	return dir
}

func (e *env) commitPush(dir, branch, path, content string) string {
	mustGit(e.t, dir, "checkout", "-q", "-B", branch, "origin/main")
	full := filepath.Join(dir, path)
	_ = os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	mustGit(e.t, dir, "add", "-A")
	mustGit(e.t, dir, "commit", "-q", "-m", "版\n\nLogiforge-Session-Id: s1")
	mustGit(e.t, dir, "push", "-q", "-f", "origin", branch)
	return mustGit(e.t, dir, "rev-parse", "HEAD")
}

var keys = pullreq.Keyring{Current: "k1", Keys: map[string][]byte{"k1": []byte("secret")}}

const root = "tree/h-0000000000/hypothesis.md"

func TestForgejo(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cfg := Config{URL: e.url, Org: e.org, UserTeam: "logiforge-users", Writer: e.login("writer"), WriterToken: e.tokens["writer"], MergeWait: 5 * time.Second}
	gf, auth := New(cfg, keys)

	// ---- repository の用意 ----
	ref, _ := gf.GitForgeRef(ctx, "q")
	files := []ports.File{{Path: root, Content: []byte("---\nid: h-0000000000\nstatement: 根\n---\n")}}
	if err := gf.SetupRepository(ctx, ref, files, "用意\n\nLogiforge-Init: 1"); err != nil {
		t.Fatalf("確かめること：書き込み主体が organization に repository を作り、空の repository に contents で最初のコミットを置ける：%v", err)
	}
	t.Log("確かめた：team の can_create_org_repo で、書き込み主体が organization に repository を作れる")
	t.Log("確かめた：auto_init なしの空の repository に、POST /contents で最初のコミットを置ける")
	if err := gf.SetupRepository(ctx, ref, files, "x"); !errors.Is(err, ports.ErrMainExists) {
		t.Fatalf("2 回目：%v", err)
	}
	// 守る設定が消えていても、呼び直せば作り直す。
	e.call(e.admin, "DELETE", "/repos/"+ref+"/branch_protections/main", nil, nil, 204)
	_ = gf.SetupRepository(ctx, ref, files, "x")
	e.call(e.admin, "GET", "/repos/"+ref+"/branch_protections/main", nil, nil, 200)
	rs, err := gf.ListRepositories(ctx)
	if err != nil || len(rs) != 1 || rs[0].GitForgeRef != ref {
		t.Fatalf("list: %v %v", rs, err)
	}

	// ---- main を守る ----
	alice := e.clone("alice", ref)
	head1 := e.commitPush(alice, "topic", "tree/h-0000000000/background.md", "一\n")
	if out, err := git(t, alice, "push", "-q", "origin", "topic:main"); err == nil {
		t.Fatalf("利用者が main に push できた：%s", out)
	}
	adminDir := e.clone("root", ref)
	_ = os.WriteFile(filepath.Join(adminDir, "x"), []byte("x"), 0o644)
	mustGit(t, adminDir, "add", "-A")
	mustGit(t, adminDir, "commit", "-q", "-m", "x")
	if out, err := git(t, adminDir, "push", "-q", "origin", "main"); err == nil {
		t.Fatalf("確かめること：管理者が enable_push: false を越えて main に push できた：%s", out)
	}
	t.Log("確かめた：サイトの管理者でも、enable_push: false の main には push できない（pre-receive で拒まれる）")

	// ---- 提案 ----
	ar, err := gf.CreateApplyRequest(ctx, ref, ports.NewApplyRequest{ID: "apr_1", Branch: "topic", Description: "説明", FirstRevision: head1, Opener: e.login("agent")})
	if err != nil || ar.State != ports.StateOpen || ar.HeadRevision != head1 {
		t.Fatalf("create: %+v %v", ar, err)
	}
	if again, err := gf.CreateApplyRequest(ctx, ref, ports.NewApplyRequest{ID: "apr_1", Branch: "topic", FirstRevision: head1}); err != nil || again.Number != ar.Number {
		t.Fatalf("送り直し：%+v %v", again, err)
	}
	if _, err := gf.CreateApplyRequest(ctx, ref, ports.NewApplyRequest{ID: "apr_9", Branch: "topic", FirstRevision: head1}); !errors.Is(err, ports.ErrBranchHasOpen) {
		t.Fatalf("同じブランチ：%v", err)
	}
	got, err := gf.GetApplyRequest(ctx, ref, "apr_1")
	if err != nil || got.FirstRevision != head1 || got.Opener != e.login("agent") || got.Description != "説明" {
		t.Fatalf("get: %+v %v", got, err)
	}

	// 読むだけの team の利用者は、記録のコメントを編集・削除できず、pull request を close できない。
	var comments []struct {
		ID int64 `json:"id"`
	}
	e.call(e.admin, "GET", fmt.Sprintf("/repos/%s/issues/%d/comments", ref, ar.Number), nil, &comments, 200)
	cid := comments[0].ID
	for _, c := range []struct{ method, path string }{
		{"PATCH", fmt.Sprintf("/repos/%s/issues/comments/%d", ref, cid)},
		{"DELETE", fmt.Sprintf("/repos/%s/issues/comments/%d", ref, cid)},
		{"PATCH", fmt.Sprintf("/repos/%s/pulls/%d", ref, ar.Number)},
	} {
		var in any = map[string]string{"body": "x", "state": "closed"}
		if s := e.call(e.tokens["alice"], c.method, c.path, in, nil); s != 403 {
			t.Fatalf("確かめること：読むだけの team の人が %s %s できた：%d", c.method, c.path, s)
		}
	}
	t.Log("確かめた：issue と pull request が読むだけの team の人は、他人のコメントを編集・削除できず、pull request も close できない（403）")

	// refs/pull/<index>/head を fetch できる。
	mustGit(t, alice, "fetch", "-q", "origin", fmt.Sprintf("refs/pull/%d/head:refs/remotes/pr", ar.Number))
	if got := mustGit(t, alice, "rev-parse", "refs/remotes/pr"); got != head1 {
		t.Fatalf("refs/pull: %s", got)
	}
	t.Log("確かめた：refs/pull/<index>/head を、git の fetch で読める")

	// ---- レビュー ----
	if err := gf.AddReview(ctx, ref, "apr_1", ports.Review{ID: "rev_1", Kind: ports.ReviewApprove, Body: "よい", Revision: head1, Reviewer: e.login("agent"), Session: "s2", PromptRevision: "p1"}); err != nil {
		t.Fatal(err)
	}
	reviews, err := gf.ListReviews(ctx, ref, "apr_1")
	if err != nil || reviews.ChainBroken || len(reviews.Items) != 1 || reviews.Items[0].Kind != ports.ReviewApprove || reviews.Items[0].Revision != head1 || reviews.Items[0].Session != "s2" {
		t.Fatalf("reviews: %+v %v", reviews, err)
	}

	// ---- 反映 ----
	if _, err := gf.Merge(ctx, ref, "apr_1", strings.Repeat("0", 40), "t", "m"); !errors.Is(err, ports.ErrHeadMoved) {
		t.Fatalf("違う head：%v", err)
	}
	sha, err := gf.Merge(ctx, ref, "apr_1", head1, "反映", "本文\n\nLogiforge-Apply-Request: apr_1")
	if err != nil || len(sha) != 40 {
		t.Fatalf("merge: %q %v", sha, err)
	}
	if mh, err := gf.MainHead(ctx, ref); err != nil || mh != sha {
		t.Fatalf("main head: %q %v", mh, err)
	}
	mustGit(t, alice, "fetch", "-q", "origin")
	if msg := mustGit(t, alice, "log", "-1", "--format=%B", "origin/main"); !strings.Contains(msg, "Logiforge-Apply-Request: apr_1") || mustGit(t, alice, "rev-parse", "origin/main") != sha {
		t.Fatalf("反映のコミット：%s", msg)
	}
	if got, _ := gf.GetApplyRequest(ctx, ref, "apr_1"); got.State != ports.StateMerged {
		t.Fatalf("state: %v", got.State)
	}
	// 送り直しは、同じ反映のコミットを返す。
	if again, err := gf.Merge(ctx, ref, "apr_1", head1, "反映", "本文"); err != nil || again != sha {
		t.Fatalf("merge の送り直し：%q %v", again, err)
	}
	if _, err := gf.Merge(ctx, ref, "apr_1", strings.Repeat("1", 40), "反映", "本文"); !errors.Is(err, ports.ErrHeadMoved) {
		t.Fatalf("merge 済みで違う head：%v", err)
	}

	// ---- 衝突 ----
	hA := e.commitPush(alice, "a", "tree/h-0000000000/background.md", "一\n二\n")
	hB := e.commitPush(alice, "b", "tree/h-0000000000/background.md", "一\n三\n")
	for id, br := range map[string]string{"apr_a": "a", "apr_b": "b"} {
		if _, err := gf.CreateApplyRequest(ctx, ref, ports.NewApplyRequest{ID: id, Branch: br, FirstRevision: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gf.Merge(ctx, ref, "apr_a", hA, "a", "Logiforge-Apply-Request: apr_a"); err != nil {
		t.Fatal(err)
	}
	if _, err := gf.Merge(ctx, ref, "apr_b", hB, "b", "Logiforge-Apply-Request: apr_b"); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("衝突：%v", err)
	}
	if err := gf.RecordMergeFailure(ctx, ref, "apr_b", ports.MergeFailure{Revision: hB, Reason: "conflict", Detail: "衝突"}); err != nil {
		t.Fatal(err)
	}

	// ---- close と reopen ----
	if err := gf.Close(ctx, ref, "apr_b", "やめる", "s3"); err != nil {
		t.Fatal(err)
	}
	e.call(e.admin, "PATCH", fmt.Sprintf("/repos/%s/pulls/%d", ref, mustGet(t, gf, ref, "apr_b").Number), map[string]string{"state": "open"}, nil, 201)
	if b := mustGet(t, gf, ref, "apr_b"); b.State != ports.StateClosed || b.CloseReason != "やめる" || len(b.MergeFailures) != 1 {
		t.Fatalf("reopen のあと：%+v", b)
	}

	// ---- ブランチを消すと閉じる ----
	hC := e.commitPush(alice, "c", "tree/h-0000000000/background.md", "一\n二\n四\n")
	if _, err := gf.CreateApplyRequest(ctx, ref, ports.NewApplyRequest{ID: "apr_c", Branch: "c", FirstRevision: hC}); err != nil {
		t.Fatal(err)
	}
	mustGit(t, alice, "push", "-q", "origin", "--delete", "c")
	var c ports.ApplyRequest
	for i := 0; i < 20; i++ {
		if c = mustGet(t, gf, ref, "apr_c"); c.State == ports.StateClosed {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if c.State != ports.StateClosed {
		t.Fatalf("確かめること：ブランチを消しても pull request が閉じない：%v", c.State)
	}
	t.Log("確かめた：head のブランチを消すと、pull request は閉じる")

	// ---- 編集された記録は数えない ----
	var cs []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	e.call(e.admin, "GET", fmt.Sprintf("/repos/%s/issues/%d/comments", ref, c.Number), nil, &cs, 200)
	e.call(e.admin, "PATCH", fmt.Sprintf("/repos/%s/issues/comments/%d", ref, cs[0].ID), map[string]string{"body": strings.Replace(cs[0].Body, "apr_c", "apr_x", 1)}, nil, 200)
	if _, err := gf.GetApplyRequest(ctx, ref, "apr_x"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("編集した記録を数えた：%v", err)
	}

	// ---- 人への依頼と回答 ----
	hr, err := gf.CreateHumanRequest(ctx, ref, ports.NewHumanRequest{ID: "hrq_1", Kind: ports.HumanRequestPVR, TargetNode: "h-0000000000", Body: "測ってください\n詳しく", Requester: e.login("agent"), Session: "s4"})
	if err != nil || hr.Kind != ports.HumanRequestPVR {
		t.Fatalf("起票：%+v %v", hr, err)
	}
	photo := bytes.Repeat([]byte{0xff, 0xd8, 0x00}, 1000)
	if err := gf.Answer(ctx, ref, "hrq_1", "測りました", e.login("alice"), []ports.NewAttachment{{Name: "photo.jpg", Content: photo}}); err != nil {
		t.Fatal(err)
	}
	if err := gf.Answer(ctx, ref, "hrq_1", "もう一度", e.login("alice"), nil); !errors.Is(err, ports.ErrAlreadyAnswered) {
		t.Fatalf("2 回目の回答：%v", err)
	}
	got1, err := gf.GetHumanRequest(ctx, ref, "hrq_1")
	if err != nil || got1.Answer == nil || got1.Answer.AnsweredBy != e.login("alice") || len(got1.Answer.Attachments) != 1 || got1.TargetNode != "h-0000000000" {
		t.Fatalf("get: %+v %v", got1, err)
	}
	t.Log("確かめた：添付を足したあとも、記録の MAC は合う（MAC は updated_at に頼らない）")
	data, err := gf.ReadAttachment(ctx, ref, "hrq_1", got1.Answer.Attachments[0].SHA256)
	if err != nil || !bytes.Equal(data, photo) {
		t.Fatalf("添付：%d %v", len(data), err)
	}
	list, err := gf.ListHumanRequests(ctx, ref)
	if err != nil || len(list) != 1 || list[0].Answer == nil {
		t.Fatalf("list: %+v %v", list, err)
	}
	ars, err := gf.ListApplyRequests(ctx, ref)
	if err != nil || len(ars) != 3 { // apr_1、apr_a、apr_b（apr_c の記録は編集されたので数えない）
		t.Fatalf("list: %d %v", len(ars), err)
	}

	// ---- repository の名前を変えても、記録は壊れない（MAC は repository の id で求める） ----
	e.call(e.admin, "PATCH", "/repos/"+ref, map[string]string{"name": "q-renamed"}, nil, 200)
	renamed := e.org + "/q-renamed"
	if got, err := gf.GetApplyRequest(ctx, renamed, "apr_b"); err != nil || got.State != ports.StateClosed || got.CloseReason != "やめる" {
		t.Fatalf("名前を変えたあと：%+v %v", got, err)
	}
	if got, err := gf.ListReviews(ctx, renamed, "apr_1"); err != nil || got.ChainBroken || len(got.Items) != 1 {
		t.Fatalf("名前を変えたあとのレビュー：%+v %v", got, err)
	}
	e.call(e.admin, "PATCH", "/repos/"+renamed, map[string]string{"name": "q"}, nil, 200)

	// ---- worker：読むだけのユーザーで、repository を列挙し、git を読む ----
	src := NewWorkerSource(cfg, e.login("reader"), e.tokens["reader"])
	st := &store.Dir{Root: t.TempDir()}
	// テストの Forgejo は http で動くので、http を許す（本番は https だけ）。
	w := worker.New(worker.Config{Dir: t.TempDir(), GitEnv: append(src.GitEnv(), "GIT_ALLOW_PROTOCOL=http:https:file")}, src, st)
	w.SyncAll(ctx)
	mustGit(t, alice, "fetch", "-q", "origin")
	if latest, _ := store.GetLatest(ctx, st, "q"); latest != mustGit(t, alice, "rev-parse", "origin/main") {
		t.Fatalf("worker の latest：%q", latest)
	}
	if h, _, _ := store.GetBranchHistory(ctx, st, "q", "a"); len(h.Entries) != 1 {
		t.Fatalf("worker のブランチの履歴：%+v", h)
	}
	m, err := store.GetMain(ctx, st, "q", mustGit(t, alice, "rev-parse", "origin/main"))
	if err != nil || m.Commit.Number < 3 {
		t.Fatalf("worker のスナップショット：%v", err)
	}
	t.Log("確かめた：read:repository と read:organization のトークンで、worker が repository を列挙し、credential helper で git を読める")

	// ---- 認証 ----
	for who, want := range map[string]ports.PrincipalKind{"alice": ports.PrincipalUser, "agent": ports.PrincipalClient} {
		p, err := auth.Authenticate(ctx, e.tokens[who])
		if err != nil || p.Kind != want || p.Login != e.login(who) {
			t.Fatalf("%s: %+v %v", who, p, err)
		}
	}
	for _, tok := range []string{e.tokens["outsider"], "bad-token", e.tokens["writer"]} {
		if _, err := auth.Authenticate(ctx, tok); !errors.Is(err, ports.ErrUnauthenticated) {
			t.Fatalf("organization の外：%v", err)
		}
	}
	if ok, err := auth.CanRead(ctx, e.tokens["alice"], "q"); !ok || err != nil {
		t.Fatalf("can read: %v %v", ok, err)
	}
	if ok, _ := auth.CanRead(ctx, e.tokens["outsider"], "q"); ok {
		t.Fatal("organization の外の人が読める")
	}
}

func mustGet(t *testing.T, gf ports.GitForge, ref, id string) ports.ApplyRequest {
	t.Helper()
	ar, err := gf.GetApplyRequest(context.Background(), ref, id)
	if err != nil {
		t.Fatal(err)
	}
	return ar
}
