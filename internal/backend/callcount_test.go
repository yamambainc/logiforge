package backend

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// countingForge は、GitForge のポートの、すべてのメソッドの呼び出しを数える（呼び出しの回数のテスト）。
// ports.GitForge を埋め込まずに、すべてのメソッドを書く。なぜ：埋め込むと、数えていないメソッドを項目ごとに呼んでも、テストが落ちないから。
type countingForge struct {
	inner ports.GitForge
	calls map[string]int
}

var _ ports.GitForge = (*countingForge)(nil)

func (c *countingForge) n(name string) { c.calls[name]++ }

func (c *countingForge) ListRepositories(ctx context.Context) ([]ports.Repository, error) {
	c.n("ListRepositories")
	return c.inner.ListRepositories(ctx)
}
func (c *countingForge) GitForgeRef(ctx context.Context, name string) (string, error) {
	c.n("GitForgeRef")
	return c.inner.GitForgeRef(ctx, name)
}
func (c *countingForge) GitURL(ref string) string { c.n("GitURL"); return c.inner.GitURL(ref) }
func (c *countingForge) MainHead(ctx context.Context, ref string) (string, error) {
	c.n("MainHead")
	return c.inner.MainHead(ctx, ref)
}
func (c *countingForge) SetupRepository(ctx context.Context, ref string, files []ports.File, message string) error {
	c.n("SetupRepository")
	return c.inner.SetupRepository(ctx, ref, files, message)
}
func (c *countingForge) Archived(ctx context.Context, ref string) (bool, error) {
	c.n("Archived")
	return c.inner.Archived(ctx, ref)
}
func (c *countingForge) CreateApplyRequest(ctx context.Context, repo string, ar ports.NewApplyRequest) (ports.ApplyRequest, error) {
	c.n("CreateApplyRequest")
	return c.inner.CreateApplyRequest(ctx, repo, ar)
}
func (c *countingForge) GetApplyRequest(ctx context.Context, repo, id string) (ports.ApplyRequest, error) {
	c.n("GetApplyRequest")
	return c.inner.GetApplyRequest(ctx, repo, id)
}
func (c *countingForge) ListApplyRequests(ctx context.Context, repo, state string) ([]ports.ApplyRequest, error) {
	c.n("ListApplyRequests")
	return c.inner.ListApplyRequests(ctx, repo, state)
}
func (c *countingForge) UpdateDescription(ctx context.Context, repo, id, description string) error {
	c.n("UpdateDescription")
	return c.inner.UpdateDescription(ctx, repo, id, description)
}
func (c *countingForge) Merge(ctx context.Context, repo, id, headSHA, title, message string) (string, error) {
	c.n("Merge")
	return c.inner.Merge(ctx, repo, id, headSHA, title, message)
}
func (c *countingForge) Close(ctx context.Context, repo, id, reason, session string) error {
	c.n("Close")
	return c.inner.Close(ctx, repo, id, reason, session)
}
func (c *countingForge) RecordMergeFailure(ctx context.Context, repo, id string, f ports.MergeFailure) error {
	c.n("RecordMergeFailure")
	return c.inner.RecordMergeFailure(ctx, repo, id, f)
}
func (c *countingForge) SetWaiting(ctx context.Context, repo, id string, w ports.Waiting) error {
	c.n("SetWaiting")
	return c.inner.SetWaiting(ctx, repo, id, w)
}
func (c *countingForge) AddReview(ctx context.Context, repo, id string, r ports.Review) (ports.Review, error) {
	c.n("AddReview")
	return c.inner.AddReview(ctx, repo, id, r)
}
func (c *countingForge) ListReviews(ctx context.Context, repo, id string) (ports.Reviews, error) {
	c.n("ListReviews")
	return c.inner.ListReviews(ctx, repo, id)
}
func (c *countingForge) MergedApplyRequest(ctx context.Context, repo, sha string, committed time.Time) (ports.ApplyRequest, error) {
	c.n("MergedApplyRequest")
	return c.inner.MergedApplyRequest(ctx, repo, sha, committed)
}
func (c *countingForge) CreateHumanRequest(ctx context.Context, repo string, hr ports.NewHumanRequest) (ports.HumanRequest, error) {
	c.n("CreateHumanRequest")
	return c.inner.CreateHumanRequest(ctx, repo, hr)
}
func (c *countingForge) GetHumanRequest(ctx context.Context, repo, id string) (ports.HumanRequest, error) {
	c.n("GetHumanRequest")
	return c.inner.GetHumanRequest(ctx, repo, id)
}
func (c *countingForge) ListHumanRequests(ctx context.Context, repo string) ([]ports.HumanRequest, error) {
	c.n("ListHumanRequests")
	return c.inner.ListHumanRequests(ctx, repo)
}
func (c *countingForge) Answer(ctx context.Context, repo, id, body, answeredBy string, files []ports.NewAttachment) error {
	c.n("Answer")
	return c.inner.Answer(ctx, repo, id, body, answeredBy, files)
}
func (c *countingForge) ReadAttachment(ctx context.Context, repo, id, sha256 string) ([]byte, error) {
	c.n("ReadAttachment")
	return c.inner.ReadAttachment(ctx, repo, id, sha256)
}

