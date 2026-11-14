package backend

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// trailer の名前（design/files/revisions.md）。
const trailerSession = "Logiforge-Session-Id"

// arState は、提案の状態を決める。head のブランチが消えた open の提案は、closed として扱う（design/adapters/pull-requests.md の「提案の状態」）。
func arState(ar ports.ApplyRequest, h store.BranchHistory) ports.State {
	if ar.State == ports.StateOpen {
		if n := len(h.Entries); n > 0 && h.Entries[n-1].Deleted {
			return ports.StateClosed
		}
	}
	return ar.State
}

// clockSlack は、worker と GitForge の時計のずれとして見込む時間である。
const clockSlack = 5 * time.Minute

// headCommits は、ブランチの履歴から、提案のブランチに push された先頭のコミットの一覧を作る（design/backend/apply-request.md の「先頭のコミット」）。
//   - 提案を作ったときの先頭のコミットから後の、ブランチの先頭を、初めて見た順に並べる。
//   - 先頭が前に見た SHA に戻ったら（A → B → A）、一覧に足さない。
//
// GitForge の今の head が、まだブランチの履歴にない（worker が追いついていない）ときは、それを、base と時刻のない最後の項目として足す。
func headCommits(ar ports.ApplyRequest, h store.BranchHistory) []HeadCommit {
	// SeenAt は worker の時計、CreatedAt は GitForge の時計である。時計のずれと、worker が先頭を見つけた時刻と提案を作った時刻の間を見込み、
	// 作った時刻に余裕を足して比べる。
	created := ar.CreatedAt.Add(clockSlack)
	start := -1
	for i, e := range h.Entries {
		if e.Deleted {
			continue
		}
		if e.Head == ar.FirstRevision && (start < 0 || !e.SeenAt.After(created)) {
			start = i
		}
	}
	var out []HeadCommit
	seen := map[string]bool{}
	if start >= 0 {
		for _, e := range h.Entries[start:] {
			if e.Deleted {
				break
			}
			if seen[e.Head] {
				continue
			}
			seen[e.Head] = true
			seenAt := e.SeenAt
			out = append(out, HeadCommit{Commit: e.Head, BaseCommit: e.Base, SeenAt: &seenAt})
		}
	}
	if ar.HeadRevision != "" && !seen[ar.HeadRevision] {
		out = append(out, HeadCommit{Commit: ar.HeadRevision})
	}
	return out
}

// arDetail は、提案と、その計算に使ったものである。
type arDetail struct {
	port     ports.ApplyRequest
	reviews  ports.Reviews
	branch   *snapshot.Branch // 先頭のコミットのスナップショット。まだなければ nil
	model    ApplyRequest
	current  HeadCommit
	baseView *view
}

