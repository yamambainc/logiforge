package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/snapstore"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
	"github.com/yamambainc/logiforge/internal/workcopy"
	"github.com/yamambainc/logiforge/internal/worker"
)

// ---- テスト用のリモートの git リポジトリ ----

type remote struct {
	t   *testing.T
	dir string
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
	full := filepath.Join(r.dir, p)
	_ = os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, content, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *remote) commit(msg string) string {
	r.git("add", "-A")
	r.git("commit", "--quiet", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

const (
	H0 = "h-0000000000"
	S1 = "s-1111111111"
	H1 = "h-3333333333"
	H2 = "h-4444444444"
	S2 = "s-2222222222"
	H3 = "h-5555555555"
	V1 = "v-7777777777"

	dH0 = "tree/" + H0
	dS1 = dH0 + "/" + S1
	dH1 = dS1 + "/" + H1
	dH2 = dS1 + "/" + H2
)

// ---- 偽の GitForge と認証 ----

type fakeForge struct {
	ars     []ports.ApplyRequest
	reviews map[string]ports.Reviews
	hrs     []ports.HumanRequest
	files   map[string][]byte
	// remote は、squash merge を行う git リポジトリである。
	remote *remote
	next   int
	// mergeErr があれば、Merge はそれを返す（衝突や、head が動いた場合を作るため）。
	mergeErr error
	// mainHead があれば、MainHead はそれを返す（worker が追いつかない場合を作るため）。
	mainHead string
	// onMainHead があれば、MainHead の前に呼ぶ（確かめているあいだに、ほかの人が review を出した場合を作るため）。
	onMainHead func()
	// archived は、archive の git リポジトリ（gitforge_ref）。
	archived map[string]bool
	// waitingErr があれば、SetWaiting はそれを返す（ラベルを書けない主体の場合を作るため）。
	waitingErr error
	// keys は、人への依頼の Idempotency-Key（主体と改行とキー）から、作った依頼を引く。
	keys map[string]ports.HumanRequest
}

func (f *fakeForge) ListRepositories(context.Context) ([]ports.Repository, error) { return nil, nil }
func (f *fakeForge) GitForgeRef(_ context.Context, name string) (string, error) {
	// unreg- で始まる名前は、登録していない repository とする（GitHub を使う構成）。
	if strings.HasPrefix(name, "unreg-") {
		return "", ports.ErrNotRegistered
	}
	return "o/" + name, nil
}
func (f *fakeForge) GitURL(ref string) string { return "https://forge.example.com/" + ref + ".git" }
func (f *fakeForge) MainHead(context.Context, string) (string, error) {
	if f.onMainHead != nil {
		f.onMainHead()
	}
	if f.mainHead != "" {
		return f.mainHead, nil
	}
	if f.remote == nil {
		return "", ports.ErrNotFound
	}
	return f.remote.git("rev-parse", "main"), nil
}
func (f *fakeForge) SetupRepository(_ context.Context, ref string, _ []ports.File, _ string) error {
	if ref == "o/q" {
		return ports.ErrMainExists
	}
	return errors.New("unused")
}
func (f *fakeForge) Archived(_ context.Context, ref string) (bool, error) {
	return f.archived[ref], nil
}
func (f *fakeForge) number() int {
	f.next++
	return 100 + f.next
}
func (f *fakeForge) CreateApplyRequest(_ context.Context, _ string, n ports.NewApplyRequest) (ports.ApplyRequest, error) {
	for _, ar := range f.ars {
		if ar.Branch == n.Branch && ar.State == ports.StateOpen {
			return ports.ApplyRequest{}, ports.ErrBranchHasOpen
		}
	}
	num := f.number()
	head := ""
	if f.remote != nil {
		head = f.remote.git("rev-parse", n.Branch)
	}
	ar := ports.ApplyRequest{ID: strconv.Itoa(num), Number: num, Branch: n.Branch, Description: n.Description,
		HeadRevision: head, State: ports.StateOpen, Opener: n.Opener, Waiting: ports.WaitingReview, CreatedAt: time.Now().UTC()}
	f.ars = append(f.ars, ar)
	return ar, nil
}
func (f *fakeForge) ar(id string) *ports.ApplyRequest {
	for i := range f.ars {
		if f.ars[i].ID == id {
			return &f.ars[i]
		}
	}
	return nil
}
func (f *fakeForge) GetApplyRequest(_ context.Context, _, id string) (ports.ApplyRequest, error) {
	for _, ar := range f.ars {
		if ar.ID == id {
			return ar, nil
		}
	}
	return ports.ApplyRequest{}, ports.ErrNotFound
}
func (f *fakeForge) MergedApplyRequest(_ context.Context, _, sha string, _ time.Time) (ports.ApplyRequest, error) {
	for _, ar := range f.ars {
		if ar.State == ports.StateMerged && ar.MergedRevision == sha {
			return ar, nil
		}
	}
	return ports.ApplyRequest{}, ports.ErrNotFound
}
func (f *fakeForge) ListApplyRequests(_ context.Context, _, state string) ([]ports.ApplyRequest, error) {
	var out []ports.ApplyRequest
	for _, ar := range f.ars {
		if state == "open" && ar.State != ports.StateOpen || state == "closed" && ar.State == ports.StateOpen {
			continue
		}
		x := ar
		x.Comments = nil
		out = append(out, x)
	}
	return out, nil
}
func (f *fakeForge) SetWaiting(_ context.Context, _, id string, w ports.Waiting) error {
	if f.waitingErr != nil {
		return f.waitingErr
	}
	ar := f.ar(id)
	if ar == nil {
		return ports.ErrNotFound
	}
	// GitForge と同じに、既に付いているラベルは付け直さない（付けた出来事は増えない）。
	if ar.Waiting == w {
		return nil
	}
	ar.Waiting = w
	switch w {
	case ports.WaitingReview:
		ar.ReviewWaitingAt = time.Now().UTC()
	case ports.WaitingFix:
		// 付けた人は記録しない。ずれ直しは、付けた人を見ないからである（design/adapters/pull-requests.md の「ラベルがずれたとき」）。
		ar.FixWaitingLabeled = append(ar.FixWaitingLabeled, ports.LabelEvent{At: time.Now().UTC()})
	}
	return nil
}
func (f *fakeForge) UpdateDescription(_ context.Context, _, id, d string) error {
	f.ar(id).Description = d
	return nil
}

// Merge は、版のコミットを、リモートの main に squash merge する。
func (f *fakeForge) Merge(_ context.Context, _, id, head, title, message string) (string, error) {
	ar := f.ar(id)
	if f.mergeErr != nil {
		return "", f.mergeErr
	}
	if ar.HeadRevision != head {
		return "", ports.ErrHeadMoved
	}
	f.remote.git("checkout", "--quiet", "main")
	cmd := exec.Command("git", "-C", f.remote.dir, "merge", "--squash", head)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		f.remote.git("reset", "--quiet", "--hard", "HEAD")
		return "", fmt.Errorf("%w: %s", ports.ErrConflict, out)
	}
	// テストの merge の pull request の探し方（trailerLookup）が、merge した pull request を引けるように、テストだけの印を書く。
	sha := f.remote.commit(title + "\n\n" + message + "\nTest-Apply-Request: " + id + "\nTest-Merged-Head: " + head)
	ar.State, ar.MergedRevision = ports.StateMerged, sha
	return sha, nil
}
func (f *fakeForge) Close(_ context.Context, _, id, reason string) error {
	ar := f.ar(id)
	ar.State = ports.StateClosed
	return nil
}

// RecordMergeFailure は、コメントを足し、ラベルを付け直す（既に付いていても、付けた出来事を足す）。
func (f *fakeForge) RecordMergeFailure(ctx context.Context, repo, id string, mf ports.MergeFailure, w ports.Waiting) error {
	ar := f.ar(id)
	ar.Comments = append(ar.Comments, ports.Comment{Author: "bob", Body: "反映できなかった（" + mf.Reason + "）\n\n" + mf.Detail, CreatedAt: time.Now().UTC()})
	ar.Waiting = ports.WaitingNone
	return f.SetWaiting(ctx, repo, id, w)
}
func (f *fakeForge) AddReview(_ context.Context, _, id string, r ports.Review) (ports.Review, error) {
	rs := f.reviews[id]
	f.next++
	r.ID, r.CreatedAt = strconv.Itoa(f.next), time.Now().UTC()
	rs.Items = append(rs.Items, r)
	f.reviews[id] = rs
	return r, nil
}
func (f *fakeForge) ListReviews(_ context.Context, _, id string) (ports.Reviews, error) {
	return f.reviews[id], nil
}
func (f *fakeForge) CreateHumanRequest(_ context.Context, _ string, n ports.NewHumanRequest) (ports.HumanRequest, error) {
	key := n.Requester + "\n" + n.TargetNode + "\n" + n.Body
	if hr, ok := f.keys[key]; ok {
		return hr, nil
	}
	num := f.number()
	hr := ports.HumanRequest{ID: strconv.Itoa(num), Number: num, Kind: n.Kind, TargetNode: n.TargetNode, Body: n.Body, Requester: n.Requester}
	if f.keys == nil {
		f.keys = map[string]ports.HumanRequest{}
	}
	f.keys[key] = hr
	f.hrs = append(f.hrs, hr)
	return hr, nil
}
func (f *fakeForge) GetHumanRequest(_ context.Context, _, id string) (ports.HumanRequest, error) {
	for _, hr := range f.hrs {
		if hr.ID == id {
			return hr, nil
		}
	}
	return ports.HumanRequest{}, ports.ErrNotFound
}
func (f *fakeForge) ListHumanRequests(context.Context, string) ([]ports.HumanRequest, error) {
	return f.hrs, nil
}
func (f *fakeForge) Answer(_ context.Context, _, id, body, by string, files []ports.NewAttachment) error {
	for i := range f.hrs {
		if f.hrs[i].ID != id {
			continue
		}
		if a := f.hrs[i].Answer; a != nil {
			if a.Body == body && a.AnsweredBy == by {
				return nil
			}
			return ports.ErrAlreadyAnswered
		}
		ans := &ports.Answer{Body: body, AnsweredBy: by, CreatedAt: time.Now().UTC()}
		for _, file := range files {
			sha := tree.SHA256Hex(file.Content)
			f.files[sha] = file.Content
			ans.Attachments = append(ans.Attachments, ports.Attachment{Name: file.Name, SHA256: sha, ID: sha})
		}
		f.hrs[i].Answer = ans
		return nil
	}
	return ports.ErrNotFound
}
func (f *fakeForge) ReadAttachment(_ context.Context, _, _, sha string) ([]byte, error) {
	if b, ok := f.files[sha]; ok {
		return b, nil
	}
	return nil, ports.ErrNotFound
}

type fakeAuth struct{}

func (fakeAuth) Authenticate(_ context.Context, token string) (ports.Principal, error) {
	if token == "bad" {
		return ports.Principal{}, ports.ErrUnauthenticated
	}
	return ports.Principal{Login: token, Kind: ports.PrincipalClient}, nil
}
func (fakeAuth) CanRead(_ context.Context, token, _ string) (bool, error) {
	return token != "stranger", nil
}
func (fakeAuth) CanWrite(_ context.Context, token, _ string) (bool, error) {
	return token != "stranger" && token != "reader", nil
}
func (a fakeAuth) CanCreate(ctx context.Context, token, repo string) (bool, error) {
	return a.CanWrite(ctx, token, repo)
}

type source struct{ dir string }

// trailerLookup は、テストのための、merge の pull request を探す GitForge の代わりである。テストのリポジトリのコミットのメッセージの、
// テストだけの印（Test-Apply-Request: と Test-Merged-Head:）で探す。印がなければ、pull request はない。
type trailerLookup struct{ dir string }

func (v trailerLookup) MergedApplyRequest(_ context.Context, _ ports.Repository, sha string, _ time.Time) (string, string, error) {
	out, err := exec.Command("git", "-C", v.dir, "log", "-1", "--format=%B", sha).Output()
	if err != nil {
		return "", "", err
	}
	var ar, head string
	for _, l := range strings.Split(string(out), "\n") {
		if x, ok := strings.CutPrefix(l, "Test-Apply-Request: "); ok {
			ar = strings.TrimSpace(x)
		}
		if x, ok := strings.CutPrefix(l, "Test-Merged-Head: "); ok {
			head = strings.TrimSpace(x)
		}
	}
	return ar, head, nil
}

func (s source) ListRepositories(context.Context) ([]ports.Repository, error) {
	return []ports.Repository{{Name: "q", GitForgeRef: "o/q"}}, nil
}
func (s source) GitForgeRef(_ context.Context, name string) (string, error) { return "o/" + name, nil }
func (s source) GitURL(string) string                                       { return s.dir }

// fixture は、次の木を作る。
//   - コミット 1：根 H0、S1（H1、H2）
//   - コミット 2（apr_1 の反映）：H1 に検証結果 V1（true、artifact つき）
//   - ブランチ b（apr_2、open）：H2 の下に S2（H3）を作る
type fixture struct {
	b       *Backend
	forge   *fakeForge
	r       *remote
	c1, c2  string
	head    string
	art     []byte
	caller  Caller
	answerT time.Time
}

func setup(t *testing.T) *fixture {
	ctx := context.Background()
	r := &remote{t: t, dir: t.TempDir()}
	r.git("init", "--quiet", "--initial-branch=main")
	r.write(dH0+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H0, Statement: "根"}))
	r.write(dS1+"/story.md", tree.MarshalStory(tree.StoryContent{ID: S1, Title: "ストーリー1", Summary: "s"}))
	r.write(dH1+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H1, Statement: "子1"}))
	r.write(dH2+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H2, Statement: "子2"}))
	c1 := r.commit("用意\n\nLogiforge-Init: 1")
	art := []byte("data")
	sha := tree.SHA256Hex(art)
	r.write(tree.ArtifactPath(sha), art)
	r.write(dH1+"/verdicts/"+V1+".md", tree.MarshalVerdict(tree.VerdictContent{ID: V1, Value: true, Report: "# 結果\n\n本文\n",
		Artifacts: []tree.ArtifactRef{{Kind: "file", Name: "d.txt", SHA256: sha}}}))
	c2 := r.commit("反映\n\nTest-Apply-Request: apr_1")
	r.git("checkout", "--quiet", "-b", "b")
	r.write(dH2+"/"+S2+"/story.md", tree.MarshalStory(tree.StoryContent{ID: S2, Title: "新しいストーリー", Summary: "s"}))
	r.write(dH2+"/"+S2+"/"+H3+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H3, Statement: "孫"}))
	head := r.commit("版")
	r.git("checkout", "--quiet", "main")

	st := &store.Dir{Root: t.TempDir()}
	w := worker.New(worker.Config{Dir: t.TempDir(), Lookup: trailerLookup{r.dir}}, source{r.dir}, st)
	w.SyncAll(ctx)

	now := time.Now().UTC()
	forge := &fakeForge{
		ars: []ports.ApplyRequest{
			{ID: "apr_1", Number: 1, Branch: "a", Target: "main", State: ports.StateMerged, MergedRevision: c2},
			{ID: "apr_2", Number: 2, Branch: "b", State: ports.StateOpen, HeadRevision: head, CreatedAt: now.Add(time.Hour), Description: "説明"},
		},
		reviews: map[string]ports.Reviews{},
		hrs: []ports.HumanRequest{
			{ID: "3", Number: 3, Kind: ports.HumanRequestPVR, TargetNode: H2, Body: "測って",
				Answer: &ports.Answer{Body: "測った", AnsweredBy: "alice", CreatedAt: now, Attachments: []ports.Attachment{{Name: "p.jpg", SHA256: tree.SHA256Hex([]byte("jpg")), ID: "1"}}}},
		},
		files: map[string][]byte{tree.SHA256Hex([]byte("jpg")): []byte("jpg")},
	}
	git, err := workcopy.NewCache(t.TempDir(), nil, func(string) string { return r.dir })
	if err != nil {
		t.Fatal(err)
	}
	b := &Backend{Forge: forge, Auth: fakeAuth{}, Snap: &snapstore.Adapter{Store: st}, Git: git}
	return &fixture{b: b, forge: forge, r: r, c1: c1, c2: c2, head: head, art: art, caller: Caller{Token: "agent"}, answerT: now}
}