// 作業の一覧と、作業の終わりの判定と、apply request の一覧の、呼び出しの回数のテスト（design/backend/list-reads.md の「呼び出しの回数のテスト」）。
// open の apply request と人への依頼が 2 つずつでも 40 個ずつでも、GitForge のポートを、どのメソッドも同じ回数だけ呼ぶ。
func TestListCallCount(t *testing.T) {
	ctx := context.Background()
	counts := map[int]map[string]int{}
	for _, n := range []int{2, 40} {
		f := setup(t)
		base := f.forge.ars[1]
		hbase := f.forge.hrs[0]
		for i := 0; i < n; i++ {
			hr := hbase
			hr.ID = fmt.Sprintf("%d", 500+i)
			hr.Number = 500 + i
			f.forge.hrs = append(f.forge.hrs, hr)
		}
		for i := 0; i < n; i++ {
			ar := base
			ar.ID = fmt.Sprintf("%d", 100+i)
			ar.Number = 100 + i
			ar.CreatedAt = time.Now().Add(time.Hour)
			f.forge.ars = append(f.forge.ars, ar)
		}
		cf := &countingForge{inner: f.forge, calls: map[string]int{}}
		f.b.Forge = cf
		if _, err := f.b.ListWork(ctx, f.caller, "q", WorkQuery{}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.b.GetCompletion(ctx, f.caller, "q"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.b.ListApplyRequests(ctx, f.caller, "q", "open", "", 200); err != nil {
			t.Fatal(err)
		}
		if _, err := f.b.ListHumanRequests(ctx, f.caller, "q", "", "", "", "", 200); err != nil {
			t.Fatal(err)
		}
		counts[n] = cf.calls
	}
	if fmt.Sprint(counts[2]) != fmt.Sprint(counts[40]) {
		t.Fatalf("GitForge のポートを呼ぶ回数が、apply request と人への依頼の数で変わった：2 個 %v、40 個 %v", counts[2], counts[40])
	}
	// 4 つの要求で、それぞれの一覧を 1 回ずつだけ読む（作業の一覧、作業の終わりの判定、apply request の一覧、人への依頼の一覧）。
	if counts[40]["ListApplyRequests"] != 3 || counts[40]["ListHumanRequests"] != 3 {
		t.Fatalf("1 回の要求の中で、同じ一覧を二度読んだ：%v", counts[40])
	}
	for _, name := range []string{"GetApplyRequest", "ListReviews", "GetHumanRequest", "SetWaiting", "ReadAttachment", "MergedApplyRequest"} {
		if counts[40][name] != 0 {
			t.Fatalf("一覧で %s を呼んだ：%v", name, counts[40])
		}
	}
}
