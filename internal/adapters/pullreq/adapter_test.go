package pullreq

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// fake は、メモリの上の GitForge である。calls は、呼んだ API の名前の数である（呼び出しの回数のテストのため）。
type fake struct {
	actor    string
	now      time.Time
	next     int
	pulls    map[int]*Pull
	issues   map[int]*Issue
	reviews  map[int][]PullReview
	labels   map[string]Label
	events   map[int][]Event
	comments map[int][]Comment
	files    map[string][]byte
	canPush  map[string]bool
	calls    map[string]int
	// decisions があれば、ReviewDecisions は、PR 全体の承認の状態を返す GitForge（GitHub）のように、番号ごとに返す。
	// なければ、返さない GitForge（Forgejo）のように、空を返す。
	decisions map[int]string
	// noAttachments は、添付を持てない GitForge にする。
	noAttachments bool
	// closedErr は、ClosedPullsPage が返す誤りである。
	closedErr error
}

func newFake() *fake {
	return &fake{actor: "alice", now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), next: 1, pulls: map[int]*Pull{}, issues: map[int]*Issue{},
		reviews: map[int][]PullReview{}, labels: map[string]Label{}, events: map[int][]Event{},
		comments: map[int][]Comment{}, files: map[string][]byte{}, canPush: map[string]bool{"alice": true, "bob": true}, calls: map[string]int{}}
}

