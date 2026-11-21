package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
)

// agentCtx は、テストの主体（fakeAgent）のトークンで GitForge を呼ぶ ctx である。
func agentCtx() context.Context { return ports.WithToken(context.Background(), fakeAgent) }

func newTestAdapter(t *testing.T) (*fakeGitHub, *pullreq.Adapter, *Auth) {
	f := newFakeGitHub(t)
	a, auth, err := New(f.config())
	if err != nil {
		t.Fatal(err)
	}
	a.API.(api).c.sleep = func(context.Context, time.Duration) error { return nil }
	return f, a, auth
}

// 用意：空の repository に、根のファイルを contents API で置き、残り（背景情報）を 2 つ目のコミットにまとめる。署名は付けない。
// 2 回目は ErrMainExists。
func TestSetupRepository(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	files := []ports.File{{Path: "tree/h-aaaaaaaaaa/hypothesis.md", Content: []byte("問い")}, {Path: "tree/h-aaaaaaaaaa/background.md", Content: []byte("背景")}}
	if _, err := a.MainHead(ctx, fakeRepo); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("空の repository の main：%v", err)
	}
	if err := a.SetupRepository(ctx, fakeRepo, files, "m\n\nLogiforge-Init: 1"); err != nil {
		t.Fatal(err)
	}
	if f.commits != 2 || f.files["tree/h-aaaaaaaaaa/background.md"] != "背景" || len(f.pulls) != 0 {
		t.Fatalf("commits %d files %v pulls %d", f.commits, f.files, len(f.pulls))
	}
	if !strings.Contains(f.messages[1], "Logiforge-Init: 1") {
		t.Fatalf("2 つ目のメッセージ：%q", f.messages[1])
	}
	if strings.Contains(f.messages[0]+f.messages[1], "Logiforge-Init-Signature") {
		t.Fatalf("署名を付けた：%q", f.messages)
	}
	if err := a.SetupRepository(ctx, fakeRepo, files, "m"); !errors.Is(err, ports.ErrMainExists) {
		t.Fatalf("2 回目：%v", err)
	}
	if h, err := a.MainHead(ctx, fakeRepo); err != nil || h == "" {
		t.Fatalf("main：%q %v", h, err)
	}
}

// 用意が済んでいるか：空の repository では false、用意のあとは true。
func TestPrepared(t *testing.T) {
	ctx := agentCtx()
	_, a, _ := newTestAdapter(t)
	if ok, err := a.Prepared(ctx, fakeRepo); err != nil || ok {
		t.Fatalf("空の repository：%v %v", ok, err)
	}
	if err := a.SetupRepository(ctx, fakeRepo, []ports.File{{Path: "tree/h-aaaaaaaaaa/hypothesis.md", Content: []byte("問い")}}, "m\n\nLogiforge-Init: 1"); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.Prepared(ctx, fakeRepo); err != nil || !ok {
		t.Fatalf("用意のあと：%v %v", ok, err)
	}
}

// 用意が 1 つ目のコミットのあとで止まったら、送り直し（根の識別子は新しい）で、2 つ目のファイルを main の根に置いて続ける。
func TestSetupRepositoryResume(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	files := func(id string) []ports.File {
		return []ports.File{{Path: "tree/" + id + "/hypothesis.md", Content: []byte("id: " + id + "\n問い")}, {Path: "tree/" + id + "/background.md", Content: []byte("背景")}}
	}
	f.failPutAfter = 1
	if err := a.SetupRepository(ctx, fakeRepo, files("h-aaaaaaaaaa"), "m\n\nLogiforge-Init: 1"); err == nil {
		t.Fatal("2 つ目で失敗したのに成功した")
	}
	if err := a.SetupRepository(ctx, fakeRepo, files("h-bbbbbbbbbb"), "m\n\nLogiforge-Init: 1"); !errors.Is(err, ports.ErrMainExists) {
		t.Fatalf("送り直し：%v", err)
	}
	if f.commits != 2 || f.files["tree/h-aaaaaaaaaa/background.md"] != "背景" || len(f.files) != 2 {
		t.Fatalf("続きを置いていない：commits %d files %v", f.commits, f.files)
	}
	// 中身の違う用意（別の主文）では、続けない。
	f2, a2, _ := newTestAdapter(t)
	f2.failPutAfter = 1
	_ = a2.SetupRepository(ctx, fakeRepo, files("h-aaaaaaaaaa"), "m\n\nLogiforge-Init: 1")
	other := files("h-bbbbbbbbbb")
	other[0].Content = []byte("id: h-bbbbbbbbbb\n別の問い")
	if err := a2.SetupRepository(ctx, fakeRepo, other, "m\n\nLogiforge-Init: 1"); !errors.Is(err, ports.ErrMainExists) || f2.commits != 1 {
		t.Fatalf("別の用意を続けた：%v commits %d", err, f2.commits)
	}
}