func code(err error) Code {
	if e := AsError(err); e != nil {
		return e.Code
	}
	return ""
}

func TestReads(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, c := f.b, f.caller

	repo, err := b.GetRepository(ctx, c, "q")
	if err != nil || repo.RootHypothesisID != H0 || repo.RootValue != nil || repo.GitURL != "https://forge.example.com/o/q.git" {
		t.Fatalf("repository: %+v %v", repo, err)
	}
	if _, err := b.GetRepository(ctx, Caller{Token: "stranger"}, "q"); code(err) != CodeNotFound {
		t.Fatalf("読めない主体：%v", err)
	}
	if _, err := b.GetTree(ctx, c, "q", At{Commit: strings.Repeat("0", 40)}); code(err) != CodeSnapshotPending {
		t.Fatalf("ないリビジョン：%v", err)
	}
	tr, err := b.GetTree(ctx, c, "q", At{Commit: f.c1})
	if err != nil || tr.Commit != f.c1 || len(tr.Warnings) != 0 {
		t.Fatalf("tree: %+v %v", tr, err)
	}

	nodes, err := b.ListNodes(ctx, c, "q", At{}, "", -1)
	if err != nil || len(nodes.Nodes) != 4 {
		t.Fatalf("nodes: %+v %v", nodes, err)
	}
	h2 := nodes.Nodes[3].(*Hypothesis)
	if h2.ID != H2 || len(h2.OpenApplyRequestIDs) != 1 || h2.OpenApplyRequestIDs[0] != "apr_2" || !h2.Leaf || h2.Value != nil {
		t.Fatalf("H2: %+v", h2)
	}
	if n, _ := b.ListNodes(ctx, c, "q", At{}, S1, 0); len(n.Nodes) != 1 {
		t.Fatalf("depth 0: %+v", n)
	}

	node, err := b.GetNode(ctx, c, "q", H1, At{})
	if err != nil || node.Verdict == nil || node.Verdict.SubmittedCommit != f.c2 || node.Verdict.ApplyRequestID != "apr_1" || node.Verdict.SupersededCommit != nil {
		t.Fatalf("node: %+v %v", node.Verdict, err)
	}
	if v := node.Node.(*Hypothesis); v.Value == nil || !*v.Value || v.ValidVerdictID == nil {
		t.Fatalf("H1: %+v", v)
	}

	hist, err := b.GetNodeHistory(ctx, c, "q", H1, "", 1)
	if err != nil || len(hist.Items) != 1 || hist.Items[0].Commit != f.c2 || !hist.Items[0].Reflected || hist.NextCursor == nil {
		t.Fatalf("history: %+v %v", hist, err)
	}
	hist2, _ := b.GetNodeHistory(ctx, c, "q", H1, *hist.NextCursor, 1)
	if len(hist2.Items) != 1 || hist2.Items[0].Commit != f.c1 || hist2.Items[0].Reflected || hist2.NextCursor != nil {
		t.Fatalf("history 2: %+v", hist2)
	}

	ars, err := b.ListApplyRequests(ctx, c, "q", "", "", 1)
	if err != nil || len(ars.Items) != 1 || ars.Items[0].ID != "apr_1" || ars.NextCursor == nil {
		t.Fatalf("ars: %+v %v", ars, err)
	}
	ars2, _ := b.ListApplyRequests(ctx, c, "q", "", *ars.NextCursor, 1)
	if len(ars2.Items) != 1 || ars2.Items[0].ID != "apr_2" || ars2.NextCursor != nil {
		t.Fatalf("ars 2: %+v", ars2)
	}
	if merged := ars.Items[0]; merged.MergedCommit == nil || *merged.MergedCommit != f.c2 {
		t.Fatalf("merged: %+v", merged)
	}
	if open, _ := b.ListApplyRequests(ctx, c, "q", "open", "", 0); len(open.Items) != 1 {
		t.Fatalf("state=open: %+v", open)
	}

	ar, err := b.GetApplyRequest(ctx, c, "q", "apr_2")
	if err != nil || ar.HeadCommit != f.head || len(ar.HeadCommits) != 1 || ar.HeadCommits[0].BaseCommit != f.c2 || ar.HeadCommits[0].SeenAt == nil || ar.Awaiting == nil || *ar.Awaiting != "review" {
		t.Fatalf("ar: %+v %v", ar, err)
	}
	if len(ar.Operations) != 1 || ar.Operations[0].Kind != "story.create" || ar.Operations[0].ParentHypothesisID != H2 || ar.Operations[0].Hypotheses[0].ID != H3 {
		t.Fatalf("ops: %+v", ar.Operations)
	}

	mat, err := b.GetReviewMaterial(ctx, c, "q", "apr_2")
	if err != nil || len(mat.Targets) != 1 || mat.Targets[0].(*Hypothesis).ID != H2 || len(mat.Ancestors) != 1 || mat.Ancestors[0].ID != H0 {
		t.Fatalf("material: %+v %v", mat, err)
	}

	data, err := b.GetArtifact(ctx, c, "q", tree.SHA256Hex(f.art), "")
	if err != nil || string(data) != "data" {
		t.Fatalf("artifact: %q %v", data, err)
	}
	if _, err := b.GetArtifact(ctx, c, "q", tree.SHA256Hex(f.art), f.c1); code(err) != CodeNotFound {
		t.Fatalf("コミット 1 には artifact がない：%v", err)
	}

	vd, err := b.GetVerdict(ctx, c, "q", V1)
	if err != nil || vd.HypothesisID != H1 || len(vd.Artifacts) != 1 {
		t.Fatalf("verdict: %+v %v", vd, err)
	}

	hrs, err := b.ListHumanRequests(ctx, c, "q", "pvr", "answered", "", "", 0)
	if err != nil || len(hrs.Items) != 1 || hrs.Items[0].Answer == nil || len(hrs.Items[0].Answer.Attachments) != 1 {
		t.Fatalf("hrs: %+v %v", hrs, err)
	}
	if got, _ := b.ListHumanRequests(ctx, c, "q", "", "open", "", "", 0); len(got.Items) != 0 {
		t.Fatalf("open: %+v", got)
	}
	att, err := b.GetAnswerAttachment(ctx, c, "q", "3", tree.SHA256Hex([]byte("jpg")))
	if err != nil || string(att) != "jpg" {
		t.Fatalf("attachment: %v", err)
	}
}

