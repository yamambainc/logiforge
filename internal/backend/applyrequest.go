package backend

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

// trailer の名前（design/files/revisions.md）。

// arState は、apply request の状態を決める。head のブランチが消えた open の apply request は、closed として扱う（design/adapters/pull-requests.md の「apply request の状態」）。
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

// headCommits は、ブランチの履歴から、apply request のブランチに push された先頭のコミットの一覧を作る（design/backend/apply-request/README.md の「先頭のコミット」）。
//   - pull request を作ったときの先頭のコミットから後の、ブランチの先頭を、初めて見た順に並べる。
//   - 先頭が前に見た SHA に戻ったら（A → B → A）、一覧に足さない。
//
// GitForge の今の head が、まだブランチの履歴にない（worker が追いついていない）ときは、それを、base と時刻のない最後の項目として足す。
func headCommits(ar ports.ApplyRequest, h store.BranchHistory) []HeadCommit {
	// SeenAt は worker の時計、CreatedAt は GitForge の時計である。
	//   作ったときの先頭は、worker が作った時刻の前後どちらで見たかわからないので、作った時刻から時計のずれを引いた時刻のあとに見た、
	//   最初の先頭から並べる。その前にしか見ていなければ、最後に見た先頭から並べる。並べる先頭が多くても、レビューのセッションの条件が厳しくなるだけである。
	from := ar.CreatedAt.Add(-clockSlack)
	start := -1
	for i, e := range h.Entries {
		if e.Deleted {
			start = -1
			continue
		}
		// ブランチが main と同じ（まだ自分のコミットがない）ときの先頭は、apply request の先頭ではない。
		if e.Head == e.Base {
			continue
		}
		if start < 0 || h.Entries[start].SeenAt.Before(from) {
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
			if seen[e.Head] || e.Head == e.Base {
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

// arDetail は、apply request と、その計算に使ったものである。
type arDetail struct {
	port     ports.ApplyRequest
	reviews  ports.Reviews
	history  store.BranchHistory
	branch   *snapshot.Branch // 先頭のコミットのスナップショット。まだなければ nil
	model    ApplyRequest
	current  HeadCommit
	baseView *view
}

// arRead は、apply request を読むときに、何を読むかである。
type arRead struct {
	// ops は、操作の一覧を作るか。reviews は、その pull request の review を GitForge から読むか。
	// 一覧では、どちらも読まない（design/backend/list-reads.md）。review は、1 件の読みと、反映でだけ読む。
	ops, reviews bool
}

var (
	readList   = arRead{}
	readDetail = arRead{ops: true, reviews: true}
)

// applyRequest は、apply request を読み、応答を作る。
// latest は、呼ぶ側が 1 度だけ読んだ、今の main の木である（merged の apply request の、反映のコミットを探すのに使う）。
// 1 回の要求の中で、同じものを読み直さないため。
func (b *Backend) applyRequest(ctx context.Context, repo, gfRef string, ar ports.ApplyRequest, how arRead, latest *view) (*arDetail, error) {
	h, err := b.Snap.BranchHistory(ctx, repo, ar.Branch)
	if err != nil {
		return nil, err
	}
	var reviews ports.Reviews
	if how.reviews {
		if reviews, err = b.Forge.ListReviews(ctx, gfRef, ar.ID); err != nil {
			return nil, err
		}
	}
	d := &arDetail{port: ar, reviews: reviews, history: h}
	vs := headCommits(ar, h)
	m := &d.model
	target := ar.Target
	if target == "" {
		target = mainTarget
	}
	*m = ApplyRequest{ID: ar.ID, Branch: ar.Branch, Target: target, State: string(arState(ar, h)), HeadCommits: vs, Description: ar.Description,
		Operations: []Operation{}, Reviews: []Review{}, Comments: []Comment{}, Warnings: append([]string{}, ar.Warnings...)}
	if len(vs) > 0 {
		d.current = vs[len(vs)-1]
		m.HeadCommit = d.current.Commit
	}
	for _, c := range ar.Comments {
		m.Comments = append(m.Comments, commentOf(c))
	}
	for _, r := range reviews.Items {
		m.Reviews = append(m.Reviews, reviewOf(r, r.Revision == d.current.Commit))
	}
	m.Warnings = append(m.Warnings, reviews.Warnings...)
	// 向き先が main でない apply request は、Logiforge の検査と反映の対象でないので、レビュー待ちにも修正待ちにもしない。
	if m.State == string(ports.StateOpen) && targetsMain(ar) {
		m.Awaiting = awaiting(ar.Waiting)
		// 承認の状態は、1 件を読むときは review の一覧と一緒に、一覧では pull request の一覧と一緒に、GitForge から読む。
		dec := ar.ReviewDecision
		switch {
		case how.reviews:
			dec = reviews.Decision
		case dec == ports.DecisionNone && ar.Waiting == ports.WaitingNone:
			// 一覧で承認の状態が分からず、どちらのラベルもなければ、承認済みとみなす。Logiforge がラベルを外すのは、approve のときだけだから
			// （design/adapters/pull-requests.md の「ラベルがずれたとき」）。1 件を読めば、GitForge の review から正しい状態に直る。
			dec = ports.DecisionApproved
		}
		if dec != ports.DecisionNone {
			s := string(dec)
			m.ReviewDecision = &s
		}
		if ar.Waiting == ports.WaitingBoth {
			m.Warnings = append(m.Warnings, "レビュー待ちと修正待ちのラベルが、両方付いている。修正待ちとみなす")
		}
	}
	// 反映したコミットは、GitForge の merge のコミットである。worker が取り込んで、読む側に出ているときだけ返す。
	if m.State == string(ports.StateMerged) && latest != nil && ar.MergedRevision != "" {
		if n, err := b.mainNumber(ctx, latest, ar.MergedRevision); err != nil {
			return nil, err
		} else if n > 0 {
			r := ar.MergedRevision
			m.MergedCommit = &r
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
	// 向き先が main でなければ、差分を検査しないので、操作の一覧は空にする（design/backend/apply-request/target.md の「向き先」）。
	if how.ops && d.branch != nil && targetsMain(ar) {
		ops, base, err := b.operations(ctx, repo, d.branch)
		if err != nil {
			return nil, err
		}
		m.Operations, d.baseView = ops, base
	}
	return d, nil
}

// awaiting は、open で向き先が main の apply request が、レビュー待ち（review）か修正待ち（fix）かを、pull request のラベルで決める
// （design/backend/compute.md の「レビュー待ちと修正待ち」）。両方あれば修正待ちとみなす。
// ラベルがなければ、どちらでもない（approve されて merge を待つもの。nil）。
func awaiting(w ports.Waiting) *string {
	var s string
	switch w {
	case ports.WaitingReview:
		s = "review"
	case ports.WaitingFix, ports.WaitingBoth:
		s = "fix"
	default:
		return nil
	}
	return &s
}

// desiredWaiting は、その pull request の出来事のうち、いちばん新しいもので、正しいラベルを決める
// （design/adapters/pull-requests.md の「ラベルがずれたとき」）。
//   - 数える request changes の review：修正待ち
//   - 修正待ちのラベルを付けたこと（opener が直すべき理由で、approve か merge が確かめに通らなかったとき。GitForge の画面で付けたものも含む）：修正待ち。
//     ラベルを付けられるのは、書く権限に近い権限を持つ人だけである（design/adapters/pull-requests.md）
//   - レビュー待ちのラベルを付けたこと（作成、直し終えたという申告）：レビュー待ち
//   - 数える approve の review：どちらのラベルもない（どのコミットへの approve かは見ない。古い approve を外すかは GitForge の設定に任せる）
//   - どれもなければ、レビュー待ち
//
// reviews は、数える review だけである。
func desiredWaiting(ar ports.ApplyRequest, reviews []ports.Review) ports.Waiting {
	at := map[ports.Waiting]time.Time{ports.WaitingReview: ar.ReviewWaitingAt}
	later := func(w ports.Waiting, t time.Time) {
		if t.After(at[w]) {
			at[w] = t
		}
	}
	for _, e := range ar.FixWaitingLabeled {
		later(ports.WaitingFix, e.At)
	}
	for _, r := range reviews {
		switch {
		case r.Kind == ports.ReviewRequestChanges:
			later(ports.WaitingFix, r.CreatedAt)
		case r.Kind == ports.ReviewApprove:
			later(ports.WaitingNone, r.CreatedAt)
		}
	}
	// GitForge の時刻は秒の単位なので、同じ秒の出来事は、どちらが新しいか決まらない。そのときは、今のラベルがその中にあれば変えない。
	var newest time.Time
	for _, t := range at {
		if t = t.Truncate(time.Second); t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() {
		return ports.WaitingReview
	}
	var tied []ports.Waiting
	for _, w := range []ports.Waiting{ports.WaitingFix, ports.WaitingReview, ports.WaitingNone} {
		if t, ok := at[w]; ok && t.Truncate(time.Second).Equal(newest) {
			tied = append(tied, w)
		}
	}
	if len(tied) > 1 && slices.Contains(tied, ar.Waiting) {
		return ar.Waiting
	}
	return tied[0]
}

// repairWaiting は、1 件の apply request を読んだときに、ラベルがずれていれば、要求を送った主体のトークンで直す
// （design/adapters/pull-requests.md の「ラベルがずれたとき」）。d は、review を読んだもの（readDetail）である。
// 直せなければ（主体に書く権限がない、など）、応答に警告を付ける。
func (b *Backend) repairWaiting(ctx context.Context, gfRef string, d *arDetail) {
	if d.model.State != string(ports.StateOpen) || !targetsMain(d.port) {
		return
	}
	want := desiredWaiting(d.port, d.reviews.Items)
	if d.port.Waiting == want {
		return
	}
	if err := b.Forge.SetWaiting(ctx, gfRef, d.port.ID, want); err != nil {
		d.model.Warnings = append(d.model.Warnings, "レビュー待ちか修正待ちかのラベルがずれているが、直せない（書く権限のある人が読めば直る）："+err.Error())
		return
	}
	d.port.Waiting = want
	d.model.Awaiting = awaiting(want)
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

// claims は、ノードを対象にした open の apply request と人への依頼である。
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

// openState は、GitForge から読んだ、open の apply request と人への依頼である。
type openState struct {
	ars    []ports.ApplyRequest // open の apply request（head のブランチが消えたものを除く）
	all    []ports.ApplyRequest
	hrs    []ports.HumanRequest
	claims claims
	// arTargets は、open の apply request の対象ノードである。先頭のコミットのスナップショットがなければ、空。
	arTargets map[string][]string
}

// inRepository は、pull request が、その repository の apply request かを返す。ディレクトリの repository では、
// ディレクトリの中を変えるブランチだけが、worker のブランチの履歴に入る（design/directory/README.md の「apply request とみなす PR」）。
func inRepository(gfRef string, h store.BranchHistory) bool {
	if _, dir := ports.SplitRef(gfRef); dir == "" {
		return true
	}
	return len(h.Entries) > 0
}

// readOpen は、GitForge から apply request と人への依頼を読み、open のものが対象にするノードを求める。
func (b *Backend) readOpen(ctx context.Context, repo, gfRef string, v *view) (*openState, error) {
	all, err := b.Forge.ListApplyRequests(ctx, gfRef, "open")
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
		if arState(ar, h) != ports.StateOpen || !inRepository(gfRef, h) {
			continue
		}
		// 向き先が main でないものも、回答を引用した検証結果を探すのに使う（積み上げた変更は、いずれ main 向きの apply request に含まれるから）。
		// ただし、検査と反映の対象でないので、ノードを対象にしたもの（作業から外すもの）には数えない。
		st.ars = append(st.ars, ar)
		if !targetsMain(ar) {
			continue
		}
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