func (f *fake) tick() time.Time {
	f.now = f.now.Add(time.Second)
	return f.now
}
func (f *fake) call(name string) { f.calls[name]++ }
func (f *fake) total() int {
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func (f *fake) CreatePull(_ context.Context, _, head, title, body string) (Pull, error) {
	f.call("CreatePull")
	p := &Pull{Number: f.next, Title: title, Body: body, Head: head, HeadSHA: "h-" + head, State: "open", Base: "main", Author: f.actor, CreatedAt: f.tick(), UpdatedAt: f.now}
	f.pulls[p.Number] = p
	f.next++
	return *p, nil
}
func (f *fake) GetPull(_ context.Context, _ string, n int) (Pull, error) {
	f.call("GetPull")
	p, ok := f.pulls[n]
	if !ok {
		return Pull{}, ports.ErrNotFound
	}
	x := *p
	x.Labels = slices.Clone(p.Labels)
	return x, nil
}
func (f *fake) ListPulls(_ context.Context, _, state string) ([]Pull, error) {
	f.call("ListPulls")
	var out []Pull
	for i := 1; i < f.next; i++ {
		if p, ok := f.pulls[i]; ok && (state == "all" || p.State == state) {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (f *fake) ClosedPullsPage(_ context.Context, _ string, page int) ([]Pull, bool, error) {
	f.call("ClosedPullsPage")
	if f.closedErr != nil {
		return nil, false, f.closedErr
	}
	var all []Pull
	for i := f.next - 1; i >= 1; i-- {
		if p, ok := f.pulls[i]; ok && p.State == "closed" {
			all = append(all, *p)
		}
	}
	slices.SortStableFunc(all, func(a, b Pull) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	const size = 2
	lo := (page - 1) * size
	if lo >= len(all) {
		return nil, false, nil
	}
	hi := min(lo+size, len(all))
	return all[lo:hi], hi < len(all), nil
}
func (f *fake) EditPullBody(_ context.Context, _ string, n int, b string) error {
	f.call("EditPullBody")
	f.pulls[n].Body = b
	return nil
}
func (f *fake) ClosePull(_ context.Context, _ string, n int) error {
	f.call("ClosePull")
	f.pulls[n].State = "closed"
	f.events[n] = append(f.events[n], Event{Kind: "closed", Actor: f.actor, CreatedAt: f.tick()})
	return nil
}
func (f *fake) MergePull(_ context.Context, _ string, n int, head, _, _ string) (string, error) {
	f.call("MergePull")
	p := f.pulls[n]
	if p.HeadSHA != head {
		return "", ports.ErrHeadMoved
	}
	p.State, p.Merged, p.MergeCommit, p.UpdatedAt = "closed", true, "m-"+head, f.tick()
	return p.MergeCommit, nil
}
func (f *fake) ReviewDecisions(_ context.Context, _ string, numbers []int) (map[int]string, error) {
	f.call("ReviewDecisions")
	out := map[int]string{}
	if f.decisions == nil {
		return out, nil
	}
	for _, n := range numbers {
		out[n] = f.decisions[n]
	}
	return out, nil
}

func (f *fake) ListReviews(_ context.Context, _ string, n int) ([]PullReview, error) {
	f.call("ListReviews")
	out := slices.Clone(f.reviews[n])
	for i := range out {
		if out[i].Author == "unknown" {
			continue // 書く権限を読めない人
		}
		ok := f.canPush[out[i].Author]
		out[i].CanPush = &ok
	}
	return out, nil
}
func (f *fake) CreateReview(_ context.Context, _ string, n int, kind ports.ReviewKind, body, commit string) (PullReview, error) {
	f.call("CreateReview")
	if f.pulls[n].Author == f.actor && kind == ports.ReviewApprove {
		return PullReview{}, errors.New("approve your own pull is not allowed")
	}
	r := PullReview{ID: int64(len(f.reviews[n]) + 1), Kind: kind, Body: body, Author: f.actor, CommitID: commit, SubmittedAt: f.tick()}
	f.reviews[n] = append(f.reviews[n], r)
	return r, nil
}
func (f *fake) EnsureLabel(_ context.Context, _ string, l Label) error {
	f.call("EnsureLabel")
	if _, ok := f.labels[l.Name]; !ok {
		f.labels[l.Name] = l
	}
	return nil
}
func (f *fake) labelsOf(n int) *[]string {
	if p, ok := f.pulls[n]; ok {
		return &p.Labels
	}
	return &f.issues[n].Labels
}
func (f *fake) AddLabel(_ context.Context, _ string, n int, name string) error {
	f.call("AddLabel")
	if _, ok := f.labels[name]; !ok {
		return errors.New("ない名前のラベル")
	}
	ls := f.labelsOf(n)
	if !slices.Contains(*ls, name) {
		*ls = append(*ls, name)
	}
	f.events[n] = append(f.events[n], Event{Kind: "labeled", Label: name, Actor: f.actor, CreatedAt: f.tick()})
	return nil
}
func (f *fake) RemoveLabel(_ context.Context, _ string, n int, name string) error {
	f.call("RemoveLabel")
	ls := f.labelsOf(n)
	*ls = slices.DeleteFunc(*ls, func(x string) bool { return x == name })
	f.events[n] = append(f.events[n], Event{Kind: "unlabeled", Label: name, Actor: f.actor, CreatedAt: f.tick()})
	return nil
}
func (f *fake) ListEvents(_ context.Context, _ string, n int) ([]Event, error) {
	f.call("ListEvents")
	return slices.Clone(f.events[n]), nil
}
func (f *fake) CreateComment(_ context.Context, _ string, n int, body string) (Comment, error) {
	f.call("CreateComment")
	c := Comment{ID: int64(100 + len(f.comments[n])), IssueNumber: n, Body: body, Author: f.actor, CreatedAt: f.tick()}
	f.comments[n] = append(f.comments[n], c)
	return c, nil
}
func (f *fake) ListComments(_ context.Context, _ string, n int) ([]Comment, error) {
	f.call("ListComments")
	return slices.Clone(f.comments[n]), nil
}
func (f *fake) CreateIssue(_ context.Context, _, title, body string, labels []string) (Issue, error) {
	f.call("CreateIssue")
	for _, l := range labels {
		if _, ok := f.labels[l]; !ok {
			return Issue{}, errors.New("ない名前のラベル")
		}
	}
	is := &Issue{Number: f.next, Title: title, Body: body, State: "open", Author: f.actor, Labels: slices.Clone(labels), CreatedAt: f.tick()}
	f.issues[is.Number] = is
	f.next++
	return *is, nil
}
func (f *fake) ListIssues(context.Context, string) ([]Issue, error) {
	f.call("ListIssues")
	var out []Issue
	for i := 1; i < f.next; i++ {
		if is, ok := f.issues[i]; ok {
			out = append(out, *is)
		}
	}
	return out, nil
}
func (f *fake) GetIssue(_ context.Context, _ string, n int) (Issue, error) {
	f.call("GetIssue")
	is, ok := f.issues[n]
	if !ok {
		return Issue{}, ports.ErrNotFound
	}
	return *is, nil
}
func (f *fake) CloseIssue(_ context.Context, _ string, n int) error {
	f.call("CloseIssue")
	f.issues[n].State, f.issues[n].ClosedAt = "closed", f.tick()
	f.events[n] = append(f.events[n], Event{Kind: "closed", Actor: f.actor, CreatedAt: f.now})
	return nil
}
func (f *fake) AttachmentsSupported() bool { return !f.noAttachments }
func (f *fake) UploadAttachment(_ context.Context, _ string, commentID int64, name string, content []byte) (string, error) {
	f.call("UploadAttachment")
	for n, cs := range f.comments {
		for i := range cs {
			if cs[i].ID == commentID {
				if cs[i].Attachments == nil {
					cs[i].Attachments = map[string]string{}
				}
				id := "a" + strconv.Itoa(len(f.files))
				cs[i].Attachments[name] = id
				f.files[id] = content
				f.comments[n] = cs
				return id, nil
			}
		}
	}
	return "", ports.ErrNotFound
}
func (f *fake) DownloadAttachment(_ context.Context, _ string, _ int64, id string) ([]byte, error) {
	f.call("DownloadAttachment")
	return f.files[id], nil
}

func adapter(f *fake) *Adapter { return &Adapter{API: f, Now: func() time.Time { return f.now }} }

func TestApplyRequestLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	a := adapter(f)
	ar, err := a.CreateApplyRequest(ctx, "o/r", ports.NewApplyRequest{Branch: "b", Description: "説明", Opener: "alice"})
	if err != nil || ar.ID != "1" || ar.Opener != "alice" || ar.Waiting != ports.WaitingReview || ar.ReviewWaitingAt.IsZero() {
		t.Fatalf("作成：%+v %v", ar, err)
	}
	if l := f.labels[LabelReviewWaiting]; l.Color != "#1d76db" || l.Description == "" {
		t.Fatalf("ラベルの色と説明：%+v", l)
	}
	// 送り直し：同じ主体なら同じものを返す。ほかの主体なら受け付けない。
	if again, err := a.CreateApplyRequest(ctx, "o/r", ports.NewApplyRequest{Branch: "b", Opener: "Alice"}); err != nil || again.ID != "1" || len(f.pulls) != 1 {
		t.Fatalf("送り直し：%+v %v", again, err)
	}
	if _, err := a.CreateApplyRequest(ctx, "o/r", ports.NewApplyRequest{Branch: "b", Opener: "bob"}); !errors.Is(err, ports.ErrBranchHasOpen) {
		t.Fatalf("ほかの主体：%v", err)
	}

	// 修正待ちに付け替えると、ラベルは 1 つだけになる。
	prev := f.actor
	f.actor = "bob"
	if err := a.SetWaiting(ctx, "o/r", "1", ports.WaitingFix); err != nil || !slices.Equal(f.pulls[1].Labels, []string{LabelFixWaiting}) {
		t.Fatalf("修正待ち：%v %v", f.pulls[1].Labels, err)
	}
	// 既に修正待ちでも、反映の失敗では付け直す。失敗を書いた carol が付けた出来事が残る（ずれ直しで失敗の出来事として読む）。
	f.actor = "carol"
	if err := a.RecordMergeFailure(ctx, "o/r", "1", ports.MergeFailure{Revision: "h-b", Reason: "conflict", Detail: "衝突"}, ports.WaitingFix); err != nil {
		t.Fatal(err)
	}
	f.actor = prev
	got, err := a.GetApplyRequest(ctx, "o/r", "1")
	if err != nil || got.Waiting != ports.WaitingFix || len(got.Comments) != 1 || !strings.Contains(got.Comments[0].Body, "conflict") || len(got.FixWaitingLabeled) != 2 || got.FixWaitingLabeled[1].Actor != "carol" || !slices.Equal(f.pulls[1].Labels, []string{LabelFixWaiting}) {
		t.Fatalf("反映の失敗：%+v %v", got, err)
	}
	if cs := f.comments[1]; len(cs) != 1 || !strings.Contains(cs[0].Body, "衝突") {
		t.Fatalf("反映の失敗のコメント：%+v", cs)
	}

	if err := a.Close(ctx, "o/r", "1", "取り\n下げる"); err != nil {
		t.Fatal(err)
	}
	got, _ = a.GetApplyRequest(ctx, "o/r", "1")
	if cs := f.comments[1]; got.State != ports.StateClosed || cs[len(cs)-1].Body != "閉じた理由：取り 下げる" || len(f.pulls[1].Labels) != 0 {
		t.Fatalf("close：%+v %v", got, f.pulls[1].Labels)
	}
	if _, err := a.GetApplyRequest(ctx, "o/r", "apr_1"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("番号でない識別子：%v", err)
	}
}

func TestReviewsCounted(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	a := adapter(f)
	if _, err := a.CreateApplyRequest(ctx, "o/r", ports.NewApplyRequest{Branch: "b", Opener: "alice"}); err != nil {
		t.Fatal(err)
	}
	f.actor = "bob"
	r, err := a.AddReview(ctx, "o/r", "1", ports.Review{Kind: ports.ReviewApprove, Body: "良い", Revision: "h-b", PromptRevision: "p1"})
	if err != nil || r.Body != "良い\n\nプロンプトの版：p1" || r.Reviewer != "bob" || r.ID == "" {
		t.Fatalf("review：%+v %v", r, err)
	}
	f.reviews[1] = append(f.reviews[1],
		PullReview{ID: 11, Kind: ports.ReviewRequestChanges, Body: "x", Author: "mallory", CommitID: "h-b"},
		PullReview{ID: 12, Kind: ports.ReviewRequestChanges, Body: "y", Author: "bob", CommitID: "h-b", Dismissed: true},
		PullReview{ID: 13, Kind: ports.ReviewApprove, Body: "z", Author: "mallory", CommitID: "h-b"},
		PullReview{ID: 14, Kind: ports.ReviewApprove, Body: "w", Author: "bob", CommitID: "h-b"},
		PullReview{ID: 15, Kind: ports.ReviewApprove, Body: "v", Author: "unknown", CommitID: "h-b"},
	)
	// 数える：bob の approve 2 つ。
	// 数えない：dismiss された review、書く権限のない mallory の request changes と approve
	// （public の repository で、書く権限のない人が反映を止めたり、させたりできないように）、書く権限を確かめられない人の approve。
	rs, err := a.ListReviews(ctx, "o/r", "1")
	if err != nil || len(rs.Items) != 2 || rs.Items[0].ID != "1" || rs.Items[1].ID != "14" || len(rs.Warnings) != 3 {
		t.Fatalf("数える review：%+v %v", rs, err)
	}
	if !strings.Contains(strings.Join(rs.Warnings, "\n"), "確かめられない") {
		t.Fatalf("確かめられない review の警告：%v", rs.Warnings)
	}
	if f.calls["ListReviews"] != 1 {
		t.Fatalf("review は、pull request ごとに 1 回だけ読む：%d", f.calls["ListReviews"])
	}
}

func TestMergedApplyRequestStopsAtCommitDate(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	a := adapter(f)
	for _, b := range []string{"a", "b", "c", "d", "e"} {
		if _, err := a.CreateApplyRequest(ctx, "o/r", ports.NewApplyRequest{Branch: b, Opener: "alice"}); err != nil {
			t.Fatal(err)
		}
	}
	for n := 1; n <= 5; n++ {
		f.pulls[n].State, f.pulls[n].Merged, f.pulls[n].MergeCommit, f.pulls[n].UpdatedAt = "closed", true, "m"+strconv.Itoa(n), f.now.Add(time.Duration(n)*time.Hour)
	}
	committed := f.now.Add(2 * time.Hour)
	f.now = f.now.Add(10 * time.Hour)
	ar, err := a.MergedApplyRequest(ctx, "o/r", "m2", committed)
	if err != nil || ar.ID != "2" {
		t.Fatalf("見つける：%+v %v", ar, err)
	}
	// 直接 push したコミット（どの pull request にもない）は、committed より前の更新まで読んだら、そこでやめる。
	f.calls = map[string]int{}
	if _, err := a.MergedApplyRequest(ctx, "o/r", "direct", f.now.Add(-6*time.Hour)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("見つからない：%v", err)
	}
	if f.calls["ClosedPullsPage"] != 2 {
		t.Fatalf("読むのをやめる位置：%d ページ読んだ", f.calls["ClosedPullsPage"])
	}
	// committer date が未来なら、今の時刻を使う。
	f.calls = map[string]int{}
	if _, err := a.MergedApplyRequest(ctx, "o/r", "direct", f.now.Add(1000*time.Hour)); !errors.Is(err, ports.ErrNotFound) || f.calls["ClosedPullsPage"] != 1 {
		t.Fatalf("未来の committer date：%v %d", err, f.calls["ClosedPullsPage"])
	}
}

// 一覧は、pull request の一覧だけから読む。項目の数によらず、呼ぶ回数は同じである（design/backend/list-reads.md の「呼び出しの回数のテスト」）。
func TestListCallCount(t *testing.T) {
	ctx := context.Background()
	count := func(n int) (int, int) {
		f := newFake()
		a := adapter(f)
		for i := 0; i < n; i++ {
			if _, err := a.CreateApplyRequest(ctx, "o/r", ports.NewApplyRequest{Branch: "b" + strconv.Itoa(i), Opener: "alice"}); err != nil {
				t.Fatal(err)
			}
			if _, err := a.CreateHumanRequest(ctx, "o/r", ports.NewHumanRequest{Kind: ports.HumanRequestPVR, TargetNode: "h-1", Body: "b" + strconv.Itoa(i), Requester: "alice"}); err != nil {
				t.Fatal(err)
			}
		}
		f.calls = map[string]int{}
		ars, err := a.ListApplyRequests(ctx, "o/r", "open")
		if err != nil || len(ars) != n {
			t.Fatalf("一覧：%d %v", len(ars), err)
		}
		arCalls := f.total()
		f.calls = map[string]int{}
		hrs, err := a.ListHumanRequests(ctx, "o/r")
		if err != nil || len(hrs) != n {
			t.Fatalf("人への依頼の一覧：%d %v", len(hrs), err)
		}
		return arCalls, f.total()
	}
	ar2, hr2 := count(2)
	ar40, hr40 := count(40)
	// apply request の一覧は、pull request の一覧と、承認の状態（ReviewDecisions）の 2 回である。
	if ar2 != ar40 || hr2 != hr40 || ar2 != 2 || hr2 != 1 {
		t.Fatalf("呼ぶ回数が、項目の数で変わる：apply request %d と %d、人への依頼 %d と %d", ar2, ar40, hr2, hr40)
	}
}

func TestHumanRequestAndAnswer(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	a := adapter(f)
	hr, err := a.CreateHumanRequest(ctx, "o/r:logiforge/x", ports.NewHumanRequest{Kind: ports.HumanRequestDecision, TargetNode: "h-1", Body: "決めて", Requester: "alice"})
	if err != nil || hr.ID != "1" || hr.Kind != ports.HumanRequestDecision || hr.TargetNode != "h-1" || hr.Body != "決めて" || hr.Requester != "alice" || f.issues[1].Title != "h-1 について：決めて" {
		t.Fatalf("起票：%+v %v", hr, err)
	}
	if !slices.Contains(f.issues[1].Labels, "logiforge/logiforge/x") {
		t.Fatalf("ディレクトリのラベル：%v", f.issues[1].Labels)
	}
	// 送り直し：同じ主体が、同じタイトルと本文で、少し前に作った open の issue があれば、それを返す。本文が違えば、別の依頼である。
	if again, err := a.CreateHumanRequest(ctx, "o/r:logiforge/x", ports.NewHumanRequest{Kind: ports.HumanRequestDecision, TargetNode: "h-1", Body: "決めて", Requester: "alice"}); err != nil || again.ID != "1" {
		t.Fatalf("送り直し：%+v %v", again, err)
	}
	if other, err := a.CreateHumanRequest(ctx, "o/r:logiforge/x", ports.NewHumanRequest{Kind: ports.HumanRequestDecision, TargetNode: "h-1", Body: "違う", Requester: "alice"}); err != nil || other.ID == "1" {
		t.Fatalf("違う本文の依頼：%+v %v", other, err)
	}
	// ほかのディレクトリの repository からは見えない。
	if hrs, _ := a.ListHumanRequests(ctx, "o/r:logiforge/y"); len(hrs) != 0 {
		t.Fatalf("ほかのディレクトリ：%+v", hrs)
	}

	f.actor = "bob"
	files := []ports.NewAttachment{{Name: "写真.jpg", Content: []byte("jpg")}}
	if err := a.Answer(ctx, "o/r:logiforge/x", "1", "答え", "bob", files); err != nil {
		t.Fatal(err)
	}
	got, err := a.GetHumanRequest(ctx, "o/r:logiforge/x", "1")
	if err != nil || got.Answer == nil || got.Answer.Body != "答え" || got.Answer.AnsweredBy != "bob" || len(got.Answer.Attachments) != 1 {
		t.Fatalf("回答：%+v %v", got.Answer, err)
	}
	if err := a.Answer(ctx, "o/r:logiforge/x", "1", "答え", "bob", files); err != nil {
		t.Fatalf("同じ回答の送り直し：%v", err)
	}
	if err := a.Answer(ctx, "o/r:logiforge/x", "1", "別", "bob", nil); !errors.Is(err, ports.ErrAlreadyAnswered) {
		t.Fatalf("違う回答：%v", err)
	}
	data, err := a.ReadAttachment(ctx, "o/r:logiforge/x", "1", got.Answer.Attachments[0].SHA256)
	if err != nil || string(data) != "jpg" {
		t.Fatalf("添付：%q %v", data, err)
	}
	list, _ := a.ListHumanRequests(ctx, "o/r:logiforge/x")
	if len(list) != 2 || list[0].Answer == nil || list[0].Answer.CreatedAt.IsZero() || list[1].Answer != nil {
		t.Fatalf("一覧の回答済み：%+v", list)
	}
	// 一覧は、issue のコメントと添付を読まない。依頼の本文も書き換えない。
	if len(list[0].Answer.Attachments) != 0 || list[0].Body != got.Body || strings.Contains(f.issues[1].Body, "sha256") {
		t.Fatalf("一覧の回答：%+v（1 件の読み %+v）本文 %q", list[0], got, f.issues[1].Body)
	}
}

// コメントを書いたあと、閉じる前に止まったときは、送り直しで書き直さずに閉じる。
func TestAnswerResumes(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	a := adapter(f)
	if _, err := a.CreateHumanRequest(ctx, "o/r", ports.NewHumanRequest{Kind: ports.HumanRequestPVR, TargetNode: "h-1", Body: "b", Requester: "alice"}); err != nil {
		t.Fatal(err)
	}
	f.actor = "bob"
	if _, err := f.CreateComment(ctx, "o/r", 1, "答え"); err != nil {
		t.Fatal(err)
	}
	if err := a.Answer(ctx, "o/r", "1", "答え", "bob", nil); err != nil {
		t.Fatal(err)
	}
	if len(f.comments[1]) != 1 || f.issues[1].State != "closed" {
		t.Fatalf("続きから：%+v %s", f.comments[1], f.issues[1].State)
	}
}

func TestAnswerAttachmentsUnsupported(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	f.noAttachments = true
	a := adapter(f)
	if _, err := a.CreateHumanRequest(ctx, "o/r", ports.NewHumanRequest{Kind: ports.HumanRequestPVR, TargetNode: "h-1", Body: "b", Requester: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Answer(ctx, "o/r", "1", "答え", "bob", []ports.NewAttachment{{Name: "x", Content: []byte("x")}}); !errors.Is(err, ports.ErrAttachmentsUnsupported) || len(f.comments[1]) != 0 {
		t.Fatalf("添付を持てない GitForge：%v", err)
	}
}

func TestNodeTitle(t *testing.T) {
	if got := nodeOfTitle(nodeTitle("h-abc12", "要約")); got != "h-abc12" {
		t.Fatalf("タイトルの対象ノード：%q", got)
	}
	if got := nodeOfTitle("ただのタイトル"); got != "" {
		t.Fatalf("対象ノードのないタイトル：%q", got)
	}
}

// 反映した pull request の一覧そのものを読めない（404 を含む）ときは、ErrNotFound（一覧になかった）にしない。
// worker が、反映の apply request のないコミットとして置かず、次の見回りでやり直すためである。
func TestMergedApplyRequestListError(t *testing.T) {
	f := newFake()
	a := adapter(f)
	for _, e := range []error{fmt.Errorf("github: 404 Not Found: %w", ports.ErrNotFound), fmt.Errorf("github: 502: %w", ports.ErrUnavailable)} {
		f.closedErr = e
		if _, err := a.MergedApplyRequest(context.Background(), "o/r", "m1", f.now); err == nil || errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("%v：%v", e, err)
		}
	}
}
