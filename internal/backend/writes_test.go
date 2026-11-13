package backend

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/snapstore"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

// live は、setup の木に、動いている worker（内部 API つき）をつなぐ。書いた直後の読みを確かめるため。
func live(t *testing.T) *fixture {
	f := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := f.b.Snap.(*snapstore.Adapter).Store.(*store.Dir)
	w := worker.New(worker.Config{Dir: t.TempDir(), Interval: time.Hour}, source{f.r.dir}, st)
	srv := httptest.NewServer(w.Handler("k"))
	t.Cleanup(srv.Close)
	go func() { _ = w.Run(ctx) }()
	f.b.Snap = &snapstore.Adapter{Store: st, WorkerURL: srv.URL, Key: "k"}
	f.b.WaitTimeout = 10 * time.Second
	f.forge.remote = f.r
	return f
}

// branch は、main から新しいブランチを作り、ファイルを置いてコミットする。
func (f *fixture) branch(name, session string, files map[string][]byte) string {
	f.r.git("checkout", "--quiet", "-B", name, "main")
	for p, c := range files {
		f.r.write(p, c)
	}
	head := f.r.commit("版\n\nLogiforge-Session-Id: " + session)
	f.r.git("checkout", "--quiet", "main")
	return head
}

func verdictFile(hypDir, id string, value bool) map[string][]byte {
	return map[string][]byte{hypDir + "/verdicts/" + id + ".md": tree.MarshalVerdict(tree.VerdictContent{ID: id, Value: value, Report: "r\n"})}
}

func TestCreateApplyRequest(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	b, c := f.b, f.caller
	head := f.branch("c", "s3", verdictFile(dH2, "v-9999999999", false))

	if _, err := b.CreateApplyRequest(ctx, Caller{Token: "u", Principal: ports.Principal{Kind: ports.PrincipalUser}}, "q", NewApplyRequest{Branch: "c", Session: "s"}); code(err) != CodeForbidden {
		t.Fatalf("user：%v", err)
	}
	c.Principal = ports.Principal{Login: "agent", Kind: ports.PrincipalClient}
	if _, err := b.CreateApplyRequest(ctx, c, "q", NewApplyRequest{Branch: "c"}); code(err) != CodeSessionRequired {
		t.Fatalf("セッションなし：%v", err)
	}
	if _, err := b.CreateApplyRequest(ctx, c, "q", NewApplyRequest{Branch: "c", Session: "s", Revision: strings.Repeat("0", 40)}); code(err) != CodeValidationFailed {
		t.Fatalf("revision 違い：%v", err)
	}
	if _, err := b.CreateApplyRequest(ctx, c, "q", NewApplyRequest{Branch: "nothing", Session: "s"}); code(err) != CodeValidationFailed {
		t.Fatalf("ないブランチ：%v", err)
	}
	out, err := b.CreateApplyRequest(ctx, c, "q", NewApplyRequest{Branch: "c", Session: "s", Revision: head, IdempotencyKey: "k1"})
	if err != nil || out.Value.State != "open" || len(out.Value.Operations) != 1 || out.Value.Operations[0].Kind != "verdict.submit" {
		t.Fatalf("create: %+v %v", out.Value, err)
	}
	again, err := b.CreateApplyRequest(ctx, c, "q", NewApplyRequest{Branch: "c", Session: "s", IdempotencyKey: "k1"})
	if err != nil || again.Value.ID != out.Value.ID {
		t.Fatalf("送り直し：%+v %v", again.Value, err)
	}
	if _, err := b.CreateApplyRequest(ctx, c, "q", NewApplyRequest{Branch: "c", Session: "s", IdempotencyKey: "k2"}); code(err) != CodeValidationFailed {
		t.Fatalf("同じブランチの 2 つ目：%v", err)
	}

	// 4 種類の操作として読めない差分は、受け付けない。
	f.branch("bad", "s3", map[string][]byte{dH1 + "/hypothesis.md": tree.MarshalHypothesis(tree.HypothesisContent{ID: H1, Statement: "書き換え"})})
	if _, err := b.CreateApplyRequest(ctx, c, "q", NewApplyRequest{Branch: "bad", Session: "s"}); code(err) != CodeValidationFailed || !strings.Contains(err.Error(), "change_not_allowed") {
		t.Fatalf("読めない差分：%v", err)
	}
}

