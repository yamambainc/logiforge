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
	st := f.b.Snap.(*snapstore.Adapter).Store.(*store.Dir)
	w := worker.New(worker.Config{Dir: t.TempDir(), Interval: time.Hour}, source{f.r.dir}, st)
	srv := httptest.NewServer(w.Handler("k"))
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	// 一時ディレクトリを消す前に、worker を止めて、終わるのを待つ（Cleanup は、登録と逆の順に動く）。
	t.Cleanup(func() {
		srv.Close()
		cancel()
		<-done
	})
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

var (
	agentC    = Caller{Token: "agent", Principal: ports.Principal{Login: "agent", Kind: ports.PrincipalClient}}
	reviewerC = Caller{Token: "rev", Principal: ports.Principal{Login: "reviewer", Kind: ports.PrincipalClient}}
)

func approve(t *testing.T, f *fixture, id string) (Written[ReviewResult], error) {
	t.Helper()
	return f.b.ReviewApplyRequest(context.Background(), reviewerC, "q", id, NewReview{Kind: "approve", Version: 0, Revision: f.forge.ar(id).HeadRevision, Session: "s9", PromptRevision: "p"})
}

func create(t *testing.T, f *fixture, branch, session string, files map[string][]byte) string {
	t.Helper()
	f.branch(branch, session, files)
	out, err := f.b.CreateApplyRequest(context.Background(), agentC, "q", NewApplyRequest{Branch: branch, Session: session})
	if err != nil {
		t.Fatal(err)
	}
	return out.Value.ID
}

func lastFailure(t *testing.T, res Written[ReviewResult], err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	fs := res.Value.ApplyRequest.MergeFailures
	if res.Value.ApplyRequest.State != "open" || len(fs) == 0 {
		t.Fatalf("反映に失敗していない：%+v", res.Value.ApplyRequest)
	}
	return fs[len(fs)-1].Reason
}