func TestWorkAndCompletion(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, c := f.b, f.caller

	w, err := b.ListWork(ctx, c, "q", WorkQuery{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, it := range w.Items {
		kinds[it.Kind] = it.SubjectID
	}
	// H2 は apr_2 が対象にしているので、未決の葉に出ない。
	if _, ok := kinds["undecided_leaf"]; ok || kinds["review_waiting"] != "apr_2" || kinds["human_request_answered"] != "3" {
		t.Fatalf("work: %+v", w.Items)
	}
	if w.Counts["review_waiting"] != 1 {
		t.Fatalf("counts: %+v", w.Counts)
	}
	all, _ := b.ListWork(ctx, c, "q", WorkQuery{IncludeClaimed: true, Kinds: []string{"undecided_leaf"}})
	if len(all.Items) != 1 || all.Items[0].SubjectID != H2 {
		t.Fatalf("include_claimed: %+v", all.Items)
	}

	// ページ：カーソルは、計算したコミットに結びつける。
	p1, _ := b.ListWork(ctx, c, "q", WorkQuery{Limit: 1})
	if len(p1.Items) != 1 || p1.NextCursor == nil {
		t.Fatalf("page 1: %+v", p1)
	}
	p2, _ := b.ListWork(ctx, c, "q", WorkQuery{Limit: 1, Cursor: *p1.NextCursor})
	if len(p2.Items) != 1 || p2.Items[0] == p1.Items[0] || p2.Commit != p1.Commit {
		t.Fatalf("page 2: %+v", p2)
	}

	// 修正待ち：pull request に修正待ちのラベルがある（一覧は review を読まない）。
	f.forge.ar("apr_2").Waiting = ports.WaitingFix
	w2, _ := b.ListWork(ctx, c, "q", WorkQuery{Kinds: []string{"fix_waiting", "review_waiting"}})
	if len(w2.Items) != 1 || w2.Items[0].Kind != "fix_waiting" {
		t.Fatalf("fix: %+v", w2.Items)
	}

	comp, err := b.GetCompletion(ctx, c, "q")
	if err != nil || comp.Decided || comp.Done {
		t.Fatalf("completion: %+v %v", comp, err)
	}
	if _, err := b.ListWork(ctx, c, "q", WorkQuery{Kinds: []string{"nope"}}); code(err) != CodeValidationFailed {
		t.Fatalf("kind: %v", err)
	}
}

func TestExport(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	e, err := f.b.ExportTree(ctx, f.caller, "q", "")
	if err != nil || e.Format != ExportFormat || len(e.Nodes) != 4 || len(e.Verdicts) != 1 || !e.Verdicts[0].Valid {
		t.Fatalf("export: %+v %v", e, err)
	}
	md := e.Markdown()
	for _, want := range []string{"# q", "## 仮説 " + H0, "## ストーリー " + S1 + "：ストーリー1", "### 検証結果 " + V1 + "（有効）", "> # 結果", "- 値：真", "- artifact：d.txt（file："} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown に %q がない:\n%s", want, md)
		}
	}
}

