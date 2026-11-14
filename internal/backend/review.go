package backend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ids"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
)

// chainBrokenDetail は、レビューの鎖が切れているときの、opener の手である（design/adapters/pull-requests.md の「レビューの書き方」）。
const chainBrokenDetail = "レビューの鎖が切れている（数えないレビューがある）ので、この apply request には、レビューも反映もできない。" +
	"opener は、この apply request を close し、同じブランチから apply request を作り直す"

// NewReview は、レビューの要求である。
type NewReview struct {
	Kind           string
	Revision       string
	Version        int
	Body           string
	PromptRevision string
	Session        string
}

// ReviewResult は、レビューの応答である。
type ReviewResult struct {
	Review       Review       `json:"review"`
	ApplyRequest ApplyRequest `json:"apply_request"`
}

// ReviewApplyRequest は、レビューを付ける。approve で反映の条件を満たしたら、この要求の中で反映する（design/backend/merge.md）。
// 反映に失敗しても、エラーにはしない。応答の apply_request.merge_failures に理由が入る。
func (b *Backend) ReviewApplyRequest(ctx context.Context, c Caller, repo, id string, n NewReview) (Written[ReviewResult], error) {
	var out Written[ReviewResult]
	if err := requireClient(c, n.Session); err != nil {
		return out, err
	}
	kind := ports.ReviewKind(n.Kind)
	if kind != ports.ReviewApprove && kind != ports.ReviewRequestChanges && kind != ports.ReviewComment {
		return out, fail(CodeValidationFailed, "kind は approve、request_changes、comment のどれか")
	}
	if kind == ports.ReviewApprove && n.PromptRevision == "" {
		return out, fail(CodeValidationFailed, "approve では prompt_revision が必須")
	}
	if (n.Revision == "") == (n.Version == 0) {
		return out, fail(CodeValidationFailed, "revision と version のどちらか一方を送る")
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return out, err
	}
	d, err := b.openApplyRequest(ctx, repo, ref, id)
	if e := AsError(err); e != nil && e.Code == CodeNotOpen && kind == ports.ReviewApprove {
		// approve の送り直し：反映済みで、最後の決めるレビューが、同じセッションの approve なら、成功として今の状態を返す。
		if res, ok, err := b.approveResent(ctx, c, repo, ref, id, n.Session); err != nil || ok {
			out.Value = res
			return out, err
		}
	}
	if err != nil {
		return out, err
	}
	if d.reviews.ChainBroken {
		return out, fail(CodeNotOpen, chainBrokenDetail)
	}
	// 版の正体は SHA である。番号は、SHA に引き直す。
	rev := n.Revision
	if n.Version != 0 {
		for _, v := range d.model.Versions {
			if v.Version == n.Version {
				rev = v.Revision
			}
		}
	}
	if rev != d.current.Revision {
		return out, fail(CodeVersionConflict, "レビューした版が、今の版（%d：%s）と違う", d.current.Version, d.current.Revision)
	}
	// 今の版が GitForge にはあるが、worker がまだスナップショットにしていない。版を書いたセッションも差分もわからないので、待ってもらう。
	if d.branch == nil {
		return out, fail(CodeSnapshotPending, "今の版（%d：%s）のスナップショットが、まだない。少し待って送り直す", d.current.Version, d.current.Revision)
	}
	// 差分を書いたどのセッションとも、別のセッションでレビューする。
	for _, v := range d.model.Versions {
		if v.AuthorSessionID != nil && *v.AuthorSessionID == n.Session {
			return out, fail(CodeSameSession, "版 %d を書いたセッションと同じセッションで、レビューした", v.Version)
		}
	}
	if kind == ports.ReviewApprove && d.model.Versions[len(d.model.Versions)-1].AuthorSessionID == nil {
		return out, fail(CodeValidationFailed, "今の版のコミットに Logiforge-Session-Id: がないので、approve できない（design/files/revisions.md の「セッション」）")
	}
	r := ports.Review{ID: ids.NewULID(ids.PrefixReview, b.now()), Kind: kind, Body: n.Body, Revision: rev,
		Reviewer: c.Principal.Login, Session: n.Session, PromptRevision: n.PromptRevision}
	if err := b.Forge.AddReview(ctx, ref, id, r); err != nil {
		return out, err
	}
	var warnings []string
	if c.Principal.Login == d.port.Opener {
		warnings = append(warnings, "reviewer の主体が、apply request を作った主体と同じ（セッションは自己申告なので、確かめられない）")
	}
	if kind == ports.ReviewApprove {
		pending, err := b.merge(ctx, repo, ref, id)
		if err != nil {
			return out, err
		}
		out.Pending = pending
	}
	ar, err := b.GetApplyRequest(ctx, c, repo, id)
	if err != nil {
		return out, err
	}
	ar.Warnings = append(ar.Warnings, warnings...)
	out.Value = ReviewResult{ApplyRequest: ar, Review: Review{ID: r.ID, Kind: string(kind), Version: versionOf(ar.Versions, rev), Revision: rev,
		Body: r.Body, ReviewerSessionID: r.Session, PromptRevision: r.PromptRevision, Valid: true}}
	return out, nil
}

