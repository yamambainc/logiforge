package backend

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// apply_request を付けた読みは、open の apply request の差分を、今の main に重ねた、反映したあとの木を返す
// （design/backend/snapshot.md の「向き先に重ねる」）。
func TestReadAfterApplyRequest(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	b, c := f.b, f.caller
	at := At{ApplyRequest: "apr_2"}

	nodes, err := b.ListNodes(ctx, c, "q", at, "", -1)
	if err != nil {
		t.Fatal(err)
	}
	if nodes.Commit != f.c2 {
		t.Fatalf("重ねた向き先のコミット：%s", nodes.Commit)
	}
	ids := []string{}
	for _, n := range nodes.Nodes {
		switch x := n.(type) {
		case *Hypothesis:
			ids = append(ids, x.ID)
		case *Story:
			ids = append(ids, x.ID)
		}
	}
	if !slices.Contains(ids, S2) || !slices.Contains(ids, H3) {
		t.Fatalf("反映したあとの木に、新しいノードがない：%v", ids)
	}
	// H2 は、子のストーリーができるので、葉でなくなる。
	h2, err := b.GetNode(ctx, c, "q", H2, at)
	if err != nil || h2.Node.(*Hypothesis).Leaf || len(h2.Children) != 1 {
		t.Fatalf("H2：%+v %v", h2, err)
	}
	if _, err := b.GetTree(ctx, c, "q", at); err != nil {
		t.Fatal(err)
	}
	// 今の main の木には、まだない。
	if _, err := b.GetNode(ctx, c, "q", H3, At{}); code(err) != CodeNotFound {
		t.Fatalf("今の main の H3：%v", err)
	}

	cases := []struct {
		name string
		at   At
		want Code
	}{
		{"ない apply request", At{ApplyRequest: "apr_none"}, CodeNotFound},
		{"open でない", At{ApplyRequest: "apr_1"}, CodeNotOpen},
		{"commit と同時", At{Commit: f.c1, ApplyRequest: "apr_2"}, CodeValidationFailed},
	}
	for _, cs := range cases {
		if _, err := b.GetTree(ctx, c, "q", cs.at); code(err) != cs.want {
			t.Fatalf("%s：%v", cs.name, err)
		}
	}
	if _, err := b.GetNode(ctx, c, "q", "h-0000000099", at); code(err) != CodeNotFound {
		t.Fatalf("反映したあとの木にないノード：%v", err)
	}

	// 先頭のコミットのスナップショットがまだなければ、snapshot_pending。
	f.forge.ar("apr_2").HeadRevision = f.c1[:39] + "f"
	if _, err := b.GetTree(ctx, c, "q", at); code(err) != CodeSnapshotPending {
		t.Fatalf("先頭のスナップショットがない：%v", err)
	}
	f.forge.ar("apr_2").HeadRevision = f.head

	// 重ねられなければ、overlay_conflict。今の main で、同じパスを違う中身で作る。
	f.r.git("checkout", "--quiet", "main")
	f.r.write(dH2+"/"+S2+"/story.md", tree.MarshalStory(tree.StoryContent{ID: S2, Title: "別のストーリー", Summary: "s"}))
	f.r.commit("手で足した")
	syncFixture(t, f)
	if _, err := b.GetTree(ctx, c, "q", at); code(err) != CodeOverlayConflict {
		t.Fatalf("衝突：%v", err)
	}
}

// 向き先が main でない apply request は、検査も反映もしない（design/backend/apply-request.md の「向き先」）。
func TestTargetNotMain(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	b, c := f.b, f.caller
	f.forge.ar("apr_2").Target = "develop"

	ar, err := b.GetApplyRequest(ctx, c, "q", "apr_2")
	if err != nil {
		t.Fatal(err)
	}
	if ar.Target != "develop" || len(ar.Operations) != 0 || ar.Awaiting != nil {
		t.Fatalf("向き先が main でない apply request：%+v", ar)
	}
	if _, err := b.GetReviewMaterial(ctx, c, "q", "apr_2"); code(err) != CodeOverlayUnsupported {
		t.Fatalf("レビューの材料：%v", err)
	}
	if _, err := b.GetTree(ctx, c, "q", At{ApplyRequest: "apr_2"}); code(err) != CodeOverlayUnsupported {
		t.Fatalf("反映したあとの木：%v", err)
	}
	w, err := b.ListWork(ctx, c, "q", WorkQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range w.Items {
		if it.SubjectID == "apr_2" {
			t.Fatalf("作業の一覧に出た：%+v", it)
		}
	}
	// 対象にしたノード（H2）も、押さえられていないので、未決の葉として作業に出る。
	if !slices.ContainsFunc(w.Items, func(it WorkItem) bool { return it.Kind == "undecided_leaf" && it.NodeID == H2 }) {
		t.Fatalf("H2 が作業に出ない：%+v", w.Items)
	}

	// approve しても反映しない。警告を付ける。
	res, err := approve(t, f, "apr_2")
	if err != nil {
		t.Fatal(err)
	}
	if res.Value.ApplyRequest.State != "open" || !slices.ContainsFunc(res.Value.ApplyRequest.Warnings, func(s string) bool { return strings.Contains(s, "反映しない") }) {
		t.Fatalf("approve：%+v", res.Value.ApplyRequest)
	}
	// 向き先を main に戻すと、レビュー待ちになる（approve し直すと反映する）。
	f.forge.ar("apr_2").Target = "main"
	ar, _ = b.GetApplyRequest(ctx, c, "q", "apr_2")
	if ar.Awaiting == nil || *ar.Awaiting != "review" {
		t.Fatalf("main に戻したあと：%+v", ar.Awaiting)
	}
	res, err = approve(t, f, "apr_2")
	if err != nil || res.Value.ApplyRequest.State != "merged" {
		t.Fatalf("approve し直し：%+v %v", res.Value.ApplyRequest, err)
	}
}

// レビューで snapshot_pending を返すときは、GitForge にレビューを記録していない（design/api/writes.md）。
// そのため、CLI が送り直しても、二重に記録しない。
func TestReviewSnapshotPendingWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	// worker が取り込んでいない、新しい先頭のコミットを push する。
	f.r.git("checkout", "--quiet", "b")
	f.r.write(dH2+"/"+S2+"/"+H3+"/background.md", []byte("足した\n"))
	head := f.r.commit("版 2\n\nLogiforge-Session-Id: s2")
	f.forge.ar("apr_2").HeadRevision = head
	before := len(f.forge.reviews["apr_2"].Items)
	for _, kind := range []string{"comment", "approve"} {
		_, err := f.b.ReviewApplyRequest(ctx, reviewerC, "q", "apr_2", NewReview{Kind: kind, Commit: head, Session: "s9", PromptRevision: "p"})
		if code(err) != CodeSnapshotPending {
			t.Fatalf("%s：%v", kind, err)
		}
	}
	if got := len(f.forge.reviews["apr_2"].Items); got != before {
		t.Fatalf("snapshot_pending なのに、レビューを記録した：%d → %d", before, got)
	}
}
