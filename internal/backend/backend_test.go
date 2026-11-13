package backend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/snapstore"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
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
}

func (f *fakeForge) ListRepositories(context.Context) ([]ports.Repository, error) { return nil, nil }
func (f *fakeForge) GitForgeRef(_ context.Context, name string) (string, error) {
	return "o/" + name, nil
}
func (f *fakeForge) GitURL(ref string) string { return "https://forge.example.com/" + ref + ".git" }
func (f *fakeForge) SetupRepository(context.Context, string, []ports.File, string) error {
	return errors.New("unused")
}
func (f *fakeForge) CreateApplyRequest(context.Context, string, ports.NewApplyRequest) (ports.ApplyRequest, error) {
	return ports.ApplyRequest{}, errors.New("unused")
}
func (f *fakeForge) GetApplyRequest(_ context.Context, _, id string) (ports.ApplyRequest, error) {
	for _, ar := range f.ars {
		if ar.ID == id {
			return ar, nil
		}
	}
	return ports.ApplyRequest{}, ports.ErrNotFound
}
func (f *fakeForge) ListApplyRequests(context.Context, string) ([]ports.ApplyRequest, error) {
	return f.ars, nil
}
func (f *fakeForge) UpdateDescription(context.Context, string, string, string) error { return nil }
func (f *fakeForge) Merge(context.Context, string, string, string, string, string) (string, error) {
	return "", errors.New("unused")
}
func (f *fakeForge) Close(context.Context, string, string, string, string) error { return nil }
func (f *fakeForge) RecordMergeFailure(context.Context, string, string, ports.MergeFailure) error {
	return nil
}
func (f *fakeForge) AddReview(context.Context, string, string, ports.Review) error { return nil }
func (f *fakeForge) ListReviews(_ context.Context, _, id string) (ports.Reviews, error) {
	return f.reviews[id], nil
}
func (f *fakeForge) CreateHumanRequest(context.Context, string, ports.NewHumanRequest) (ports.HumanRequest, error) {
	return ports.HumanRequest{}, errors.New("unused")
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
func (f *fakeForge) Answer(context.Context, string, string, string, string, []ports.NewAttachment) error {
	return nil
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

type source struct{ dir string }

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
	c2 := r.commit("反映\n\nLogiforge-Apply-Request: apr_1\nLogiforge-Session-Id: s1")
	r.git("checkout", "--quiet", "-b", "b")
	r.write(dH2+"/"+S2+"/story.md", tree.MarshalStory(tree.StoryContent{ID: S2, Title: "新しいストーリー", Summary: "s"}))
	r.write(dH2+"/"+S2+"/"+H3+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H3, Statement: "孫"}))
	head := r.commit("版\n\nLogiforge-Session-Id: s2")
	r.git("checkout", "--quiet", "main")

	st := &store.Dir{Root: t.TempDir()}
	w := worker.New(worker.Config{Dir: t.TempDir()}, source{r.dir}, st)
	w.SyncAll(ctx)

	now := time.Now().UTC()
	forge := &fakeForge{
		ars: []ports.ApplyRequest{
			{ID: "apr_1", Number: 1, Branch: "a", State: ports.StateMerged},
			{ID: "apr_2", Number: 2, Branch: "b", State: ports.StateOpen, FirstRevision: head, HeadRevision: head, CreatedAt: now.Add(time.Hour), Description: "説明"},
		},
		reviews: map[string]ports.Reviews{},
		hrs: []ports.HumanRequest{
			{ID: "hrq_1", Number: 3, Kind: ports.HumanRequestPVR, TargetNode: H2, Body: "測って",
				Answer: &ports.Answer{Body: "測った", AnsweredBy: "alice", CreatedAt: now, Attachments: []ports.Attachment{{Name: "p.jpg", SHA256: tree.SHA256Hex([]byte("jpg")), ID: "1"}}}},
		},
		files: map[string][]byte{tree.SHA256Hex([]byte("jpg")): []byte("jpg")},
	}
	b := &Backend{Forge: forge, Auth: fakeAuth{}, Snap: &snapstore.Adapter{Store: st}}
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
	if _, err := b.GetTree(ctx, c, "q", strings.Repeat("0", 40)); code(err) != CodeSnapshotPending {
		t.Fatalf("ないリビジョン：%v", err)
	}
	tr, err := b.GetTree(ctx, c, "q", f.c1)
	if err != nil || tr.Revision != f.c1 || len(tr.Warnings) != 0 {
		t.Fatalf("tree: %+v %v", tr, err)
	}

	nodes, err := b.ListNodes(ctx, c, "q", "", "", -1)
	if err != nil || len(nodes.Nodes) != 4 {
		t.Fatalf("nodes: %+v %v", nodes, err)
	}
	h2 := nodes.Nodes[3].(*Hypothesis)
	if h2.ID != H2 || len(h2.OpenApplyRequestIDs) != 1 || h2.OpenApplyRequestIDs[0] != "apr_2" || !h2.Leaf || h2.Value != nil {
		t.Fatalf("H2: %+v", h2)
	}
	if n, _ := b.ListNodes(ctx, c, "q", "", S1, 0); len(n.Nodes) != 1 {
		t.Fatalf("depth 0: %+v", n)
	}

	node, err := b.GetNode(ctx, c, "q", H1, "")
	if err != nil || node.Verdict == nil || node.Verdict.SubmittedRevision != f.c2 || node.Verdict.ApplyRequestID != "apr_1" || node.Verdict.SupersededRevision != nil {
		t.Fatalf("node: %+v %v", node.Verdict, err)
	}
	if v := node.Node.(*Hypothesis); v.Value == nil || !*v.Value || v.ValidVerdictID == nil {
		t.Fatalf("H1: %+v", v)
	}

	hist, err := b.GetNodeHistory(ctx, c, "q", H1, "", 1)
	if err != nil || len(hist.Items) != 1 || hist.Items[0].Revision != f.c2 || !hist.Items[0].Reflected || hist.NextCursor == nil {
		t.Fatalf("history: %+v %v", hist, err)
	}
	hist2, _ := b.GetNodeHistory(ctx, c, "q", H1, *hist.NextCursor, 1)
	if len(hist2.Items) != 1 || hist2.Items[0].Revision != f.c1 || hist2.Items[0].Reflected || hist2.NextCursor != nil {
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
	if merged := ars.Items[0]; merged.MergedRevision == nil || *merged.MergedRevision != f.c2 {
		t.Fatalf("merged: %+v", merged)
	}
	if open, _ := b.ListApplyRequests(ctx, c, "q", "open", "", 0); len(open.Items) != 1 {
		t.Fatalf("state=open: %+v", open)
	}

	ar, err := b.GetApplyRequest(ctx, c, "q", "apr_2")
	if err != nil || ar.Version != 1 || len(ar.Versions) != 1 || ar.Versions[0].BaseRevision != f.c2 || ar.Awaiting == nil || *ar.Awaiting != "review" {
		t.Fatalf("ar: %+v %v", ar, err)
	}
	if ar.Versions[0].AuthorSessionID == nil || *ar.Versions[0].AuthorSessionID != "s2" {
		t.Fatalf("session: %+v", ar.Versions[0])
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
	att, err := b.GetAnswerAttachment(ctx, c, "q", "hrq_1", tree.SHA256Hex([]byte("jpg")))
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
	if _, ok := kinds["undecided_leaf"]; ok || kinds["review_waiting"] != "apr_2" || kinds["human_request_answered"] != "hrq_1" {
		t.Fatalf("work: %+v", w.Items)
	}
	if w.Counts["review_waiting"] != 1 {
		t.Fatalf("counts: %+v", w.Counts)
	}
	all, _ := b.ListWork(ctx, c, "q", WorkQuery{IncludeClaimed: true, Kinds: []string{"undecided_leaf"}})
	if len(all.Items) != 1 || all.Items[0].SubjectID != H2 {
		t.Fatalf("include_claimed: %+v", all.Items)
	}

	// ページ：カーソルは、計算したリビジョンに結びつける。
	p1, _ := b.ListWork(ctx, c, "q", WorkQuery{Limit: 1})
	if len(p1.Items) != 1 || p1.NextCursor == nil {
		t.Fatalf("page 1: %+v", p1)
	}
	p2, _ := b.ListWork(ctx, c, "q", WorkQuery{Limit: 1, Cursor: *p1.NextCursor})
	if len(p2.Items) != 1 || p2.Items[0] == p1.Items[0] || p2.Revision != p1.Revision {
		t.Fatalf("page 2: %+v", p2)
	}

	// 修正待ち：今の版への最後の決めるレビューが request_changes。
	f.forge.reviews["apr_2"] = ports.Reviews{Items: []ports.Review{{ID: "rev_1", Kind: ports.ReviewRequestChanges, Revision: f.head}}}
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