func TestMergeFailurePaths(t *testing.T) {
	ctx := context.Background()

	t.Run("git の衝突", func(t *testing.T) {
		f := live(t)
		id := create(t, f, "x", "s3", map[string][]byte{dH1 + "/background.md": []byte("一\n")})
		f.forge.mergeErr = ports.ErrConflict
		res, err := approve(t, f, id)
		if r := lastFailure(t, res, err); r != "conflict" || *res.Value.ApplyRequest.Awaiting != "fix" {
			t.Fatalf("got %s", r)
		}
	})
	t.Run("head が動いた", func(t *testing.T) {
		f := live(t)
		id := create(t, f, "x", "s3", map[string][]byte{dH1 + "/background.md": []byte("一\n")})
		f.forge.mergeErr = ports.ErrHeadMoved
		if _, err := approve(t, f, id); code(err) != CodeVersionConflict {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("worker が main に追いつかない", func(t *testing.T) {
		f := live(t)
		f.b.WaitTimeout = 500 * time.Millisecond
		id := create(t, f, "x", "s3", map[string][]byte{dH1 + "/background.md": []byte("一\n")})
		f.forge.mainHead = strings.Repeat("e", 40)
		res, err := approve(t, f, id)
		if r := lastFailure(t, res, err); r != "snapshot_pending" || *res.Value.ApplyRequest.Awaiting != "review" {
			t.Fatalf("got %s %v", r, res.Value.ApplyRequest.Awaiting)
		}
	})
	t.Run("不変条件（識別子の重なり）", func(t *testing.T) {
		f := live(t)
		story := func(parent string) map[string][]byte {
			return map[string][]byte{
				parent + "/s-aaaaaaaaaa/story.md":                   tree.MarshalStory(tree.StoryContent{ID: "s-aaaaaaaaaa", Title: "t", Summary: "s"}),
				parent + "/s-aaaaaaaaaa/h-bbbbbbbbbb/hypothesis.md": tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-bbbbbbbbbb", Statement: "x"}),
			}
		}
		// 葉の仮説を、もう 1 つ main に足す（H5）。
		const h5 = "h-6666666666"
		f.r.write(dS1+"/"+h5+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: h5, Statement: "子3"}))
		f.r.git("add", "-A")
		f.r.git("commit", "--quiet", "-m", "x")
		f.r.git("commit", "--quiet", "--amend", "-m", "反映\n\nLogiforge-Apply-Request: apr_0")
		// 2 つの提案が、別の仮説の下に、同じ識別子のストーリーと仮説を作る。対象ノードは違う。
		x := create(t, f, "x", "s3", story(dH2))
		y := create(t, f, "y", "s4", story(dS1+"/"+h5))
		if _, err := approve(t, f, x); err != nil {
			t.Fatal(err)
		}
		res, err := approve(t, f, y)
		if r := lastFailure(t, res, err); r != "invariant_violation" {
			t.Fatalf("got %s", r)
		}
	})
	t.Run("引用した部分木が変わった", func(t *testing.T) {
		f := live(t)
		id := create(t, f, "x", "s3", map[string][]byte{dH2 + "/verdicts/v-9999999999.md": tree.MarshalVerdict(tree.VerdictContent{ID: "v-9999999999", Value: true, Report: "r\n",
			Citations: []tree.Citation{{TargetKind: "subtree", NodeID: H1, Revision: f.c2}}})})
		other := create(t, f, "y", "s4", map[string][]byte{dH1 + "/background.md": []byte("変えた\n")})
		if _, err := approve(t, f, other); err != nil {
			t.Fatal(err)
		}
		res, err := approve(t, f, id)
		if r := lastFailure(t, res, err); r != "stale_target" {
			t.Fatalf("got %s", r)
		}
	})
	_ = ctx
}

func TestMergeRecordsAllSessionsAndResend(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	id := create(t, f, "x", "s3", map[string][]byte{dH1 + "/background.md": []byte("一\n")})
	// 2 つ目の版を、別のセッションで push する。
	f.r.git("checkout", "--quiet", "x")
	f.r.write(dH1+"/background.md", []byte("一\n二\n"))
	head := f.r.commit("版 2\n\nLogiforge-Session-Id: s4")
	f.r.git("checkout", "--quiet", "main")
	f.forge.ar(id).HeadRevision = head
	w := worker.New(worker.Config{Dir: t.TempDir()}, source{f.r.dir}, f.b.Snap.(*snapstore.Adapter).Store.(*store.Dir))
	w.SyncAll(ctx)

	res, err := approve(t, f, id)
	if err != nil || res.Value.ApplyRequest.State != "merged" {
		t.Fatalf("approve: %+v %v", res.Value.ApplyRequest, err)
	}
	c, _ := f.b.GetCommitForTest(ctx, "q", *res.Value.ApplyRequest.MergedRevision)
	var sessions []string
	for _, tr := range c {
		if tr.Key == "Logiforge-Session-Id" {
			sessions = append(sessions, tr.Value)
		}
	}
	if strings.Join(sessions, ",") != "s3,s4" {
		t.Fatalf("sessions: %v", sessions)
	}
	// approve の送り直しは、成功として今の状態を返す。
	again, err := approve(t, f, id)
	if err != nil || again.Value.ApplyRequest.State != "merged" {
		t.Fatalf("送り直し：%+v %v", again.Value.ApplyRequest, err)
	}
	if _, err := f.b.ReviewApplyRequest(ctx, reviewerC, "q", id, NewReview{Kind: "approve", Revision: head, Session: "other", PromptRevision: "p"}); code(err) != CodeNotOpen {
		t.Fatalf("別のセッションの approve：%v", err)
	}
}

func TestHumanRequestTarget(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	for _, node := range []string{H0, S1, H1} { // H0 は葉でない、S1 はストーリー、H1 は値の決まった葉（要再検討でない葉は可。H1 は葉）
		_, err := f.b.CreateHumanRequest(ctx, agentC, "q", NewHumanRequest{NodeID: node, Kind: "pvr", Body: "b", Session: "s"})
		if node == H1 {
			if err != nil {
				t.Fatalf("葉の H1：%v", err)
			}
			continue
		}
		if code(err) != CodeValidationFailed {
			t.Fatalf("%s：%v", node, err)
		}
	}
}

// 今の版を、worker がまだスナップショットにしていなければ、レビューは snapshot_pending にする。
func TestReviewBeforeSnapshot(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	id := create(t, f, "x", "s3", map[string][]byte{dH1 + "/background.md": []byte("一\n")})
	f.forge.ar(id).HeadRevision = strings.Repeat("a", 40) // GitForge にはあるが、worker はまだ見ていない版
	_, err := f.b.ReviewApplyRequest(ctx, reviewerC, "q", id, NewReview{Kind: "approve", Revision: strings.Repeat("a", 40), Session: "s9", PromptRevision: "p"})
	if code(err) != CodeSnapshotPending {
		t.Fatalf("got %v", err)
	}
}