func TestAwaiting(t *testing.T) {
	for w, want := range map[ports.Waiting]string{ports.WaitingNone: "review", ports.WaitingReview: "review", ports.WaitingFix: "fix", ports.WaitingBoth: "fix"} {
		if got := awaiting(w); got == nil || *got != want {
			t.Fatalf("%q: %v", w, got)
		}
	}
}

// ずれたラベルの正しいもの：出来事のうち、いちばん新しいもので決める（design/adapters/pull-requests.md の「ラベルがずれたとき」）。
func TestDesiredWaiting(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rc := func(at time.Time) ports.Review { return ports.Review{Kind: ports.ReviewRequestChanges, CreatedAt: at} }
	cases := []struct {
		name    string
		ar      ports.ApplyRequest
		reviews []ports.Review
		want    ports.Waiting
	}{
		{"何もない", ports.ApplyRequest{}, nil, ports.WaitingReview},
		{"request changes", ports.ApplyRequest{ReviewWaitingAt: t0}, []ports.Review{rc(t0.Add(time.Minute))}, ports.WaitingFix},
		{"直し終えたと申告した", ports.ApplyRequest{ReviewWaitingAt: t0.Add(2 * time.Minute)}, []ports.Review{rc(t0.Add(time.Minute))}, ports.WaitingReview},
		{"approve は修正待ちにしない", ports.ApplyRequest{ReviewWaitingAt: t0}, []ports.Review{{Kind: ports.ReviewApprove, CreatedAt: t0.Add(time.Minute)}}, ports.WaitingReview},
		{"確かめに通らず修正待ちのラベルを付けた（approve は出していない）", ports.ApplyRequest{ReviewWaitingAt: t0, FixWaitingLabeled: []ports.LabelEvent{{Actor: "bob", At: t0.Add(time.Minute)}}}, nil, ports.WaitingFix},
		// ラベルを付けられるのは、書く権限がある人だけなので、だれが付けたかは見ない。
		{"GitForge の画面で修正待ちのラベルを付けた", ports.ApplyRequest{ReviewWaitingAt: t0, Waiting: ports.WaitingFix, FixWaitingLabeled: []ports.LabelEvent{{Actor: "admin", At: t0.Add(time.Minute)}}},
			[]ports.Review{{Kind: ports.ReviewApprove, Reviewer: "bob", CreatedAt: t0.Add(30 * time.Second)}}, ports.WaitingFix},
		{"修正待ちのラベルのあとの approve", ports.ApplyRequest{ReviewWaitingAt: t0, Waiting: ports.WaitingFix, FixWaitingLabeled: []ports.LabelEvent{{Actor: "bob", At: t0.Add(time.Minute)}}},
			[]ports.Review{{Kind: ports.ReviewApprove, Reviewer: "bob", CreatedAt: t0.Add(2 * time.Minute)}}, ports.WaitingReview},
		{"同じ秒なら今の修正待ちを変えない", ports.ApplyRequest{ReviewWaitingAt: t0, Waiting: ports.WaitingFix}, []ports.Review{rc(t0.Add(300 * time.Millisecond))}, ports.WaitingFix},
		{"同じ秒なら今のレビュー待ちを変えない", ports.ApplyRequest{ReviewWaitingAt: t0.Add(500 * time.Millisecond), Waiting: ports.WaitingReview}, []ports.Review{rc(t0)}, ports.WaitingReview},
		{"request changes のあとの approve", ports.ApplyRequest{ReviewWaitingAt: t0}, []ports.Review{rc(t0.Add(time.Minute)), {Kind: ports.ReviewApprove, CreatedAt: t0.Add(2 * time.Minute)}}, ports.WaitingReview},
		{"修正待ちのラベルのあとのレビュー待ちのラベル（直し終えたと申告した）", ports.ApplyRequest{FixWaitingLabeled: []ports.LabelEvent{{At: t0}}, ReviewWaitingAt: t0.Add(time.Minute)}, nil, ports.WaitingReview},
	}
	for _, c := range cases {
		if got := desiredWaiting(c.ar, c.reviews); got != c.want {
			t.Fatalf("%s: %q", c.name, got)
		}
	}
}