// approveResent は、反映済みの提案への approve が、送り直しかを確かめる。送り直しなら、今の状態を返す。
func (b *Backend) approveResent(ctx context.Context, c Caller, repo, ref, id, session string) (ReviewResult, bool, error) {
	ar, err := b.Forge.GetApplyRequest(ctx, ref, id)
	if err != nil || ar.State != ports.StateMerged {
		return ReviewResult{}, false, nil
	}
	reviews, err := b.Forge.ListReviews(ctx, ref, id)
	if err != nil {
		return ReviewResult{}, false, err
	}
	last := lastDecisive(reviews.Items, "")
	if last == nil || last.Kind != ports.ReviewApprove || last.Session != session {
		return ReviewResult{}, false, nil
	}
	model, err := b.GetApplyRequest(ctx, c, repo, id)
	if err != nil {
		return ReviewResult{}, false, err
	}
	return ReviewResult{ApplyRequest: model, Review: Review{ID: last.ID, Kind: string(last.Kind), Version: versionOf(model.Versions, last.Revision),
		Revision: last.Revision, Body: last.Body, ReviewerSessionID: last.Session, PromptRevision: last.PromptRevision}}, true, nil
}

// lastDecisive は、決めるレビュー（approve か request_changes）の最後のものを返す。revision が空でなければ、その版へのものだけを見る。
func lastDecisive(reviews []ports.Review, revision string) *ports.Review {
	var last *ports.Review
	for i, r := range reviews {
		if (revision == "" || r.Revision == revision) && (r.Kind == ports.ReviewApprove || r.Kind == ports.ReviewRequestChanges) {
			last = &reviews[i]
		}
	}
	return last
}