func TestReviewAndMerge(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	b := f.b
	agent := Caller{Token: "agent", Principal: ports.Principal{Login: "agent", Kind: ports.PrincipalClient}}
	reviewer := Caller{Token: "rev", Principal: ports.Principal{Login: "reviewer", Kind: ports.PrincipalClient}}
	f.branch("c", "s3", verdictFile(dH2, "v-9999999999", false))
	created, err := b.CreateApplyRequest(ctx, agent, "q", NewApplyRequest{Branch: "c", Session: "s3"})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Value.ID

	if _, err := b.ReviewApplyRequest(ctx, reviewer, "q", id, NewReview{Kind: "approve", Version: 1, Session: "s9"}); code(err) != CodeValidationFailed {
		t.Fatalf("prompt_revision なし：%v", err)
	}
	if _, err := b.ReviewApplyRequest(ctx, reviewer, "q", id, NewReview{Kind: "approve", Version: 1, Session: "s3", PromptRevision: "p"}); code(err) != CodeSameSession {
		t.Fatalf("同じセッション：%v", err)
	}
	if _, err := b.ReviewApplyRequest(ctx, reviewer, "q", id, NewReview{Kind: "approve", Revision: strings.Repeat("1", 40), Session: "s9", PromptRevision: "p"}); code(err) != CodeVersionConflict {
		t.Fatalf("違う版：%v", err)
	}
	rc, err := b.ReviewApplyRequest(ctx, reviewer, "q", id, NewReview{Kind: "request_changes", Version: 1, Session: "s9"})
	if err != nil || rc.Value.ApplyRequest.Awaiting == nil || *rc.Value.ApplyRequest.Awaiting != "fix" {
		t.Fatalf("request_changes: %+v %v", rc.Value.ApplyRequest, err)
	}

	// approve すると、この要求の中で反映し、読む側に出るまで待つ。
	ok, err := b.ReviewApplyRequest(ctx, reviewer, "q", id, NewReview{Kind: "approve", Version: 1, Session: "s9", PromptRevision: "p"})
	if err != nil || ok.Pending != "" || ok.Value.ApplyRequest.State != "merged" || ok.Value.ApplyRequest.MergedRevision == nil {
		t.Fatalf("approve: %+v pending=%q %v", ok.Value.ApplyRequest, ok.Pending, err)
	}
	m, err := b.GetCommitForTest(ctx, "q", *ok.Value.ApplyRequest.MergedRevision)
	if err != nil || trailerValue(m, "Logiforge-Apply-Request") != id || !strings.HasPrefix(trailerValue(m, "Logiforge-Version"), "1 ") || trailerValue(m, "Logiforge-Session-Id") != "s3" {
		t.Fatalf("反映のコミットの trailer: %+v %v", m, err)
	}
	if v, _ := b.GetNode(ctx, agent, "q", H2, ""); v.Verdict == nil || v.Verdict.ID != "v-9999999999" {
		t.Fatalf("反映した検証結果が読めない：%+v", v.Verdict)
	}
	if _, err := b.ReviewApplyRequest(ctx, reviewer, "q", id, NewReview{Kind: "comment", Version: 1, Session: "s9"}); code(err) != CodeNotOpen {
		t.Fatalf("merged へのレビュー：%v", err)
	}

	// reviewer の主体が、apply request を作った主体と同じなら、警告する。
	f.branch("d", "s4", map[string][]byte{dH1 + "/background.md": []byte("背景\n")})
	d, _ := b.CreateApplyRequest(ctx, agent, "q", NewApplyRequest{Branch: "d", Session: "s4"})
	self, err := b.ReviewApplyRequest(ctx, agent, "q", d.Value.ID, NewReview{Kind: "comment", Version: 1, Session: "s5"})
	if err != nil || !strings.Contains(strings.Join(self.Value.ApplyRequest.Warnings, "\n"), "同じ") {
		t.Fatalf("同じ主体：%+v %v", self.Value.ApplyRequest.Warnings, err)
	}
}

func TestMergeFailureStaleTarget(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	b := f.b
	agent := Caller{Token: "agent", Principal: ports.Principal{Login: "agent", Kind: ports.PrincipalClient}}
	reviewer := Caller{Token: "rev", Principal: ports.Principal{Login: "reviewer", Kind: ports.PrincipalClient}}
	// 2 つの提案が、どちらも H1 を対象にする。
	f.branch("x", "s3", map[string][]byte{dH1 + "/background.md": []byte("一\n")})
	f.branch("y", "s4", map[string][]byte{dH1 + "/background.md": []byte("二\n")})
	x, _ := b.CreateApplyRequest(ctx, agent, "q", NewApplyRequest{Branch: "x", Session: "s3"})
	y, _ := b.CreateApplyRequest(ctx, agent, "q", NewApplyRequest{Branch: "y", Session: "s4"})
	if _, err := b.ReviewApplyRequest(ctx, reviewer, "q", x.Value.ID, NewReview{Kind: "approve", Version: 1, Session: "s9", PromptRevision: "p"}); err != nil {
		t.Fatal(err)
	}
	res, err := b.ReviewApplyRequest(ctx, reviewer, "q", y.Value.ID, NewReview{Kind: "approve", Version: 1, Session: "s9", PromptRevision: "p"})
	if err != nil {
		t.Fatal(err)
	}
	ar := res.Value.ApplyRequest
	if ar.State != "open" || len(ar.MergeFailures) != 1 || ar.MergeFailures[0].Reason != "stale_target" || ar.Awaiting == nil || *ar.Awaiting != "fix" {
		t.Fatalf("stale_target: %+v", ar)
	}
}