// applyRequest は、提案を読み、応答を作る。
// latest は、呼ぶ側が 1 度だけ読んだ、今の main の木である（merged の提案の、反映のコミットを探すのに使う）。
// 1 回の要求の中で、同じものを読み直さないため。
func (b *Backend) applyRequest(ctx context.Context, repo, gfRef string, ar ports.ApplyRequest, withOps bool, latest *view) (*arDetail, error) {
	h, err := b.Snap.BranchHistory(ctx, repo, ar.Branch)
	if err != nil {
		return nil, err
	}
	reviews, err := b.Forge.ListReviews(ctx, gfRef, ar.ID)
	if err != nil {
		return nil, err
	}
	d := &arDetail{port: ar, reviews: reviews}
	vs := headCommits(ar, h)
	m := &d.model
	*m = ApplyRequest{ID: ar.ID, Branch: ar.Branch, State: string(arState(ar, h)), HeadCommits: vs, Description: ar.Description,
		Operations: []Operation{}, Reviews: []Review{}, MergeFailures: []MergeFailure{}, Warnings: append([]string{}, ar.Warnings...)}
	if len(vs) > 0 {
		d.current = vs[len(vs)-1]
		m.HeadCommit = d.current.Commit
	}
	for i := range m.HeadCommits {
		c, err := b.Snap.Commit(ctx, repo, m.HeadCommits[i].Commit)
		if err == nil {
			if s := trailerValue(c.Trailers, trailerSession); s != "" {
				m.HeadCommits[i].AuthorSessionID = &s
			}
		} else if !errors.Is(err, ports.ErrNotFound) {
			return nil, err
		}
	}
	if ar.CloseReason != "" || ar.CloseSession != "" {
		r, s := ar.CloseReason, ar.CloseSession
		m.ClosedReason, m.ClosedBySessionID = &r, &s
	}
	for _, f := range ar.MergeFailures {
		m.MergeFailures = append(m.MergeFailures, MergeFailure{Commit: f.Revision, Reason: f.Reason, Detail: f.Detail})
	}
	for _, r := range reviews.Items {
		m.Reviews = append(m.Reviews, Review{ID: r.ID, Kind: string(r.Kind), Commit: r.Revision,
			Body: r.Body, ReviewerSessionID: r.Session, PromptRevision: r.PromptRevision, Valid: r.Revision == d.current.Commit})
	}
	if reviews.ChainBroken {
		m.Warnings = append(m.Warnings, "レビューの鎖が切れている。反映できない。close して、同じブランチから作り直す")
	}
	m.Warnings = append(m.Warnings, reviews.Warnings...)
	if m.State == string(ports.StateOpen) {
		a := awaiting(d.current.Commit, reviews.Items, ar.MergeFailures)
		m.Awaiting = a
	}
	if m.State == string(ports.StateMerged) && latest != nil {
		for _, e := range latest.main.Tree.History {
			if e.ApplyRequest == ar.ID {
				r := e.Revision
				m.MergedCommit = &r
			}
		}
	}
	if d.current.Commit != "" {
		br, err := b.Snap.Branch(ctx, repo, d.current.Commit)
		switch {
		case err == nil:
			d.branch = br
		case errors.Is(err, ports.ErrNotFound):
			m.Warnings = append(m.Warnings, "先頭のコミットのスナップショットが、まだない（worker を待っている）")
		default:
			return nil, err
		}
	}
	if withOps && d.branch != nil {
		ops, base, err := b.operations(ctx, repo, d.branch)
		if err != nil {
			return nil, err
		}
		m.Operations, d.baseView = ops, base
	}
	return d, nil
}

// awaiting は、open の提案が、レビュー待ち（review）か修正待ち（fix）かを決める（docs/work/README.md）。
//   - 修正待ち：先頭のコミットへの最後の決めるレビュー（approve か request_changes）が request_changes か、
//     先頭のコミットの反映が、opener が直すべき理由（stale_target、invariant_violation、conflict）で失敗した。
//   - レビュー待ち：先頭のコミットへの決めるレビューがないか、approve のあと、そのほかの理由（snapshot_pending など）で反映に失敗した
//     （approve し直しを待つ）。
//   - 先頭のコミットへの最後の決めるレビューが approve で、反映の失敗もなければ、どちらでもない（反映を待っている）。
func awaiting(current string, reviews []ports.Review, failures []ports.MergeFailure) *string {
	var last ports.ReviewKind
	for _, r := range reviews {
		if r.Revision == current && (r.Kind == ports.ReviewApprove || r.Kind == ports.ReviewRequestChanges) {
			last = r.Kind
		}
	}
	failed := slices.ContainsFunc(failures, func(f ports.MergeFailure) bool { return f.Revision == current && openerFixes[f.Reason] })
	retry := slices.ContainsFunc(failures, func(f ports.MergeFailure) bool { return f.Revision == current && !openerFixes[f.Reason] })
	var s string
	switch {
	case last == ports.ReviewRequestChanges || failed:
		s = "fix"
	case last == "" || retry:
		s = "review"
	default:
		return nil
	}
	return &s
}

// openerFixes は、opener が直すべき反映の失敗の理由である。修正待ちにする。
var openerFixes = map[string]bool{"stale_target": true, "invariant_violation": true, "conflict": true}

// operations は、先頭のコミットの差分を、その base の main に重ねて検査し、操作を復元する。
func (b *Backend) operations(ctx context.Context, repo string, br *snapshot.Branch) ([]Operation, *view, error) {
	base, err := b.mainAt(ctx, repo, br.Diff.Base)
	if err != nil {
		return nil, nil, err
	}
	head, conflicts, err := snapshot.Overlay(base.files, br.Diff)
	if err != nil {
		return nil, nil, err
	}
	if len(conflicts) > 0 {
		return []Operation{}, base, nil
	}
	res, err := tree.CheckChange(base.files, head, tree.Context{})
	if err != nil {
		return nil, nil, err
	}
	out := make([]Operation, 0, len(res.Operations))
	for i, op := range res.Operations {
		out = append(out, toOperation(i+1, op))
	}
	return out, base, nil
}