// 回答を引用した検証結果が反映されたら、回答済みの依頼は作業から外れる。背景情報にたまたま書いた識別子では、外れない。
func TestAnsweredRequestLeavesWork(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	answered := func() bool {
		w, err := f.b.ListWork(ctx, f.caller, "q", WorkQuery{Kinds: []string{"human_request_answered"}})
		if err != nil {
			t.Fatal(err)
		}
		return len(w.Items) == 1
	}
	if !answered() {
		t.Fatal("回答済みの依頼が、作業にない")
	}
	sha := tree.SHA256Hex([]byte("jpg"))
	// 背景情報に識別子を書いても、外れない（open の apply request apr_2 の差分に足す）。
	f.r.git("checkout", "--quiet", "b")
	f.r.write(dH2+"/"+S2+"/"+H3+"/background.md", []byte("3 の回答を見た\n"))
	head := f.r.commit("版 2")
	f.forge.ars[1].HeadRevision = head
	w := worker.New(worker.Config{Dir: t.TempDir(), Lookup: trailerLookup{f.r.dir}}, source{f.r.dir}, f.b.Snap.(*snapstore.Adapter).Store.(*store.Dir))
	w.SyncAll(ctx)
	if !answered() {
		t.Fatal("背景情報の識別子で、作業から外れた")
	}
	// 検証結果が回答の添付の URL を引用すると、外れる。
	f.r.write(dH2+"/"+S2+"/"+H3+"/verdicts/v-8888888888.md", tree.MarshalVerdict(tree.VerdictContent{ID: "v-8888888888", Value: true, Report: "r\n",
		Artifacts: []tree.ArtifactRef{{Kind: "link", Name: "写真", URI: "https://logiforge.example.com/v1/repositories/q/human-requests/3/attachments/" + sha, SHA256: sha}}}))
	head = f.r.commit("版 3")
	f.forge.ars[1].HeadRevision = head
	w.SyncAll(ctx)
	if answered() {
		t.Fatal("回答を引用した検証結果があるのに、作業から外れない")
	}
}