func TestChainBrokenAndClose(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	b := f.b
	agent := Caller{Token: "agent", Principal: ports.Principal{Login: "agent", Kind: ports.PrincipalClient}}
	f.forge.reviews["apr_2"] = ports.Reviews{ChainBroken: true}
	_, err := b.ReviewApplyRequest(ctx, agent, "q", "apr_2", NewReview{Kind: "comment", Version: 1, Session: "s9"})
	if code(err) != CodeNotOpen || !strings.Contains(err.Error(), "close し、同じブランチから") {
		t.Fatalf("鎖が切れている：%v", err)
	}
	closed, err := b.CloseApplyRequest(ctx, agent, "q", "apr_2", "s9", "作り直す")
	if err != nil || closed.State != "closed" || closed.ClosedReason == nil || *closed.ClosedReason != "作り直す" {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if again, err := b.CloseApplyRequest(ctx, agent, "q", "apr_2", "s9", "もう一度"); err != nil || *again.ClosedReason != "作り直す" {
		t.Fatalf("2 回目の close: %+v %v", again, err)
	}
}

func TestHumanRequestWrites(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	b := f.b
	agent := Caller{Token: "agent", Principal: ports.Principal{Login: "agent", Kind: ports.PrincipalClient}}
	user := Caller{Token: "alice", Principal: ports.Principal{Login: "alice", Kind: ports.PrincipalUser}}
	if _, err := b.CreateHumanRequest(ctx, agent, "q", NewHumanRequest{NodeID: "h-zzzzzzzzzz", Kind: "pvr", Body: "b", Session: "s"}); code(err) != CodeValidationFailed {
		t.Fatalf("ないノード：%v", err)
	}
	hr, err := b.CreateHumanRequest(ctx, agent, "q", NewHumanRequest{NodeID: H1, Kind: "decision", Body: "決めて", Session: "s", IdempotencyKey: "k"})
	if err != nil || hr.State != "open" {
		t.Fatalf("create: %+v %v", hr, err)
	}
	if again, _ := b.CreateHumanRequest(ctx, agent, "q", NewHumanRequest{NodeID: H1, Kind: "decision", Body: "別", Session: "s", IdempotencyKey: "k"}); again.ID != hr.ID || again.Body != "決めて" {
		t.Fatalf("送り直し：%+v", again)
	}
	if _, err := b.AnswerHumanRequest(ctx, agent, "q", hr.ID, "答え", nil); code(err) != CodeForbidden {
		t.Fatalf("client の回答：%v", err)
	}
	files := []AnswerFile{{Kind: "file", Name: "a.txt", ContentBase64: base64.StdEncoding.EncodeToString([]byte("A"))}, {Kind: "link", Name: "論文", URI: "https://example.com/p"}}
	ans, err := b.AnswerHumanRequest(ctx, user, "q", hr.ID, "答え", files)
	if err != nil || ans.State != "answered" || len(ans.Answer.Attachments) != 1 || !strings.Contains(ans.Answer.Body, "https://example.com/p") {
		t.Fatalf("answer: %+v %v", ans, err)
	}
	if _, err := b.AnswerHumanRequest(ctx, user, "q", hr.ID, "答え", files); err != nil {
		t.Fatalf("送り直し：%v", err)
	}
	if _, err := b.AnswerHumanRequest(ctx, user, "q", hr.ID, "違う答え", nil); code(err) != CodeNotOpen {
		t.Fatalf("2 回目の回答：%v", err)
	}
}

func TestCreateRepositoryExists(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	user := Caller{Token: "alice", Principal: ports.Principal{Login: "alice", Kind: ports.PrincipalUser}}
	// q は、main がすでにあり、最初のコミットだけではないので、repository_exists。
	if _, err := f.b.CreateRepository(ctx, user, "q", "根", ""); code(err) != CodeRepositoryExists {
		t.Fatalf("got %v", err)
	}
	if _, err := f.b.CreateRepository(ctx, user, "Q", "根", ""); code(err) != CodeValidationFailed {
		t.Fatalf("名前：%v", err)
	}
	if _, err := f.b.CreateRepository(ctx, f.caller, "r", "根", ""); code(err) != CodeForbidden {
		t.Fatalf("client：%v", err)
	}
}