func TestRegistration(t *testing.T) {
	ctx := agentCtx()
	_, a, _ := newTestAdapter(t)
	if _, err := a.GitForgeRef(ctx, "other"); !errors.Is(err, ports.ErrNotRegistered) {
		t.Fatalf("登録していない名前：%v", err)
	}
	if ref, _ := a.GitForgeRef(ctx, "study"); ref != fakeRepo {
		t.Fatal(ref)
	}
	if u := a.GitURL(fakeRepo); u != "https://github.example/acme/study.git" {
		t.Fatal(u)
	}
	rs, _ := a.ListRepositories(ctx)
	if len(rs) != 1 || rs[0].Name != "study" {
		t.Fatal(rs)
	}
}

// apply request の作成、レビュー、反映、close、人への依頼を、共通の部分（pullreq）を通して確かめる。
func TestApplyRequestFlow(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	ar, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{Branch: "b1", Opener: fakeAgentLogin})
	if err != nil {
		t.Fatal(err)
	}
	if ar.ID != "1" || ar.State != ports.StateOpen || ar.Opener != fakeAgentLogin || ar.Waiting != ports.WaitingReview || ar.ReviewWaitingAt.IsZero() {
		t.Fatalf("%+v", ar)
	}
	if !f.labels["logiforge/review-waiting"] {
		t.Fatal("レビュー待ちのラベルを作っていない")
	}
	// 同じブランチに、ほかの主体はもう 1 つ作れない。同じ主体なら、送り直しとして同じものを返す。
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{Branch: "b1", Opener: "alice"}); !errors.Is(err, ports.ErrBranchHasOpen) {
		t.Fatalf("同じブランチ：%v", err)
	}
	if again, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{Branch: "b1", Opener: fakeAgentLogin}); err != nil || again.ID != "1" {
		t.Fatalf("送り直し：%+v %v", again, err)
	}
	// 作者自身の approve は、GitHub が拒む。
	if _, err := a.AddReview(ctx, fakeRepo, "1", ports.Review{Kind: ports.ReviewApprove, Revision: "h1", PromptRevision: "p"}); err == nil {
		t.Fatal("作者自身の approve が通った")
	}
	for i := range 3 {
		if _, err := a.AddReview(ctx, fakeRepo, "1", ports.Review{Kind: ports.ReviewComment, Body: "c" + strconv.Itoa(i), Revision: "h1", PromptRevision: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	rvs, err := a.ListReviews(ctx, fakeRepo, "1")
	if err != nil || len(rvs.Items) != 3 || rvs.Items[0].Body != "c0\n\nプロンプトの版：p" || rvs.Items[0].Reviewer != fakeAgentLogin {
		t.Fatalf("reviews：%+v %v", rvs, err)
	}
	if err := a.SetWaiting(ctx, fakeRepo, "1", ports.WaitingFix); err != nil || !slices.Equal(f.pulls[1].labels, []string{"logiforge/fix-waiting"}) {
		t.Fatalf("修正待ち：%v %v", f.pulls[1].labels, err)
	}
	if _, err := a.Merge(ctx, fakeRepo, "1", "other", "t", "m"); !errors.Is(err, ports.ErrHeadMoved) {
		t.Fatalf("違う head：%v", err)
	}
	// 最初の 1 回は、mergeable が null（確かめている）。待って呼び直す。
	f.pulls[1].pending = 2
	sha, err := a.Merge(ctx, fakeRepo, "1", "h1", "t", "m")
	if err != nil || sha != "mh1" || len(f.pulls[1].labels) != 0 {
		t.Fatalf("merge：%q %v %v", sha, err, f.pulls[1].labels)
	}
	// 送り直しは、同じ反映のコミットを返す。
	if sha, err := a.Merge(ctx, fakeRepo, "1", "h1", "t", "m"); err != nil || sha != "mh1" {
		t.Fatalf("送り直し：%q %v", sha, err)
	}
	got, err := a.GetApplyRequest(ctx, fakeRepo, "1")
	if err != nil || got.State != ports.StateMerged || got.MergedRevision != "mh1" {
		t.Fatalf("%+v %v", got, err)
	}

	// 衝突と、反映の失敗と、close
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{Branch: "b3", Opener: fakeAgentLogin}); err != nil {
		t.Fatal(err)
	}
	n := len(f.pulls)
	f.pulls[n].conflict, f.pulls[n].pending = true, 0
	id := strconv.Itoa(n)
	if _, err := a.Merge(ctx, fakeRepo, id, "h1", "t", "m"); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("衝突：%v", err)
	}
	if err := a.RecordMergeFailure(ctx, fakeRepo, id, ports.MergeFailure{Revision: "h1", Reason: "conflict", Detail: "衝突した"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetApplyRequest(ctx, fakeRepo, id); len(got.MergeFailures) != 1 || got.MergeFailures[0].Reason != "conflict" {
		t.Fatalf("反映の失敗：%+v", got.MergeFailures)
	}
	if err := a.Close(ctx, fakeRepo, id, "やめる"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetApplyRequest(ctx, fakeRepo, id); got.State != ports.StateClosed {
		t.Fatalf("%+v", got)
	}
	all, err := a.ListApplyRequests(ctx, fakeRepo, "all")
	if err != nil || len(all) != 2 {
		t.Fatalf("一覧：%d %v", len(all), err)
	}
	if open, _ := a.ListApplyRequests(ctx, fakeRepo, "open"); len(open) != 0 {
		t.Fatalf("open の一覧：%+v", open)
	}
}

func TestHumanRequest(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	// pull request と番号が並ぶ。issue の一覧から pull request を除く。
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{Branch: "b1", Opener: fakeAgentLogin}); err != nil {
		t.Fatal(err)
	}
	hr, err := a.CreateHumanRequest(ctx, fakeRepo, ports.NewHumanRequest{Kind: ports.HumanRequestPVR, TargetNode: "h-aaaaaaaaaa", Body: "見て", Requester: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if hr.ID != "2" || hr.TargetNode != "h-aaaaaaaaaa" || f.issues[hr.Number] == nil || !strings.Contains(strings.Join(f.issues[hr.Number].Labels, ","), "logiforge/pvr") {
		t.Fatalf("issue：%+v %+v", hr, f.issues)
	}
	err = a.Answer(ctx, fakeRepo, hr.ID, "答え", "agent", []ports.NewAttachment{{Name: "a.jpg", Content: []byte("A")}})
	if !errors.Is(err, ports.ErrAttachmentsUnsupported) {
		t.Fatalf("ファイルの添付：%v", err)
	}
	if err := a.Answer(ctx, fakeRepo, hr.ID, "答え", "agent", nil); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetHumanRequest(ctx, fakeRepo, hr.ID)
	if err != nil || got.Answer == nil || got.Answer.Body != "答え" || got.Answer.AnsweredBy != "agent" || f.issues[hr.Number].State != "closed" {
		t.Fatalf("%+v %v", got, err)
	}
	list, err := a.ListHumanRequests(ctx, fakeRepo)
	if err != nil || len(list) != 1 || list[0].Answer == nil {
		t.Fatalf("%+v %v", list, err)
	}
}

// 制限に当たったら、Retry-After を守って待ち、やり直す。
func TestRateLimitRetry(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	var waited []time.Duration
	a.API.(api).c.sleep = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }
	f.limited = 2
	if _, err := a.ListRepositories(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MainHead(ctx, fakeRepo); !errors.Is(err, ports.ErrNotFound) {
		t.Fatal(err)
	}
	if len(waited) != 2 || waited[0] != time.Second || waited[1] != 2*time.Second {
		t.Fatalf("待ち方：%v", waited)
	}
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()
	f, _, auth := newTestAdapter(t)
	auth.f.cfg.ClientID, auth.f.cfg.ClientSecret = "cid", "csecret"
	for _, tok := range []string{"ghu_alice", "ghu_bob", "ghu_carol"} {
		f.ownTokens[tok] = true
	}
	// user：Logiforge の App のトークンで、登録した GitHub リポジトリに push できる人。
	f.users["ghu_alice"] = fakeUser{login: "alice", push: []string{"ACME/Study"}}
	// 同じ人でも、ほかのトークン（gh のトークン）なら client。
	f.users["gho_alice"] = fakeUser{login: "alice", push: []string{"acme/study"}}
	// 読めるだけの人、登録していないリポジトリにだけ書ける人は、client。
	f.users["ghu_bob"] = fakeUser{login: "bob", read: []string{"acme/study"}}
	f.users["ghu_carol"] = fakeUser{login: "carol", push: []string{"acme/other"}}
	want := map[string]struct {
		kind  ports.PrincipalKind
		webui bool
	}{"ghu_alice": {ports.PrincipalUser, true}, "gho_alice": {ports.PrincipalClient, false}, "ghu_bob": {ports.PrincipalClient, true}, "ghu_carol": {ports.PrincipalClient, true}}
	for tok, w := range want {
		p, err := auth.Authenticate(ctx, tok)
		if err != nil || p.Kind != w.kind || p.WebUI != w.webui {
			t.Errorf("%s：%+v %v（want %+v）", tok, p, err, w)
		}
	}
	if _, err := auth.Authenticate(ctx, "nope"); !errors.Is(err, ports.ErrUnauthenticated) {
		t.Errorf("nope：%v", err)
	}
	if ok, err := auth.CanWrite(ctx, "ghu_alice", "study"); !ok || err != nil {
		t.Errorf("alice は study に書ける：%v %v", ok, err)
	}
	if ok, _ := auth.CanWrite(ctx, "ghu_bob", "study"); ok {
		t.Error("bob は読めるだけなのに、書けるとされた")
	}
	if ok, _ := auth.CanWrite(ctx, "ghu_alice", "other"); ok {
		t.Error("登録していない repository に書けるとされた")
	}
	// 権限は 60 秒持つ。WithFresh なら、問い合わせ直す。
	n := f.permCalls
	if _, err := auth.CanWrite(ctx, "ghu_alice", "study"); err != nil || f.permCalls != n {
		t.Errorf("キャッシュを使っていない：%d → %d", n, f.permCalls)
	}
	f.users["ghu_alice"] = fakeUser{login: "alice", read: []string{"acme/study"}}
	if ok, _ := auth.CanWrite(ports.WithFresh(ctx), "ghu_alice", "study"); ok || f.permCalls == n {
		t.Errorf("WithFresh で問い合わせ直していない：%v %d", ok, f.permCalls)
	}
	f.readable["ghu_alice"] = true
	if ok, err := auth.CanRead(ctx, "ghu_alice", "study"); !ok || err != nil {
		t.Errorf("読める：%v %v", ok, err)
	}
	if ok, _ := auth.CanRead(ctx, "ghu_bob", "study"); ok {
		t.Error("読めない主体が読めた")
	}
}

// worker は、読むだけの GitHub App があれば、その installation token で git と REST を読む。なければ、トークンなしで読む（public のリポジトリ）。
func TestWorkerSource(t *testing.T) {
	ctx := context.Background()
	f := newFakeGitHub(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.appKey = &key.PublicKey
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	app, err := NewApp(f.config(), "42", pemKey)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewWorkerSource(f.config(), app)
	if err != nil {
		t.Fatal(err)
	}
	gitEnv := func(s *WorkerSource) []string {
		env, err := s.GitEnv(ctx, fakeRepo)
		if err != nil {
			t.Fatal(err)
		}
		return env
	}
	if env := strings.Join(gitEnv(s), "\n"); !strings.Contains(env, "credential.https://github.example.helper") {
		t.Fatalf("helper の URL が違う：%s", env)
	}
	fill := func(s *WorkerSource, url string) string {
		cmd := exec.Command("git", "credential", "fill")
		cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_ASKPASS=", "SSH_ASKPASS=", "HOME="+t.TempDir()), gitEnv(s)...)
		cmd.Stdin = strings.NewReader("url=" + url + "\n\n")
		out, _ := cmd.Output()
		for _, l := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(l, "password="); ok {
				return v
			}
		}
		return ""
	}
	if got := fill(s, "https://github.example/acme/study.git"); got != "ghs_1" {
		t.Errorf("GitHub の URL：%q", got)
	}
	if got := fill(s, "https://elsewhere.example/acme/x.git"); got != "" {
		t.Errorf("ほかの URL にトークンを渡した：%q", got)
	}
	if err := readRepo(ctx, s); err != nil {
		t.Fatalf("App のトークンで repository を読めない：%v", err)
	}
	// 有効な間は使い回す。登録したリポジトリだけと、読む権限だけを求める。
	if f.appMinted != 1 {
		t.Fatalf("installation token を %d 回作った", f.appMinted)
	}
	req := f.appRequests[0]
	if fmt.Sprint(req["repositories"]) != "[study]" || fmt.Sprint(req["permissions"]) != "map[contents:read issues:read metadata:read pull_requests:read]" {
		t.Fatalf("installation token の要求：%v", req)
	}
	// 切れる前（残りが RefreshBefore より短い）なら、作り直す。
	app.tokens[fakeOwner] = appToken{token: "ghs_1", expires: time.Now().Add(RefreshBefore - time.Minute)}
	if tok, err := s.ReadToken(ctx, fakeRepo); err != nil || tok != "ghs_2" {
		t.Fatalf("作り直していない：%q %v", tok, err)
	}
	// 登録していない owner のトークンは作らない。
	if _, err := s.ReadToken(ctx, "other/x"); err == nil {
		t.Fatal("登録していない owner のトークンを作った")
	}
	// 違う鍵の App は断られる。
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	bad, err := NewApp(f.config(), "42", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(other)}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Token(ctx, fakeRepo); err == nil {
		t.Fatal("違う鍵の JWT が通った")
	}
	if _, err := NewApp(f.config(), "42", []byte("not a key")); err == nil {
		t.Fatal("PEM でない鍵を受け入れた")
	}
	// トークンなし：git の資格情報を渡さず、public なら REST も読める。private なら読めない。
	anon, err := NewWorkerSource(f.config(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if env := gitEnv(anon); len(env) != 0 {
		t.Fatalf("トークンなしなのに GitEnv：%v", env)
	}
	if err := readRepo(ctx, anon); err != nil {
		t.Fatalf("public をトークンなしで読めない：%v", err)
	}
	f.private = true
	if err := readRepo(ctx, anon); err == nil {
		t.Fatal("private をトークンなしで読めた")
	}
	if err := readRepo(ctx, s); err != nil {
		t.Fatalf("private を App で読めない：%v", err)
	}
}

func TestParseRepositories(t *testing.T) {
	rs, err := ParseRepositories("a=o/a, b=o/b")
	if err != nil || len(rs) != 2 || rs[1].GitForgeRef != "o/b" {
		t.Fatalf("%v %v", rs, err)
	}
	for _, bad := range []string{"a", "a=o", "a=o/b/c"} {
		if _, err := ParseRepositories(bad); err == nil {
			t.Errorf("%q が通った", bad)
		}
	}
}

// 反映かの判定：反映した pull request の一覧から、merge のコミットで引く。だれが merge したかは見ない。
func TestMergedApplyRequest(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{Branch: "b1", Opener: fakeAgentLogin}); err != nil {
		t.Fatal(err)
	}
	f.pulls[1].pending = 0
	sha, err := a.Merge(ctx, fakeRepo, "1", "h1", "t", "m")
	if err != nil {
		t.Fatal(err)
	}
	// worker は、トークンなし（public）で読む。
	anon := context.Background()
	ar, err := a.MergedApplyRequest(anon, fakeRepo, sha, time.Now())
	if err != nil || ar.ID != "1" || ar.HeadRevision != "h1" {
		t.Fatalf("%+v %v", ar, err)
	}
	if _, err := a.MergedApplyRequest(anon, fakeRepo, "deadbeef", time.Now()); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("merge のコミットでないもの：%v", err)
	}
	// 向き先が main でない pull request の merge は、引かない。
	f.pulls[1].base = "other"
	if _, err := a.MergedApplyRequest(anon, fakeRepo, sha, time.Now()); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("main でない向き先：%v", err)
	}
}

// merge の権限のない主体の merge は ErrMergeForbidden。制限に当たり続けた 403 は、権限がないことにしない。
func TestMergeForbidden(t *testing.T) {
	f, a, _ := newTestAdapter(t)
	ctx := agentCtx()
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{Branch: "b1", Opener: fakeAgentLogin}); err != nil {
		t.Fatal(err)
	}
	f.pulls[1].pending = 0
	f.users["tok-weak"] = fakeUser{login: "weak", noMerge: true}
	f.readable["tok-weak"] = true
	if _, err := a.Merge(ports.WithToken(context.Background(), "tok-weak"), fakeRepo, "1", "h1", "t", "m"); !errors.Is(err, ports.ErrMergeForbidden) {
		t.Fatalf("権限のない主体：%v", err)
	}
	// ルールセットに断られた merge（実際の GitHub で確かめた応答の形）も、merge の権限がないこととする。
	f.ruleBlocked = true
	if _, err := a.API.MergePull(ctx, fakeRepo, 1, "h1", "t", "m"); !errors.Is(err, ports.ErrMergeForbidden) {
		t.Fatalf("ルールセットに断られた merge：%v", err)
	}
	f.ruleBlocked = false
	f.limited = 1000
	_, err := a.API.MergePull(ctx, fakeRepo, 1, "h1", "t", "m")
	if err == nil || errors.Is(err, ports.ErrMergeForbidden) {
		t.Fatalf("制限の 403：%v", err)
	}
}

// readRepo は、worker の読むトークン（なければトークンなし）で、repository を REST で読む。
func readRepo(ctx context.Context, s *WorkerSource) error {
	tok, err := s.ReadToken(ctx, fakeRepo)
	if err != nil {
		return err
	}
	return s.c.do(ctx, tok, http.MethodGet, "/repos/"+fakeRepo, nil, nil)
}

// 読めるかは、主体の読めるリポジトリの一覧（GET /user/repos）で決め、repository ごとに GET /repos を呼ばない
// （design/backend/list-reads.md の「置き場の読み」）。一覧にない public のリポジトリだけ、1 件読んで確かめ、その結果も持つ。
func TestCanReadUsesList(t *testing.T) {
	ctx := context.Background()
	f, _, auth := newTestAdapter(t)
	f.users["gho_dan"] = fakeUser{login: "dan"}
	f.readable["gho_dan"] = true
	for i := 0; i < 5; i++ {
		if ok, err := auth.CanRead(ctx, "gho_dan", "study"); !ok || err != nil {
			t.Fatalf("読める：%v %v", ok, err)
		}
	}
	if f.userRepoCalls != 1 || f.repoGets != 0 {
		t.Fatalf("一覧は 1 回、1 件の読みは 0 回のはず：%d %d", f.userRepoCalls, f.repoGets)
	}
	// 一覧に出ない（関わっていない public のリポジトリ）なら、1 件だけ読み、その結果を持つ。
	f.hideFromUserRepos = true
	f.users["gho_eve"] = fakeUser{login: "eve"}
	f.readable["gho_eve"] = true
	for i := 0; i < 3; i++ {
		if ok, err := auth.CanRead(ctx, "gho_eve", "study"); !ok || err != nil {
			t.Fatalf("読める：%v %v", ok, err)
		}
	}
	if f.repoGets != 1 {
		t.Fatalf("1 件の読みは 1 回のはず：%d", f.repoGets)
	}
}

// repository の一覧の、呼び出しの回数のテスト（design/backend/list-reads.md の「呼び出しの回数のテスト」）。
// 登録した repository が 2 つでも 40 個でも、読めるかを確かめる GitHub への要求は、同じ回数（一覧の 1 回）である。
func TestCanReadCallCount(t *testing.T) {
	for _, n := range []int{2, 40} {
		ctx := context.Background()
		f, _, auth := newTestAdapter(t)
		f.users["gho_dan"] = fakeUser{login: "dan"}
		f.readable["gho_dan"] = true
		f.extraUserRepos = nil
		for i := 0; i < n; i++ {
			git := fmt.Sprintf("acme/r%d", i)
			auth.f.cfg.Repositories = append(auth.f.cfg.Repositories, ports.Repository{Name: fmt.Sprintf("r%d", i), GitForgeRef: git})
			f.extraUserRepos = append(f.extraUserRepos, git)
		}
		before := f.requests()
		for i := 0; i < n; i++ {
			if ok, err := auth.CanRead(ctx, "gho_dan", fmt.Sprintf("r%d", i)); !ok || err != nil {
				t.Fatalf("%d 個：r%d を読める：%v %v", n, i, ok, err)
			}
		}
		if got := f.requests() - before; got != 1 {
			t.Fatalf("%d 個：GitHub への要求は 1 回のはず：%d", n, got)
		}
	}
}
