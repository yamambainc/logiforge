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

// UserPromptRevision は、user（WebUI）のレビューの、プロンプトのリビジョンである（design/api/endpoints/apply-request/review.md）。
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

// ReviewApplyRequest は、レビューを付ける。approve では、先に今の main で確かめ、通ったときだけ GitForge に approve を出す。
// merge はしない（design/backend/merge.md の「approve」）。
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
	// approve は、今の main で確かめ、通らなければ出さない。向き先が main でなければ、Logiforge は確かめない。
	if kind == ports.ReviewApprove && targetsMain(d.port) {
		f, err := b.checkOnCurrentMain(ctx, repo, ref, d)
		if err != nil {
			return out, err
		}
		if f != nil {
			return out, b.refuse(ctx, ref, id, *f)
		}
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
	// 向き先が main でない apply request は、Logiforge の反映の対象でない（design/backend/apply-request/target.md の「向き先」）。
	if kind == ports.ReviewApprove && !targetsMain(d.port) {
		warnings = append(warnings, "向き先が main でない（"+d.port.Target+"）ので、Logiforge は確かめていない")
	}
	ar, err := b.GetApplyRequest(ctx, c, repo, id)
	if err != nil {
		return out, err
	}
	ar.Warnings = append(ar.Warnings, warnings...)
	out.Value = ReviewResult{ApplyRequest: ar, Review: reviewOf(r, true)}
	return out, nil
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

// checkOnCurrentMain は、今の main での確かめを行う（design/backend/merge.md の「approve と merge の前に確かめること」）。
// approve の前と、merge の前に使う。通らなければ、理由を返す（GitForge には書かない）。
func (b *Backend) checkOnCurrentMain(ctx context.Context, repo, ref string, d *arDetail) (*ports.MergeFailure, error) {
	failure := func(reason, detail string) (*ports.MergeFailure, error) {
		return &ports.MergeFailure{Revision: d.current.Commit, Reason: reason, Detail: detail}, nil
	}
	// 2. 今の main のスナップショット。GitForge の main が latest より進んでいれば、worker が追いつくのを待つ。
	// 待ち始めに読んだ head が、latest の history に入ったら抜ける（そのあとに main が進んでもよい）。
	if head, err := b.Forge.MainHead(ctx, ref); err == nil {
		if !b.waitLatest(ctx, repo, func(l string) bool { return b.caughtUp(ctx, repo, ref, l, head) }) {
			return failure("snapshot_pending", "GitForge の main "+shortSHA(head)+" に、worker がまだ追いついていない。少し待って送り直す")
		}
	} else if !errors.Is(err, ports.ErrNotFound) {
		return nil, err
	}
	latest, err := b.currentMain(ctx, repo)
	if err != nil {
		if e := AsError(err); e != nil && e.Code == CodeSnapshotPending {
			return failure("snapshot_pending", "今の main のスナップショットが、まだない。少し待って送り直す")
		}
		return nil, err
	}
	// 3. 対象ノードの変化、重ねられるか、差分の検査。
	reason, detail, err := b.changeProblem(ctx, latest, d.branch, d.model.Operations)
	if err != nil || reason == "" {
		return nil, err
	}
	return failure(reason, detail)
}

// refuse は、確かめに通らなかった approve か merge を断るエラーを返す。エラーの code は、理由である。
// opener が直すべき理由なら、pull request のふつうのコメントに理由を書き、修正待ちのラベルに付け替える
// （design/adapters/pull-requests.md の「レビュー待ちと修正待ちのラベル」）。ほかの理由では、GitForge に書かない。
func (b *Backend) refuse(ctx context.Context, ref, id string, f ports.MergeFailure) error {
	e := &Error{Code: Code(f.Reason), Detail: f.Detail}
	if openerFixes[f.Reason] {
		if err := b.Forge.RecordMergeFailure(ctx, ref, id, f, ports.WaitingFix); err != nil {
			e.Detail += "（pull request に理由を書けなかった：" + err.Error() + "）"
		}
	}
	return e
}

// MergeApplyRequest は、apply request を、呼んだ主体のトークンで squash merge する（design/backend/merge.md の「merge」）。
// 直前に、反映の条件を、今の main で確かめ直す。反映のコミットが読む側に出るのを待ち、上限を過ぎたら、そのコミットを Pending に返す。
func (b *Backend) MergeApplyRequest(ctx context.Context, c Caller, repo, id string) (Written[ApplyRequest], error) {
	var out Written[ApplyRequest]
	// review と同じく、client と、その repository に Write がある user が使える（design/settings/users.md の「WebUI からのレビュー」）。
	if c.Principal.Kind == ports.PrincipalUser {
		if err := b.requireUserOn(ctx, c, repo); err != nil {
			return out, err
		}
	} else if err := requireClient(c); err != nil {
		return out, err
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return out, err
	}
	if err := b.notMigrated(ctx, repo); err != nil {
		return out, err
	}
	d, err := b.openApplyRequest(ctx, repo, ref, id)
	if e := AsError(err); e != nil && e.Code == CodeNotOpen {
		// 送り直し：もう反映されていれば、成功として今の状態を返す。
		if p, err := b.Forge.GetApplyRequest(ctx, ref, id); err == nil && p.State == ports.StateMerged {
			out.Value, err = b.GetApplyRequest(ctx, c, repo, id)
			return out, err
		}
	}
	if err != nil {
		return out, err
	}
	if !targetsMain(d.port) {
		return out, fail(CodeValidationFailed, "向き先が main でない（%s）ので、Logiforge では merge しない。GitForge で merge する", d.port.Target)
	}
	// 1. 反映の条件。
	if d.branch == nil {
		return out, fail(CodeSnapshotPending, "先頭のコミット %s のスナップショットが、まだない。少し待って送り直す", shortSHA(d.current.Commit))
	}
	if p := approvalProblem(d); p != "" {
		return out, fail(CodeNotApproved, "%s。先頭のコミットを approve してもらってから merge する", p)
	}
	// 2 と 3. 今の main での確かめ。
	f, err := b.checkOnCurrentMain(ctx, repo, ref, d)
	if err != nil {
		return out, err
	}
	if f != nil {
		return out, b.refuse(ctx, ref, id, *f)
	}
	// 確かめるあいだに worker を待つことがあるので、その間に出た request_changes を見落とさないよう、review を読み直す。
	if d.reviews, err = b.Forge.ListReviews(ctx, ref, id); err != nil {
		return out, err
	}
	if p := approvalProblem(d); p != "" {
		return out, fail(CodeNotApproved, "%s。先頭のコミットを approve してもらってから merge する", p)
	}
	sha, err := b.squash(ctx, ref, id, d)
	if err != nil {
		return out, err
	}
	// 5. 反映のコミットが読む側に出るまで待つ。
	if !b.waitLatest(ctx, repo, func(l string) bool { return l == sha }) {
		out.Pending = sha
	}
	out.Value, err = b.GetApplyRequest(ctx, c, repo, id)
	return out, err
}

// approvalProblem は、反映の手順の手順 1 のうち、レビューとセッションの条件を確かめ、満たさなければ理由を返す。
//   - すべての先頭のコミットに対するレビューのうち、最後の決めるレビューが、今の先頭のコミットへの approve である
//     （design/backend/merge.md。先頭を前のコミットに戻しても、そのあとの request_changes を見落とさないため）。
func approvalProblem(d *arDetail) string {
	last := lastDecisive(d.reviews.Items, "")
	if last == nil || last.Kind != ports.ReviewApprove || last.Revision != d.current.Commit {
		return "最後の決めるレビューが、先頭のコミット " + shortSHA(d.current.Commit) + " への approve でない"
	}
	return ""
}

// changeProblem は、反映の手順の手順 3 を、onto を今の main として行い、満たさなければ失敗の理由と詳しい説明を返す。
func (b *Backend) changeProblem(ctx context.Context, onto *view, br *snapshot.Branch, ops []Operation) (string, string, error) {
	// ディレクトリの repository で、差分がディレクトリの外にも及ぶ apply request は、反映しない（design/directory/README.md）。
	if len(br.Outside) > 0 {
		return "invariant_violation", fmt.Sprintf("%s（ディレクトリの外）：%s", tree.RuleOutsideDirectory, strings.Join(br.Outside, "、")), nil
	}
	if reason, detail, err := b.staleProblem(ctx, onto, br.Diff.Base, ops); err != nil || reason != "" {
		return reason, detail, err
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

// staleProblem は、対象ノードと引用した部分木が、ブランチが main から分かれたコミット base のあとに変わっていないかを確かめる
// （design/backend/merge.md の「対象ノードの変化」）。変わっていれば stale_target と、利用者が読む説明を返す。
//   - 今の main にないノードは、同じ apply request で加えるノードなので、変わったとみなさない。先頭のコミットの木にあるかは、差分の検査で確かめる。
//   - base が今の main の並びになければ、確かめられないので、変わったものとみなす。
func (b *Backend) staleProblem(ctx context.Context, onto *view, base string, ops []Operation) (string, string, error) {
	nodes := onto.main.SnapshotTree.Nodes
	type check struct {
		label string
		last  snapshot.Rev
	}
	var checks []check
	for _, t := range targetsOf(ops) {
		if n := nodes[t]; n != nil {
			checks = append(checks, check{"対象ノード " + t, n.LastChanged})
		}
	}
	for _, op := range ops {
		for _, ci := range op.Citations {
			if ci.TargetKind != "subtree" {
				continue
			}
			if last, ok := onto.main.SnapshotTree.SubtreeLastChanged(ci.NodeID); ok {
				checks = append(checks, check{"引用した部分木 " + ci.NodeID, last})
			}
		}
	}
	if len(checks) == 0 {
		return "", "", nil
	}
	baseNum, err := b.mainNumber(ctx, onto, base)
	if err != nil {
		return "", "", err
	}
	if baseNum == 0 {
		return "stale_target", fmt.Sprintf("このブランチが main から分かれたコミット %s が、今の main の履歴にない（main が書き換えられたかもしれない）。"+
			"ブランチを今の main に合わせて（rebase か merge で）push し、レビューを受け直す", shortSHA(base)), nil
	}
	for _, c := range checks {
		if c.last.Number <= baseNum {
			continue
		}
		// どの apply request の反映かは、説明を補うだけなので、読めなければ書かない。
		by := "main のコミット " + shortSHA(c.last.Revision) + " "
		if m, err := b.Snap.Commit(ctx, onto.repo, c.last.Revision); err == nil && m.ApplyRequest != "" {
			by = "main のコミット " + shortSHA(c.last.Revision) + "（apply request #" + m.ApplyRequest + " の反映）"
		}
		return "stale_target", fmt.Sprintf("%s は、このブランチが main から分かれたコミット %s のあとに、%sで変わった。"+
			"ブランチを今の main に合わせて（rebase か merge で）push し、レビューを受け直す", c.label, shortSHA(base), by), nil
	}
	return "", "", nil
}

// shortSHA は、利用者が読む文に書く、コミットの SHA の先頭 7 文字を返す。
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// squash は、反映の手順の 4 である。メッセージに、機械が読む trailer は書かない。どのコミットがどの apply request の反映かは、
// worker が、merge の pull request を探して、コミットに置く（design/backend/invalid-main.md の「どの apply request の反映か」）。
func (b *Backend) squash(ctx context.Context, ref, id string, d *arDetail) (string, error) {
	// head の SHA を指定して squash merge する。
	title := fmt.Sprintf("apply request #%s を反映した", id)
	sha, err := b.Forge.Merge(ctx, ref, id, d.current.Commit, title, "")
	switch {
	case errors.Is(err, ports.ErrHeadMoved):
		return "", fail(CodeCommitConflict, "merge の前に、新しいコミットが push された。新しい先頭のコミットを、レビューからやり直す")
	case errors.Is(err, ports.ErrConflict):
		return "", b.refuse(ctx, ref, id, ports.MergeFailure{Revision: d.current.Commit, Reason: "conflict",
			Detail: "git の衝突で merge できない：" + err.Error() + "。ブランチを今の main に合わせて（rebase か merge で）push し、レビューを受け直す"})
	case errors.Is(err, ports.ErrMergeForbidden):
		// 呼んだ主体のトークンで merge するので、その主体に merge の権限がなければ、GitForge が断る（design/backend/merge.md の「merge」）。
		return "", fail(CodeMergeForbidden, "GitForge が、この主体の merge を断った（merge の権限がない）：%v", err)
	case err != nil:
		return "", err
	}
	return sha, nil
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