// merge は、反映の手順を行う（design/backend/merge.md の「手順」）。反映できなければ、失敗の理由を GitForge に記録し、エラーにしない。
// 反映のコミットが読む側に出るのを待ち、上限を過ぎたら、そのリビジョンを返す。
func (b *Backend) merge(ctx context.Context, repo, ref, id string) (string, error) {
	d, err := b.openApplyRequest(ctx, repo, ref, id)
	if err != nil {
		return "", err
	}
	failure := func(reason, detail string) (string, error) {
		return "", b.Forge.RecordMergeFailure(ctx, ref, id, ports.MergeFailure{Revision: d.current.Revision, Reason: reason, Detail: detail})
	}
	// 1. 反映の条件。
	if d.branch == nil {
		return failure("snapshot_pending", "今の版のスナップショットが、まだない")
	}
	if d.reviews.ChainBroken {
		return "", fail(CodeNotOpen, chainBrokenDetail)
	}
	last := lastDecisive(d.reviews.Items, d.current.Revision)
	if last == nil || last.Kind != ports.ReviewApprove {
		return "", nil
	}
	author := d.model.Versions[len(d.model.Versions)-1].AuthorSessionID
	if author == nil {
		return "", nil
	}
	for _, v := range d.model.Versions {
		if v.AuthorSessionID != nil && *v.AuthorSessionID == last.Session {
			return "", nil
		}
	}
	// 2. 今の main のスナップショット。GitForge の main が latest より進んでいれば、worker が追いつくのを待つ。
	// 待ち始めに読んだ head が、latest の history に入ったら抜ける（そのあとに main が進んでもよい）。
	if head, err := b.Forge.MainHead(ctx, ref); err == nil {
		if !b.waitLatest(ctx, repo, func(l string) bool { return b.inHistory(ctx, repo, l, head) }) {
			return failure("snapshot_pending", "GitForge の main "+head+" に、worker がまだ追いついていない")
		}
	} else if !errors.Is(err, ports.ErrNotFound) {
		return "", err
	}
	latest, err := b.mainAt(ctx, repo, "")
	if err != nil {
		if e := AsError(err); e != nil && e.Code == CodeSnapshotPending {
			return failure("snapshot_pending", "今の main のスナップショットが、まだない")
		}
		return "", err
	}
	// 3. 対象ノードの変化、重ねられるか、差分の検査。
	baseNum := latest.main.Tree.HistoryNumber(d.branch.Diff.Base)
	for _, t := range targetsOf(d.model.Operations) {
		n := latest.main.Tree.Nodes[t]
		if n == nil || baseNum == 0 {
			return failure("stale_target", fmt.Sprintf("対象ノード %s が、base のあとに変わった", t))
		}
		if slices.ContainsFunc(n.Changes, func(r snapshot.Rev) bool { return r.Number > baseNum }) {
			return failure("stale_target", fmt.Sprintf("対象ノード %s が、base のあとに変わった", t))
		}
	}
	// 引用した部分木が、base のあとに変わっていれば、それも stale_target にする（design/backend/merge.md の「対象ノードの変化」）。
	for _, op := range d.model.Operations {
		for _, ci := range op.Citations {
			if ci.TargetKind == "subtree" && latest.main.Tree.SubtreeChangedAfter(ci.NodeID, baseNum) {
				return failure("stale_target", fmt.Sprintf("引用した部分木 %s が、base のあとに変わった", ci.NodeID))
			}
		}
	}
	res, conflicts, err := b.checkVersion(ctx, latest, latest, d.branch)
	if err != nil {
		return "", err
	}
	if len(conflicts) > 0 {
		return failure("conflict", "今の main に重ねられない："+strings.Join(conflicts, "、"))
	}
	if !res.OK() {
		return failure("invariant_violation", violations(res.Rejected))
	}
	// 4. head の SHA を指定して squash merge する。
	title := fmt.Sprintf("apply request %s を反映した", id)
	// 反映したすべての版のセッションを、重なりを除いて並べる（design/files/revisions.md の「記録の形式」）。
	message := fmt.Sprintf("Logiforge-Apply-Request: %s\nLogiforge-Version: %d %s", id, d.current.Version, d.current.Revision)
	var sessions []string
	for _, v := range d.model.Versions {
		if v.AuthorSessionID != nil && !slices.Contains(sessions, *v.AuthorSessionID) {
			sessions = append(sessions, *v.AuthorSessionID)
			message += "\nLogiforge-Session-Id: " + *v.AuthorSessionID
		}
	}
	sha, err := b.Forge.Merge(ctx, ref, id, d.current.Revision, title, message)
	switch {
	case errors.Is(err, ports.ErrHeadMoved):
		return "", fail(CodeVersionConflict, "反映の前に、新しい版が push された。新しい版を、レビューからやり直す")
	case errors.Is(err, ports.ErrConflict):
		return failure("conflict", err.Error())
	case err != nil:
		return "", err
	}
	// 5. 反映のコミットが読む側に出るまで待つ。
	if !b.waitLatest(ctx, repo, func(l string) bool { return l == sha }) {
		return sha, nil
	}
	return "", nil
}

// targetsOf は、操作の対象ノードを返す（design/backend/merge.md の「対象ノードの変化」）。
func targetsOf(ops []Operation) []string {
	var out []string
	for _, op := range ops {
		var t string
		switch op.Kind {
		case "story.create":
			t = op.ParentHypothesisID
		case "story.inactivate":
			t = op.StoryID
		case "verdict.submit", "node.edit":
			t = op.HypothesisID
		}
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// inHistory は、latest の main の history に、sha があるかを返す。
func (b *Backend) inHistory(ctx context.Context, repo, latest, sha string) bool {
	if latest == sha {
		return true
	}
	m, err := b.Snap.Main(ctx, repo, latest)
	return err == nil && m.Tree.HistoryNumber(sha) > 0
}
