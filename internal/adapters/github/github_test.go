package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
)

var testKeys, _ = pullreq.KeyringFromSecrets([]string{"k1"}, []string{"secret"})

// agentCtx は、テストの主体（fakeAgent）のトークンで GitForge を呼ぶ ctx である。
func agentCtx() context.Context { return ports.WithToken(context.Background(), fakeAgent) }

func newTestAdapter(t *testing.T) (*fakeGitHub, *pullreq.Adapter, *Auth) {
	f := newFakeGitHub(t)
	a, auth, err := New(f.config(), testKeys)
	if err != nil {
		t.Fatal(err)
	}
	a.API.(api).c.sleep = func(context.Context, time.Duration) error { return nil }
	return f, a, auth
}

// 用意：空の repository に、根のファイルを contents API で置き、残り（背景情報）を 2 つ目のコミットにまとめ、署名を付ける。
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
	// 署名は、repository の識別子、根のコミット、加えたファイルの sha256 に対して合う。
	sig := lastLine(f.messages[1])
	value, ok := strings.CutPrefix(sig, snapshot.TrailerInitSignature+": ")
	sum := sha256.Sum256([]byte("背景"))
	added := []pullreq.InitFile{{Path: "tree/h-aaaaaaaaaa/background.md", SHA256: hex.EncodeToString(sum[:])}}
	if !ok || testKeys.VerifyOnly().VerifyInit(pullreq.InitScope{Kind: ports.InitSecond, RepoID: "github:R_x", Parent: mainSHA(1)}, added, value) != nil {
		t.Fatalf("署名が合わない：%q", sig)
	}
	if testKeys.VerifyOnly().VerifyInit(pullreq.InitScope{Kind: ports.InitSecond, RepoID: "github:R_x", Parent: mainSHA(1)}, []pullreq.InitFile{{Path: "tree/h-aaaaaaaaaa/background.md", SHA256: "00"}}, value) == nil {
		t.Fatal("違う中身でも署名が合った")
	}
	if err := a.SetupRepository(ctx, fakeRepo, files, "m"); !errors.Is(err, ports.ErrMainExists) {
		t.Fatalf("2 回目：%v", err)
	}
	if h, err := a.MainHead(ctx, fakeRepo); err != nil || h == "" {
		t.Fatalf("main：%q %v", h, err)
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

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	return s[strings.LastIndex(s, "\n")+1:]
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

// 提案の作成、レビュー、反映、close、人への依頼を、共通の部分（pullreq）を通して確かめる。
func TestApplyRequestFlow(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	ar, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{ID: "ar_1", Branch: "b1", FirstRevision: "h1", Opener: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if ar.ID != "ar_1" || ar.State != ports.StateOpen || ar.Opener != "alice" {
		t.Fatalf("%+v", ar)
	}
	// 同じブランチにもう 1 つは作れない。
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{ID: "ar_2", Branch: "b1", FirstRevision: "h1", Opener: "alice"}); !errors.Is(err, ports.ErrBranchHasOpen) {
		t.Fatalf("同じブランチ：%v", err)
	}
	for i := range 3 {
		if err := a.AddReview(ctx, fakeRepo, "ar_1", ports.Review{ID: "rv_" + string(rune('a'+i)), Kind: ports.ReviewComment, Revision: "h1", Reviewer: "bob", Session: "s", PromptRevision: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	rvs, err := a.ListReviews(ctx, fakeRepo, "ar_1")
	if err != nil || len(rvs.Items) != 3 || rvs.ChainBroken {
		t.Fatalf("reviews：%+v %v", rvs, err)
	}
	if _, err := a.Merge(ctx, fakeRepo, "ar_1", "other", "t", "m"); !errors.Is(err, ports.ErrHeadMoved) {
		t.Fatalf("違う head：%v", err)
	}
	// 最初の 1 回は、mergeable が null（確かめている）。待って呼び直す。
	f.pulls[1].pending = 2
	sha, err := a.Merge(ctx, fakeRepo, "ar_1", "h1", "t", "m")
	if err != nil || sha != "mh1" {
		t.Fatalf("merge：%q %v", sha, err)
	}
	// 送り直しは、同じ反映のコミットを返す。
	if sha, err := a.Merge(ctx, fakeRepo, "ar_1", "h1", "t", "m"); err != nil || sha != "mh1" {
		t.Fatalf("送り直し：%q %v", sha, err)
	}
	got, err := a.GetApplyRequest(ctx, fakeRepo, "ar_1")
	if err != nil || got.State != ports.StateMerged || got.MergedRevision != "mh1" {
		t.Fatalf("%+v %v", got, err)
	}

	// 衝突
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{ID: "ar_3", Branch: "b3", FirstRevision: "h1", Opener: "alice"}); err != nil {
		t.Fatal(err)
	}
	n := len(f.pulls)
	f.pulls[n].conflict, f.pulls[n].pending = true, 0
	if _, err := a.Merge(ctx, fakeRepo, "ar_3", "h1", "t", "m"); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("衝突：%v", err)
	}
	if err := a.Close(ctx, fakeRepo, "ar_3", "やめる", "s"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.GetApplyRequest(ctx, fakeRepo, "ar_3"); got.State != ports.StateClosed || got.CloseReason != "やめる" {
		t.Fatalf("%+v", got)
	}
	all, err := a.ListApplyRequests(ctx, fakeRepo)
	if err != nil || len(all) != 2 {
		t.Fatalf("一覧：%d %v", len(all), err)
	}
}

func TestHumanRequest(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	// pull request と番号が並ぶ。issue の一覧から pull request を除く。
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{ID: "ar_1", Branch: "b1", FirstRevision: "h1", Opener: "alice"}); err != nil {
		t.Fatal(err)
	}
	hr, err := a.CreateHumanRequest(ctx, fakeRepo, ports.NewHumanRequest{ID: "hrq_1", Kind: ports.HumanRequestPVR, TargetNode: "h-aaaaaaaaaa", Body: "見て", Requester: "agent", Session: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if f.issues[hr.Number] == nil || !strings.Contains(strings.Join(f.issues[hr.Number].Labels, ","), "logiforge/pvr") {
		t.Fatalf("issue：%+v", f.issues)
	}
	err = a.Answer(ctx, fakeRepo, "hrq_1", "答え", "alice", []ports.NewAttachment{{Name: "a.jpg", Content: []byte("A")}})
	if !errors.Is(err, ports.ErrAttachmentsUnsupported) {
		t.Fatalf("ファイルの添付：%v", err)
	}
	if err := a.Answer(ctx, fakeRepo, "hrq_1", "答え", "alice", nil); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetHumanRequest(ctx, fakeRepo, "hrq_1")
	if err != nil || got.Answer == nil || got.Answer.Body != "答え" || f.issues[hr.Number].State != "closed" {
		t.Fatalf("%+v %v", got, err)
	}
	list, err := a.ListHumanRequests(ctx, fakeRepo)
	if err != nil || len(list) != 1 {
		t.Fatalf("%d %v", len(list), err)
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
	auth.f.cfg.UserLogins = []string{"Carol"}
	auth.f.cfg.UserTeams = []string{"acme/Logiforge-Users"}
	auth.f.cfg.ClientID, auth.f.cfg.ClientSecret = "cid", "csecret"
	for _, tok := range []string{"gho_alice", "gho_bob", "gho_carol"} {
		f.ownTokens[tok] = true
	}
	// ほかのトークン（登録した人のもの）は、client。
	f.users["gho_dave_other_app"] = fakeUser{login: "carol"}
	f.users["gho_alice"] = fakeUser{login: "alice", teams: []string{"acme/logiforge-users"}}
	f.users["ghp_alice"] = fakeUser{login: "alice", teams: []string{"acme/logiforge-users"}}
	f.users["gho_bob"] = fakeUser{login: "bob", teams: []string{"acme/other", "other/logiforge-users"}}
	f.users["gho_carol"] = fakeUser{login: "carol"}
	for tok, want := range map[string]ports.PrincipalKind{"gho_alice": ports.PrincipalUser, "ghp_alice": ports.PrincipalClient, "gho_bob": ports.PrincipalClient, "gho_carol": ports.PrincipalUser, "gho_dave_other_app": ports.PrincipalClient} {
		p, err := auth.Authenticate(ctx, tok)
		if err != nil || p.Kind != want {
			t.Errorf("%s：%+v %v（want %s）", tok, p, err, want)
		}
	}
	for _, tok := range []string{"nope"} {
		if _, err := auth.Authenticate(ctx, tok); !errors.Is(err, ports.ErrUnauthenticated) {
			t.Errorf("%s：%v", tok, err)
		}
	}
	f.readable["gho_alice"] = true
	if ok, err := auth.CanRead(ctx, "gho_alice", "study"); !ok || err != nil {
		t.Errorf("読める：%v %v", ok, err)
	}
	if ok, _ := auth.CanRead(ctx, "gho_bob", "study"); ok {
		t.Error("読めない主体が読めた")
	}
	if ok, _ := auth.CanRead(ctx, "gho_alice", "other"); ok {
		t.Error("登録していない repository が読めた")
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
	if id, err := s.RepoID(ctx, fakeRepo); err != nil || id != "github:R_x" {
		t.Fatalf("RepoID：%q %v", id, err)
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
	if _, err := anon.CommentsSince(ctx, fakeRepo, time.Time{}); err != nil {
		t.Fatalf("public をトークンなしで読めない：%v", err)
	}
	f.private = true
	if _, err := anon.CommentsSince(ctx, fakeRepo, time.Time{}); err == nil {
		t.Fatal("private をトークンなしで読めた")
	}
	if _, err := s.CommentsSince(ctx, fakeRepo, time.Time{}); err != nil {
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

// 反映かの判定：merge のコミットから、merge した pull request の提案を引く。だれが merge したかは見ない。
func TestMergedApplyRequest(t *testing.T) {
	ctx := agentCtx()
	f, a, _ := newTestAdapter(t)
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{ID: "ar_1", Branch: "b1", FirstRevision: "h1", Opener: "alice"}); err != nil {
		t.Fatal(err)
	}
	f.pulls[1].pending = 0
	sha, err := a.Merge(ctx, fakeRepo, "ar_1", "h1", "t", "m")
	if err != nil {
		t.Fatal(err)
	}
	// worker は、トークンなし（public）で読む。
	anon := context.Background()
	ar, err := a.MergedApplyRequest(anon, fakeRepo, sha)
	if err != nil || ar.ID != "ar_1" || ar.HeadRevision != "h1" {
		t.Fatalf("%+v %v", ar, err)
	}
	if _, err := a.MergedApplyRequest(anon, fakeRepo, "deadbeef"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("merge のコミットでないもの：%v", err)
	}
	// 向き先が main でない pull request の merge は、引かない。
	f.pulls[1].base = "other"
	if _, err := a.MergedApplyRequest(anon, fakeRepo, sha); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("main でない向き先：%v", err)
	}
}

// merge の権限のない主体の merge は ErrMergeForbidden。制限に当たり続けた 403 は、権限がないことにしない。
func TestMergeForbidden(t *testing.T) {
	f, a, _ := newTestAdapter(t)
	ctx := agentCtx()
	if _, err := a.CreateApplyRequest(ctx, fakeRepo, ports.NewApplyRequest{ID: "ar_1", Branch: "b1", FirstRevision: "h1", Opener: "alice"}); err != nil {
		t.Fatal(err)
	}
	f.pulls[1].pending = 0
	f.users["tok-weak"] = fakeUser{login: "weak", noMerge: true}
	f.readable["tok-weak"] = true
	if _, err := a.Merge(ports.WithToken(context.Background(), "tok-weak"), fakeRepo, "ar_1", "h1", "t", "m"); !errors.Is(err, ports.ErrMergeForbidden) {
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