func toOperation(pos int, op tree.Operation) Operation {
	o := Operation{Kind: op.Kind, Position: pos}
	switch op.Kind {
	case tree.OpStoryCreate:
		o.StoryID, o.ParentHypothesisID, o.Title, o.Summary = op.StoryID, op.ParentHypothesisID, op.Title, op.Summary
		for _, h := range op.Hypotheses {
			o.Hypotheses = append(o.Hypotheses, NewHypothesis{ID: h.ID, Statement: h.Statement, Background: h.Background})
		}
	case tree.OpStoryInactivate:
		o.StoryID, o.Reason = op.StoryID, op.Reason
	case tree.OpVerdictSubmit:
		v := op.Verdict
		value := v.Value
		o.VerdictID, o.HypothesisID, o.Value, o.ReportBody = v.ID, op.HypothesisID, &value, v.Report
		o.Supersedes, o.Artifacts, o.Citations = v.Supersedes, v.Artifacts, v.Citations
	case tree.OpNodeEdit:
		o.HypothesisID, o.AppendBackground = op.HypothesisID, op.AppendBackground
	}
	return o
}

// claims は、ノードを対象にした open の提案と人への依頼である。
type claims struct {
	ars map[string][]string
	hrs map[string][]string
}

func (c claims) ar(id string) []string {
	if c.ars[id] == nil {
		return []string{}
	}
	return c.ars[id]
}

func (c claims) hr(id string) []string {
	if c.hrs[id] == nil {
		return []string{}
	}
	return c.hrs[id]
}

// openState は、GitForge から読んだ、open の提案と人への依頼である。
type openState struct {
	ars    []ports.ApplyRequest // open の提案（head のブランチが消えたものを除く）
	all    []ports.ApplyRequest
	hrs    []ports.HumanRequest
	claims claims
	// arTargets は、open の提案の対象ノードである。先頭のコミットのスナップショットがなければ、空。
	arTargets map[string][]string
}

// readOpen は、GitForge から提案と人への依頼を読み、open のものが対象にするノードを求める。
func (b *Backend) readOpen(ctx context.Context, repo, gfRef string, v *view) (*openState, error) {
	all, err := b.Forge.ListApplyRequests(ctx, gfRef)
	if err != nil {
		return nil, err
	}
	hrs, err := b.Forge.ListHumanRequests(ctx, gfRef)
	if err != nil {
		return nil, err
	}
	st := &openState{all: all, hrs: hrs, claims: claims{ars: map[string][]string{}, hrs: map[string][]string{}}, arTargets: map[string][]string{}}
	dirs := v.dirNode()
	for _, ar := range all {
		if ar.State != ports.StateOpen {
			continue
		}
		h, err := b.Snap.BranchHistory(ctx, repo, ar.Branch)
		if err != nil {
			return nil, err
		}
		if arState(ar, h) != ports.StateOpen {
			continue
		}
		st.ars = append(st.ars, ar)
		br, err := b.Snap.Branch(ctx, repo, ar.HeadRevision)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		targets := map[string]bool{}
		for _, e := range br.Diff.Entries {
			if t := targetOf(dirs, e.Path); t != "" {
				targets[t] = true
			}
		}
		for _, t := range sortedKeys(targets) {
			st.claims.ars[t] = append(st.claims.ars[t], ar.ID)
			st.arTargets[ar.ID] = append(st.arTargets[ar.ID], t)
		}
	}
	for _, hr := range hrs {
		if hr.Answer == nil && hr.TargetNode != "" {
			st.claims.hrs[hr.TargetNode] = append(st.claims.hrs[hr.TargetNode], hr.ID)
		}
	}
	for _, m := range []map[string][]string{st.claims.ars, st.claims.hrs} {
		for k := range m {
			sort.Strings(m[k])
		}
	}
	return st, nil
}