// GetCommitForTest は、コミットの情報の trailer を読む。
func (b *Backend) GetCommitForTest(ctx context.Context, repo, sha string) ([]snapshotTrailer, error) {
	c, err := b.Snap.Commit(ctx, repo, sha)
	return c.Trailers, err
}

// レビューは、だれが（reviewer）と、いつ（created_at）を返す。並びは、GitForge から読んだ順（書いた順）のまま。
func TestReviewAuthorAndTime(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	t1 := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	f.forge.reviews["apr_2"] = ports.Reviews{Items: []ports.Review{
		{ID: "rev_1", Kind: ports.ReviewComment, Revision: f.head, Body: "質問", Reviewer: "bob", CreatedAt: t1},
		{ID: "rev_2", Kind: ports.ReviewComment, Revision: f.head, Body: "答え", Reviewer: "agent", CreatedAt: t1.Add(time.Minute)},
		{ID: "rev_3", Kind: ports.ReviewComment, Revision: f.head, Body: "時刻なし"},
	}}
	ar, err := f.b.GetApplyRequest(ctx, f.caller, "q", "apr_2")
	if err != nil {
		t.Fatal(err)
	}
	rs := ar.Reviews
	if len(rs) != 3 || rs[0].Reviewer != "bob" || !rs[0].CreatedAt.Equal(t1) || rs[1].Reviewer != "agent" || !rs[1].CreatedAt.After(*rs[0].CreatedAt) {
		t.Fatalf("レビュー：%+v", rs)
	}
	if rs[2].CreatedAt != nil {
		t.Fatalf("時刻が読めないときは null：%+v", rs[2])
	}
}

// GitForge の誤りは、ポートの誤りで包んであれば、内部の誤り（500）にしない。
func TestAsErrorGitForge(t *testing.T) {
	for err, want := range map[error]Code{
		fmt.Errorf("github: 404: %w", ports.ErrNotFound):    CodeNotFound,
		fmt.Errorf("github: 403: %w", ports.ErrForbidden):   CodeForbidden,
		fmt.Errorf("github: 502: %w", ports.ErrUnavailable): CodeUnavailable,
	} {
		if got := code(err); got != want {
			t.Errorf("%v：%q", err, got)
		}
	}
	if AsError(errors.New("github: 422")) != nil {
		t.Error("ポートの誤りで包んでいない誤りは、内部の誤り")
	}
}
