package pullreq

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// fake は、メモリの上の GitForge である。
type fake struct {
	writer   string
	next     int
	pulls    map[int]*Pull
	issues   map[int]*Issue
	comments []*Comment
	reviews  map[int][]PullReview
	files    map[string][]byte
	// failUpload は、添付を上げるのを失敗させる回数である。
	failUpload int
}

func newFake() *fake {
	return &fake{writer: "bot", next: 1, pulls: map[int]*Pull{}, issues: map[int]*Issue{}, reviews: map[int][]PullReview{}, files: map[string][]byte{}}
}

func (f *fake) WriterLogin() string { return f.writer }
func (f *fake) RepoID(_ context.Context, repo string) (string, error) {
	return "id-" + repo, nil
}
func (f *fake) CreatePull(_ context.Context, _, head, title, body string) (Pull, error) {
	p := &Pull{Number: f.next, Title: title, Body: body, Head: head, HeadSHA: "h-" + head, State: "open", Author: f.writer}
	f.pulls[p.Number] = p
	f.next++
	return *p, nil
}
func (f *fake) GetPull(_ context.Context, _ string, n int) (Pull, error) { return *f.pulls[n], nil }
func (f *fake) ListPulls(context.Context, string) ([]Pull, error) {
	var out []Pull
	for i := 1; i < f.next; i++ {
		if p, ok := f.pulls[i]; ok {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (f *fake) EditPullBody(_ context.Context, _ string, n int, b string) error {
	f.pulls[n].Body = b
	return nil
}
func (f *fake) ClosePull(_ context.Context, _ string, n int) error {
	f.pulls[n].State = "closed"
	return nil
}
func (f *fake) MergePull(_ context.Context, _ string, n int, head, _, _ string) (string, error) {
	p := f.pulls[n]
	if p.HeadSHA != head {
		return "", ports.ErrHeadMoved
	}
	p.Merged, p.State = true, "closed"
	return "merge-sha", nil
}
func (f *fake) addComment(author string, n int, body string) *Comment {
	c := &Comment{ID: int64(len(f.comments) + 1), IssueNumber: n, Body: body, Author: author, CreatedAt: time.Unix(int64(len(f.comments)), 0)}
	f.comments = append(f.comments, c)
	return c
}
func (f *fake) CreateComment(_ context.Context, _ string, n int, body string) (Comment, error) {
	return *f.addComment(f.writer, n, body), nil
}
func (f *fake) ListComments(_ context.Context, _ string, n int) ([]Comment, error) {
	var out []Comment
	for _, c := range f.comments {
		if c.IssueNumber == n {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (f *fake) ListRepoComments(context.Context, string) ([]Comment, error) {
	var out []Comment
	for _, c := range f.comments {
		out = append(out, *c)
	}
	return out, nil
}
func (f *fake) CreateReview(_ context.Context, _ string, n int, sha, body string) error {
	f.reviews[n] = append(f.reviews[n], PullReview{Body: body, CommitSHA: sha, Author: f.writer})
	return nil
}
func (f *fake) ListReviews(_ context.Context, _ string, n int) ([]PullReview, error) {
	return f.reviews[n], nil
}
func (f *fake) CreateIssue(_ context.Context, _, title, body string, _ []string) (Issue, error) {
	is := &Issue{Number: f.next, Title: title, Body: body, State: "open", Author: f.writer}
	f.issues[is.Number] = is
	f.next++
	return *is, nil
}
func (f *fake) ListIssues(context.Context, string) ([]Issue, error) {
	var out []Issue
	for i := 1; i < f.next; i++ {
		if is, ok := f.issues[i]; ok {
			out = append(out, *is)
		}
	}
	return out, nil
}
func (f *fake) GetIssue(_ context.Context, _ string, n int) (Issue, error) { return *f.issues[n], nil }
func (f *fake) CloseIssue(_ context.Context, _ string, n int) error {
	f.issues[n].State = "closed"
	return nil
}
func (f *fake) UploadAttachment(_ context.Context, _ string, id int64, name string, content []byte) (string, error) {
	if f.failUpload > 0 {
		f.failUpload--
		return "", errors.New("upload failed")
	}
	c := f.comments[id-1]
	if c.Attachments == nil {
		c.Attachments = map[string]string{}
	}
	key := fmt.Sprintf("%d/%s", id, name)
	c.Attachments[name] = key
	f.files[key] = content
	return key, nil
}
func (f *fake) DownloadAttachment(_ context.Context, _ string, _ int64, key string) ([]byte, error) {
	return f.files[key], nil
}

const repo = "o/r"

func newAdapter() (*Adapter, *fake) {
	f := newFake()
	return &Adapter{API: f, Keys: ring}, f
}

func create(t *testing.T, a *Adapter, id, branch string) ports.ApplyRequest {
	t.Helper()
	ar, err := a.CreateApplyRequest(context.Background(), repo, ports.NewApplyRequest{ID: id, Branch: branch, FirstRevision: "h-" + branch, Opener: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	return ar
}

func TestApplyRequestStates(t *testing.T) {
	ctx := context.Background()
	a, f := newAdapter()
	create(t, a, "apr_open", "o")
	create(t, a, "apr_merged", "m")
	create(t, a, "apr_closed", "c")
	create(t, a, "apr_gone", "g")
	if _, err := a.Merge(ctx, repo, "apr_merged", "h-m", "t", "m"); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(ctx, repo, "apr_closed", "理由", "s"); err != nil {
		t.Fatal(err)
	}
	f.pulls[3].State = "open" // 画面から reopen されても、close の記録があれば closed
	f.pulls[4].State = "closed"
	want := map[string]ports.State{"apr_open": ports.StateOpen, "apr_merged": ports.StateMerged, "apr_closed": ports.StateClosed, "apr_gone": ports.StateClosed}
	all, err := a.ListApplyRequests(ctx, repo)
	if err != nil || len(all) != 4 {
		t.Fatalf("list: %v %v", all, err)
	}
	for _, ar := range all {
		if ar.State != want[ar.ID] {
			t.Fatalf("%s: %s", ar.ID, ar.State)
		}
	}
}

// 正しい記録を、ほかの pull request や issue に写しても数えない。
func TestCopiedRecordsAreNotCounted(t *testing.T) {
	ctx := context.Background()
	a, f := newAdapter()
	create(t, a, "apr_a", "a")
	create(t, a, "apr_b", "b")
	if err := a.Close(ctx, repo, "apr_a", "理由", "s"); err != nil {
		t.Fatal(err)
	}
	// 書き込み主体の close の記録を、apr_b の pull request に写す（書き込み主体として書いた、という最悪の場合）。
	closeRec := f.comments[len(f.comments)-1].Body
	f.addComment(f.writer, 2, closeRec)
	b, err := a.GetApplyRequest(ctx, repo, "apr_b")
	if err != nil || b.State != ports.StateOpen || len(b.Warnings) != 1 {
		t.Fatalf("写した close の記録を数えた：%+v %v", b, err)
	}

	// 識別子の違う close の記録は、MAC が合っても数えない。
	text, _ := ring.Format(Record{Trailers: []Trailer{{KeyRecord, RecordClose}, {KeyApplyRequest, "apr_a"}}}, Scope{RepoID: "id-" + repo, Number: 2})
	f.addComment(f.writer, 2, text)
	if b, _ := a.GetApplyRequest(ctx, repo, "apr_b"); b.State != ports.StateOpen {
		t.Fatalf("識別子の違う close の記録を数えた：%+v", b)
	}

	// 回答を、ほかの issue に写しても数えない。
	for _, id := range []string{"hrq_1", "hrq_2"} {
		if _, err := a.CreateHumanRequest(ctx, repo, ports.NewHumanRequest{ID: id, Kind: ports.HumanRequestPVR, Body: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Answer(ctx, repo, "hrq_1", "答え", "alice", nil); err != nil {
		t.Fatal(err)
	}
	ans := f.comments[len(f.comments)-1].Body
	f.addComment(f.writer, 4, ans)
	if hr, _ := a.GetHumanRequest(ctx, repo, "hrq_2"); hr.Answer != nil || len(hr.Warnings) != 1 {
		t.Fatalf("写した回答を数えた：%+v", hr)
	}

	// 書き込み主体でない人の、記録の形のコメントは、数えず、警告にもしない。
	f.addComment("mallory", 2, closeRec)
	if b, _ := a.GetApplyRequest(ctx, repo, "apr_b"); b.State != ports.StateOpen {
		t.Fatalf("書き込み主体でない人の記録を数えた：%+v", b)
	}
}

func TestEditedRecordIsWarned(t *testing.T) {
	ctx := context.Background()
	a, f := newAdapter()
	create(t, a, "apr_a", "a")
	if err := a.RecordMergeFailure(ctx, repo, "apr_a", ports.MergeFailure{Revision: "h-a", Reason: "conflict", Detail: "d"}); err != nil {
		t.Fatal(err)
	}
	c := f.comments[len(f.comments)-1]
	c.Body = c.Body[:len(c.Body)-1] + "0" // MAC を壊す
	ar, _ := a.GetApplyRequest(ctx, repo, "apr_a")
	if len(ar.MergeFailures) != 0 || len(ar.Warnings) != 1 {
		t.Fatalf("got %+v", ar)
	}
}

func TestCreateApplyRequestResumes(t *testing.T) {
	ctx := context.Background()
	a, f := newAdapter()
	// pull request を作ったあと、記録を書く前に止まった。
	if _, err := f.CreatePull(ctx, repo, "a", "t", ""); err != nil {
		t.Fatal(err)
	}
	ar := create(t, a, "apr_a", "a")
	if ar.Number != 1 || len(f.pulls) != 1 {
		t.Fatalf("新しく作った：%+v", ar)
	}
	if again := create(t, a, "apr_a", "a"); again.Number != 1 {
		t.Fatal("送り直しで、別の提案になった")
	}
	if _, err := a.CreateApplyRequest(ctx, repo, ports.NewApplyRequest{ID: "apr_x", Branch: "a"}); !errors.Is(err, ports.ErrBranchHasOpen) {
		t.Fatalf("got %v", err)
	}

	// 記録のない pull request を、2 つの要求が同時に引き取ったら、最初の記録が勝つ。
	if _, err := f.CreatePull(ctx, repo, "b", "t", ""); err != nil {
		t.Fatal(err)
	}
	text, _ := ring.Format(Record{Trailers: []Trailer{{KeyRecord, RecordApplyRequest}, {KeyApplyRequest, "apr_first"}}}, Scope{RepoID: "id-" + repo, Number: 2})
	racing := &racingAPI{fake: f, before: func() { f.addComment(f.writer, 2, text) }}
	b := &Adapter{API: racing, Keys: ring}
	if _, err := b.CreateApplyRequest(ctx, repo, ports.NewApplyRequest{ID: "apr_second", Branch: "b"}); !errors.Is(err, ports.ErrBranchHasOpen) {
		t.Fatalf("got %v", err)
	}
}

// racingAPI は、最初のコメントを書く直前に、別の要求の記録を割り込ませる。
type racingAPI struct {
	*fake
	before func()
}

func (r *racingAPI) CreateComment(ctx context.Context, repo string, n int, body string) (Comment, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	return r.fake.CreateComment(ctx, repo, n, body)
}

func TestReviewChain(t *testing.T) {
	ctx := context.Background()
	a, f := newAdapter()
	create(t, a, "apr_a", "a")
	add := func(id string, kind ports.ReviewKind) {
		t.Helper()
		if err := a.AddReview(ctx, repo, "apr_a", ports.Review{ID: id, Kind: kind, Revision: "h-a", Reviewer: "bob", Session: "s2"}); err != nil {
			t.Fatal(err)
		}
	}
	add("rev_1", ports.ReviewApprove)
	add("rev_2", ports.ReviewRequestChanges)
	got, err := a.ListReviews(ctx, repo, "apr_a")
	if err != nil || got.ChainBroken || len(got.Items) != 2 || got.Items[1].Kind != ports.ReviewRequestChanges {
		t.Fatalf("reviews: %+v %v", got, err)
	}

	// 後の request_changes を壊すと、前の approve に戻るのではなく、鎖が切れる。
	f.reviews[1][1].Body += "x"
	got, _ = a.ListReviews(ctx, repo, "apr_a")
	if !got.ChainBroken || len(got.Items) != 1 {
		t.Fatalf("壊したのに鎖が切れていない：%+v", got)
	}
	if err := a.AddReview(ctx, repo, "apr_a", ports.Review{ID: "rev_3", Revision: "h-a"}); err == nil {
		t.Fatal("鎖が切れているのに、レビューを足した")
	}

	// 間のレビューが消えても、鎖が切れる。
	f.reviews[1] = append(f.reviews[1][:1:1], f.reviews[1][2:]...)
	add2 := func(prev, id string) {
		text, _ := ring.Format(Record{Trailers: []Trailer{{KeyRecord, RecordReview}, {KeyApplyRequest, "apr_a"}, {KeyReviewID, id}, {KeyPreviousReview, prev}, {KeyReviewRevision, "h-a"}}}, Scope{RepoID: "id-" + repo, Number: 1})
		f.reviews[1] = append(f.reviews[1], PullReview{Body: text, CommitSHA: "h-a", Author: f.writer})
	}
	add2("rev_2", "rev_3")
	if got, _ := a.ListReviews(ctx, repo, "apr_a"); !got.ChainBroken {
		t.Fatalf("間が消えたのに鎖が切れていない：%+v", got)
	}

	// 最後のレビューを、記録でない本文に書き換えると、鎖が切れる。
	c, h := newAdapter()
	create(t, c, "apr_a", "a")
	for _, id := range []string{"rev_1", "rev_2"} {
		if err := c.AddReview(ctx, repo, "apr_a", ports.Review{ID: id, Revision: "h-a"}); err != nil {
			t.Fatal(err)
		}
	}
	h.reviews[1][1].Body = "ただの本文"
	if got, _ := c.ListReviews(ctx, repo, "apr_a"); !got.ChainBroken {
		t.Fatalf("最後を本文に書き換えたのに、鎖が切れていない：%+v", got)
	}
	// 種類が review でない、正しい記録に書き換えても、鎖が切れる。
	h.reviews[1][1].Body, _ = ring.Format(Record{Trailers: []Trailer{{KeyRecord, RecordClose}, {KeyApplyRequest, "apr_a"}}}, Scope{RepoID: "id-" + repo, Number: 1})
	if got, _ := c.ListReviews(ctx, repo, "apr_a"); !got.ChainBroken {
		t.Fatalf("種類を変えたのに、鎖が切れていない：%+v", got)
	}

	// 同時に 2 つのレビューが、同じ直前のレビューを指しても、鎖は切れない。
	d, k := newAdapter()
	create(t, d, "apr_a", "a")
	if err := d.AddReview(ctx, repo, "apr_a", ports.Review{ID: "rev_1", Revision: "h-a"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"rev_2", "rev_3"} {
		text, _ := ring.Format(Record{Trailers: []Trailer{{KeyRecord, RecordReview}, {KeyApplyRequest, "apr_a"}, {KeyReviewID, id}, {KeyPreviousReview, "rev_1"}, {KeyReviewRevision, "h-a"}}}, Scope{RepoID: "id-" + repo, Number: 1})
		k.reviews[1] = append(k.reviews[1], PullReview{Body: text, CommitSHA: "h-a", Author: k.writer})
	}
	if got, _ := d.ListReviews(ctx, repo, "apr_a"); got.ChainBroken || len(got.Items) != 3 {
		t.Fatalf("同時の 2 つで鎖が切れた：%+v", got)
	}
	if err := d.AddReview(ctx, repo, "apr_a", ports.Review{ID: "rev_4", Revision: "h-a"}); err != nil {
		t.Fatal(err)
	}

	// commit_id と記録の版が違うもの、識別子が重なったものも、鎖を切る。
	b, g := newAdapter()
	create(t, b, "apr_a", "a")
	if err := b.AddReview(ctx, repo, "apr_a", ports.Review{ID: "rev_1", Revision: "h-a"}); err != nil {
		t.Fatal(err)
	}
	g.reviews[1] = append(g.reviews[1], g.reviews[1][0])
	if got, _ := b.ListReviews(ctx, repo, "apr_a"); !got.ChainBroken || len(got.Items) != 1 {
		t.Fatalf("重なり：%+v", got)
	}
	g.reviews[1] = g.reviews[1][:1]
	g.reviews[1][0].CommitSHA = "other"
	if got, _ := b.ListReviews(ctx, repo, "apr_a"); !got.ChainBroken || len(got.Items) != 0 {
		t.Fatalf("commit_id：%+v", got)
	}
}

func TestAnswerResumes(t *testing.T) {
	ctx := context.Background()
	a, f := newAdapter()
	if _, err := a.CreateHumanRequest(ctx, repo, ports.NewHumanRequest{ID: "hrq_1", Kind: ports.HumanRequestPVR, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	files := []ports.NewAttachment{{Name: "a.jpg", Content: []byte("A")}, {Name: "b.jpg", Content: []byte("B")}}
	f.failUpload = 1
	if err := a.Answer(ctx, repo, "hrq_1", "答え\r\n", "alice", files); err == nil {
		t.Fatal("上げるのが失敗したのに成功した")
	}
	if f.issues[1].State != "open" {
		t.Fatal("途中で閉じた")
	}
	if err := a.Answer(ctx, repo, "hrq_1", "答え", "alice", files); err != nil {
		t.Fatalf("続きから：%v", err)
	}
	if f.issues[1].State != "closed" || len(f.files) != 2 {
		t.Fatalf("state %s files %d", f.issues[1].State, len(f.files))
	}
	if err := a.Answer(ctx, repo, "hrq_1", "違う答え", "alice", files); !errors.Is(err, ports.ErrAlreadyAnswered) {
		t.Fatalf("got %v", err)
	}
	if err := a.Answer(ctx, repo, "hrq_1", "答え", "alice", files[:1]); !errors.Is(err, ports.ErrAlreadyAnswered) {
		t.Fatalf("添付が違う：%v", err)
	}
	hr, _ := a.GetHumanRequest(ctx, repo, "hrq_1")
	data, err := a.ReadAttachment(ctx, repo, "hrq_1", hr.Answer.Attachments[1].SHA256)
	if err != nil || string(data) != "B" {
		t.Fatalf("read: %q %v", data, err)
	}
	// 中身を差し替えられた添付は、返さない。
	for k := range f.files {
		f.files[k] = []byte("X")
	}
	if _, err := a.ReadAttachment(ctx, repo, "hrq_1", hr.Answer.Attachments[1].SHA256); !errors.Is(err, ports.ErrAttachmentTamper) {
		t.Fatalf("got %v", err)
	}
}
