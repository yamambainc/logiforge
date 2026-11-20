package backend

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// countingForge は、GitForge のポートの呼び出しを数える（呼び出しの回数のテスト）。
type countingForge struct {
	ports.GitForge
	calls map[string]int
}

func (c *countingForge) n(name string) { c.calls[name]++ }

func (c *countingForge) ListApplyRequests(ctx context.Context, repo, state string) ([]ports.ApplyRequest, error) {
	c.n("ListApplyRequests")
	return c.GitForge.ListApplyRequests(ctx, repo, state)
}
func (c *countingForge) GetApplyRequest(ctx context.Context, repo, id string) (ports.ApplyRequest, error) {
	c.n("GetApplyRequest")
	return c.GitForge.GetApplyRequest(ctx, repo, id)
}
func (c *countingForge) ListReviews(ctx context.Context, repo, id string) (ports.Reviews, error) {
	c.n("ListReviews")
	return c.GitForge.ListReviews(ctx, repo, id)
}
func (c *countingForge) SetWaiting(ctx context.Context, repo, id string, w ports.Waiting) error {
	c.n("SetWaiting")
	return c.GitForge.SetWaiting(ctx, repo, id, w)
}
func (c *countingForge) ListHumanRequests(ctx context.Context, repo string) ([]ports.HumanRequest, error) {
	c.n("ListHumanRequests")
	return c.GitForge.ListHumanRequests(ctx, repo)
}
func (c *countingForge) GetHumanRequest(ctx context.Context, repo, id string) (ports.HumanRequest, error) {
	c.n("GetHumanRequest")
	return c.GitForge.GetHumanRequest(ctx, repo, id)
}
func (c *countingForge) MainHead(ctx context.Context, ref string) (string, error) {
	c.n("MainHead")
	return c.GitForge.MainHead(ctx, ref)
}

// 作業の一覧と、作業の終わりの判定と、apply request の一覧の、呼び出しの回数のテスト（design/backend/list-reads.md の「呼び出しの回数のテスト」）。
// open の apply request が 2 つでも 40 個でも、GitForge のポートを呼ぶ回数は同じである。
func TestListCallCount(t *testing.T) {
	ctx := context.Background()
	counts := map[int]map[string]int{}
	for _, n := range []int{2, 40} {
		f := setup(t)
		base := f.forge.ars[1]
		for i := 0; i < n; i++ {
			ar := base
			ar.ID = fmt.Sprintf("%d", 100+i)
			ar.Number = 100 + i
			ar.CreatedAt = time.Now().Add(time.Hour)
			f.forge.ars = append(f.forge.ars, ar)
		}
		cf := &countingForge{GitForge: f.forge, calls: map[string]int{}}
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
		t.Fatalf("GitForge のポートを呼ぶ回数が、apply request の数で変わった：2 個 %v、40 個 %v", counts[2], counts[40])
	}
	// 4 つの要求で、それぞれの一覧を 1 回ずつだけ読む（作業の一覧、作業の終わりの判定、apply request の一覧、人への依頼の一覧）。
	if counts[40]["ListApplyRequests"] != 3 || counts[40]["ListHumanRequests"] != 3 {
		t.Fatalf("1 回の要求の中で、同じ一覧を二度読んだ：%v", counts[40])
	}
	for _, name := range []string{"GetApplyRequest", "ListReviews", "GetHumanRequest", "SetWaiting"} {
		if counts[40][name] != 0 {
			t.Fatalf("一覧で %s を呼んだ：%v", name, counts[40])
		}
	}
}
