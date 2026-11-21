package backend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/tree"
)

// UserPromptRevision は、user（WebUI）のレビューの記録に書く、プロンプトのリビジョンである（design/api/endpoints/apply-request/review.md）。
const UserPromptRevision = "-"

// NewReview は、レビューの要求である。
type NewReview struct {
	Kind           string
	Commit         string
	Body           string
	PromptRevision string
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
	// client と、その repository に Write がある user が使える（design/settings/users.md の「WebUI からのレビュー」）。
	if c.Principal.Kind == ports.PrincipalUser {
		if err := b.requireUserOn(ctx, c, repo); err != nil {
			return out, err
		}
		// user（WebUI）のレビューには、プロンプトのリビジョンがない。記録には - を書く。
		n.PromptRevision = UserPromptRevision
	} else if err := requireClient(c); err != nil {
		return out, err
	}
	kind := ports.ReviewKind(n.Kind)
	if kind != ports.ReviewApprove && kind != ports.ReviewRequestChanges && kind != ports.ReviewComment {
		return out, fail(CodeValidationFailed, "kind は approve、request_changes、comment のどれか")
	}
	// GitForge は、本文のない comment と request changes の review を拒む（GitHub と Forgejo）。
	if (kind == ports.ReviewComment || kind == ports.ReviewRequestChanges) && strings.TrimSpace(n.Body) == "" {
		return out, fail(CodeValidationFailed, "comment と request_changes には body（本文）が要る")
	}
	if kind == ports.ReviewApprove && n.PromptRevision == "" {
		return out, fail(CodeValidationFailed, "approve では prompt_revision が必須")
	}
	if n.Commit == "" {
		return out, fail(CodeValidationFailed, "commit（レビューしたコミットの SHA）がない")
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return out, err
	}
	// 移した元の repository は、読むことだけができる（design/directory/migrate.md の「元の repository」）。
	if err := b.notMigrated(ctx, repo); err != nil {
		return out, err
	}
	d, err := b.openApplyRequest(ctx, repo, ref, id)
	if e := AsError(err); e != nil && e.Code == CodeNotOpen && kind == ports.ReviewApprove {
		// approve の送り直し：反映済みで、最後の決めるレビューが、同じセッションの approve なら、成功として今の状態を返す。
		if res, ok, err := b.approveResent(ctx, c, repo, ref, id); err != nil || ok {
			out.Value = res
			return out, err
		}
	}
	if err != nil {
		return out, err
	}
	// GitForge は、pull request の作者自身の approve を拒む（design/adapters/pull-requests.md の「数える review」）。
	if kind == ports.ReviewApprove && strings.EqualFold(c.Principal.Login, d.port.Opener) {
		return out, fail(CodeValidationFailed, "apply request を作ったアカウント %s では approve できない。opener と別のアカウントでレビューする", d.port.Opener)
	}
	rev := n.Commit
	if rev != d.current.Commit {
		return out, fail(CodeCommitConflict, "レビューしたコミットが、ブランチの先頭のコミット %s と違う", d.current.Commit)
	}
	// 先頭のコミットが GitForge にはあるが、worker がまだスナップショットにしていない。書いたセッションも差分もわからないので、待ってもらう。
	if d.branch == nil {
		return out, fail(CodeSnapshotPending, "先頭のコミット %s のスナップショットが、まだない。少し待って送り直す", d.current.Commit)
	}
	r, err := b.Forge.AddReview(ctx, ref, id, ports.Review{Kind: kind, Body: n.Body, Revision: rev,
		Reviewer: c.Principal.Login, PromptRevision: n.PromptRevision})
	if err != nil {
		return out, err
	}
	var warnings []string
	// request changes を出したら、修正待ちのラベルに付け替える（design/adapters/pull-requests.md の「レビュー待ちと修正待ちのラベル」）。
	if kind == ports.ReviewRequestChanges && targetsMain(d.port) {
		if err := b.Forge.SetWaiting(ctx, ref, id, ports.WaitingFix); err != nil {
			warnings = append(warnings, "修正待ちのラベルを付けられない（書く権限のある人が読めば直る）："+err.Error())
		}
	}
	// 向き先が main でない apply request は、Logiforge の反映の対象でない。approve は記録するだけで、反映の手順に進まない
	// （design/backend/apply-request/target.md の「向き先」）。
	if kind == ports.ReviewApprove && !targetsMain(d.port) {
		warnings = append(warnings, "向き先が main でない（"+d.port.Target+"）ので、approve しても Logiforge は反映しない")
	} else if kind == ports.ReviewApprove {
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
	out.Value = ReviewResult{ApplyRequest: ar, Review: reviewOf(r, true)}
	return out, nil
}

// approveResent は、反映済みの apply request への approve が、送り直しかを確かめる。送り直しなら、今の状態を返す。
func (b *Backend) approveResent(ctx context.Context, c Caller, repo, ref, id string) (ReviewResult, bool, error) {
	ar, err := b.Forge.GetApplyRequest(ctx, ref, id)
	if err != nil || ar.State != ports.StateMerged {
		return ReviewResult{}, false, nil
	}
	reviews, err := b.Forge.ListReviews(ctx, ref, id)
	if err != nil {
		return ReviewResult{}, false, err
	}
	last := lastDecisive(reviews.Items, "")
	if last == nil || last.Kind != ports.ReviewApprove || !strings.EqualFold(last.Reviewer, c.Principal.Login) {
		return ReviewResult{}, false, nil
	}
	model, err := b.GetApplyRequest(ctx, c, repo, id)
	if err != nil {
		return ReviewResult{}, false, err
	}
	return ReviewResult{ApplyRequest: model, Review: reviewOf(*last, false)}, true, nil
}

// lastDecisive は、決めるレビュー（approve か request_changes）の最後のものを返す。revision が空でなければ、そのコミットへのものだけを見る。
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
// 反映のコミットが読む側に出るのを待ち、上限を過ぎたら、そのコミットを返す。
func (b *Backend) merge(ctx context.Context, repo, ref, id string) (string, error) {
	d, err := b.openApplyRequest(ctx, repo, ref, id)
	if err != nil {
		return "", err
	}
	failure := func(reason, detail string) (string, error) {
		return "", b.recordFailure(ctx, ref, id, ports.MergeFailure{Revision: d.current.Commit, Reason: reason, Detail: detail})
	}
	// 1. 反映の条件。
	if d.branch == nil {
		return failure("snapshot_pending", "先頭のコミットのスナップショットが、まだない")
	}
	if approvalProblem(d) != "" {
		return "", nil
	}
	// 2. 今の main のスナップショット。GitForge の main が latest より進んでいれば、worker が追いつくのを待つ。
	// 待ち始めに読んだ head が、latest の history に入ったら抜ける（そのあとに main が進んでもよい）。
	if head, err := b.Forge.MainHead(ctx, ref); err == nil {
		if !b.waitLatest(ctx, repo, func(l string) bool { return b.caughtUp(ctx, repo, ref, l, head) }) {
			return failure("snapshot_pending", "GitForge の main "+head+" に、worker がまだ追いついていない")
		}
	} else if !errors.Is(err, ports.ErrNotFound) {
		return "", err
	}
	latest, err := b.currentMain(ctx, repo)
	if err != nil {
		if e := AsError(err); e != nil && e.Code == CodeSnapshotPending {
			return failure("snapshot_pending", "今の main のスナップショットが、まだない")
		}
		return "", err
	}
	// 壊れた repository には、直すための apply request しか反映しない（design/backend/broken.md）。
	broken, err := b.Snap.Broken(ctx, repo)
	if err != nil {
		return "", err
	}
	kind := d.port.Kind
	if kind != "" {
		problem, err := b.repairProblem(ctx, repo, kind, d.branch, latest, broken)
		if err != nil {
			return "", err
		}
		if problem != "" {
			return failure("repository_broken", problem)
		}
		return b.squash(ctx, repo, ref, id, d)
	}
	if broken != nil {
		return failure("repository_broken", fmt.Sprintf("repository が壊れている（%s から）。直すための apply request を先に反映する", broken.First))
	}
	// 3. 対象ノードの変化、重ねられるか、差分の検査。
	if reason, detail, err := b.changeProblem(ctx, latest, d.branch, d.model.Operations); err != nil || reason != "" {
		if err != nil {
			return "", err
		}
		return failure(reason, detail)
	}
	return b.squash(ctx, repo, ref, id, d)
}

// recordFailure は、反映の失敗を GitForge に残す（head の commit status と、pull request のコメント）。
// opener が直すべき理由なら修正待ちに、ほかの理由ならレビュー待ちに、ラベルを付け替える
// （design/adapters/pull-requests.md の「レビュー待ちと修正待ちのラベル」）。
func (b *Backend) recordFailure(ctx context.Context, ref, id string, f ports.MergeFailure) error {
	if err := b.Forge.RecordMergeFailure(ctx, ref, id, f); err != nil {
		return err
	}
	if openerFixes[f.Reason] {
		return b.Forge.SetWaiting(ctx, ref, id, ports.WaitingFix)
	}
	return b.Forge.SetWaiting(ctx, ref, id, ports.WaitingReview)
}

// approvalProblem は、反映の手順の手順 1 のうち、レビューとセッションの条件を確かめ、満たさなければ理由を返す。
//   - すべての先頭のコミットに対するレビューのうち、最後の決めるレビューが、今の先頭のコミットへの approve である
//     （design/backend/verify/README.md。先頭を前のコミットに戻しても、そのあとの request_changes を見落とさないため）。
func approvalProblem(d *arDetail) string {
	last := lastDecisive(d.reviews.Items, "")
	if last == nil || last.Kind != ports.ReviewApprove || last.Revision != d.current.Commit {
		return "最後の決めるレビューが、先頭のコミット " + d.current.Commit + " への approve でない"
	}
	return ""
}

// changeProblem は、反映の手順の手順 3 を、onto を今の main として行い、満たさなければ失敗の理由と詳しい説明を返す。
func (b *Backend) changeProblem(ctx context.Context, onto *view, br *snapshot.Branch, ops []Operation) (string, string, error) {
	// ディレクトリの repository で、差分がディレクトリの外にも及ぶ apply request は、反映しない（design/directory/README.md）。
	if len(br.Outside) > 0 {
		return "invariant_violation", fmt.Sprintf("%s（ディレクトリの外）：%s", tree.RuleOutsideDirectory, strings.Join(br.Outside, "、")), nil
	}
	baseNum, err := b.mainNumber(ctx, onto, br.Diff.Base)
	if err != nil {
		return "", "", err
	}
	for _, t := range targetsOf(ops) {
		n := onto.main.SnapshotTree.Nodes[t]
		if n == nil || baseNum == 0 || n.LastChanged.Number > baseNum {
			return "stale_target", fmt.Sprintf("対象ノード %s が、base のあとに変わった", t), nil
		}
	}
	// 引用した部分木が、base のあとに変わっていれば、それも stale_target にする（design/backend/merge.md の「対象ノードの変化」）。
	// 今の main にないノードは、同じ apply request で加えるノードなので、変わったとみなさない。先頭のコミットの木にあるかは、下の検査で確かめる。
	for _, op := range ops {
		for _, ci := range op.Citations {
			if ci.TargetKind != "subtree" || onto.main.SnapshotTree.Nodes[ci.NodeID] == nil {
				continue
			}
			if onto.main.SnapshotTree.SubtreeChangedAfter(ci.NodeID, baseNum) {
				return "stale_target", fmt.Sprintf("引用した部分木 %s が、base のあとに変わった", ci.NodeID), nil
			}
		}
	}
	res, conflicts, err := b.checkVersion(ctx, onto, onto, br)
	if err != nil {
		return "", "", err
	}
	if len(conflicts) > 0 {
		return "conflict", "今の main に重ねられない：" + strings.Join(conflicts, "、"), nil
	}
	if !res.OK() {
		return "invariant_violation", violations(res.Rejected), nil
	}
	return "", "", nil
}

// squash は、反映の手順の 4 と 5 である。メッセージに、機械が読む trailer は書かない。どのコミットがどの apply request の反映かは、
// worker の反映かの判定の結果が正本である（design/files/revisions.md）。
func (b *Backend) squash(ctx context.Context, repo, ref, id string, d *arDetail) (string, error) {
	failure := func(reason, detail string) (string, error) {
		return "", b.recordFailure(ctx, ref, id, ports.MergeFailure{Revision: d.current.Commit, Reason: reason, Detail: detail})
	}
	// 4. head の SHA を指定して squash merge する。
	title := fmt.Sprintf("apply request #%s を反映した", id)
	message := ""
	sha, err := b.Forge.Merge(ctx, ref, id, d.current.Commit, title, message)
	switch {
	case errors.Is(err, ports.ErrHeadMoved):
		return "", fail(CodeCommitConflict, "反映の前に、新しいコミットが push された。新しい先頭のコミットを、レビューからやり直す")
	case errors.Is(err, ports.ErrConflict):
		return failure("conflict", err.Error())
	case errors.Is(err, ports.ErrMergeForbidden):
		// reviewer のトークンで merge するので、reviewer に merge の権限がなければ、GitForge が断る（design/backend/merge.md の手順 4）。
		// apply request は open のまま残る。merge の権限のある reviewer が approve し直せば、反映される。
		return failure("merge_forbidden", "approve した reviewer に、GitForge で merge する権限がない："+err.Error())
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

// inHistory は、latest までの main の並びに、sha があるかを返す。
func (b *Backend) inHistory(ctx context.Context, repo, latest, sha string) bool {
	if latest == sha {
		return true
	}
	m, err := b.Snap.Main(ctx, repo, latest)
	if err != nil {
		return false
	}
	n, err := b.mainNumber(ctx, &view{repo: repo, main: m}, sha)
	return err == nil && n > 0
}
