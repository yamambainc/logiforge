package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/tree"
)

// 作業の種類（docs/work/README.md）。並びは、この順である。
var workKinds = []string{"undecided_leaf", "needs_reconsideration", "review_waiting", "fix_waiting", "approved", "human_request_answered", "citation_changed"}

// WorkQuery は、着手できる作業の問い合わせである。
type WorkQuery struct {
	Subtree        string
	Kinds          []string
	IncludeClaimed bool
	Cursor         string
	Limit          int
}

// work は、木と、GitForge から読んだ apply request と人への依頼から、着手できる作業を計算する。
// 返すのは、active なノードに関わる作業だけである。並びは、種類、ノードの識別子、subject_id の順。
func (b *Backend) work(ctx context.Context, repo, ref string, v *view, open *openState, includeClaimed bool) ([]WorkItem, map[string][]string, error) {
	var items []WorkItem
	arTargets := map[string][]string{}
	active := func(id string) bool {
		n := v.main.SnapshotTree.Nodes[id]
		return n != nil && !n.Inactive
	}
	claimed := func(id string) bool { return !includeClaimed && len(open.claims.ars[id]) > 0 }
	waitingHuman := func(id string) bool { return len(open.claims.hrs[id]) > 0 }

	for _, id := range sortedKeys(v.tree.Hypotheses) {
		n := v.main.SnapshotTree.Nodes[id]
		if n == nil || n.Inactive || claimed(id) || waitingHuman(id) {
			continue
		}
		if n.Leaf && n.Value == nil && len(v.tree.Hypotheses[id].ValidVerdicts()) == 0 {
			items = append(items, WorkItem{Kind: "undecided_leaf", SubjectID: id, NodeID: id})
		}
		if n.NeedsReconsideration {
			items = append(items, WorkItem{Kind: "needs_reconsideration", SubjectID: id, NodeID: id})
		}
	}

	// 引用元が変化した検証結果。
	for _, vid := range sortedKeys(v.tree.Verdicts) {
		tv := v.tree.Verdicts[vid]
		h := tv.Hypothesis
		if !active(h.ID) || claimed(h.ID) || !slices.ContainsFunc(h.ValidVerdicts(), func(x *treeVerdict) bool { return x.ID == vid }) {
			continue
		}
		for _, c := range tv.Citations {
			if c.TargetKind == "subtree" && citationChanged(v, tv, c.NodeID) {
				items = append(items, WorkItem{Kind: "citation_changed", SubjectID: vid, NodeID: h.ID})
				break
			}
		}
	}

	// レビュー待ちと修正待ちの apply request と、GitForge が承認済みと判定した apply request（merge を待つもの）。
	// 承認の状態は、GitForge が一覧と一緒に返すときだけわかる（design/adapters/pull-requests.md の「承認の状態」）。
	for _, ar := range open.ars {
		d, err := b.applyRequest(ctx, repo, ref, ar, readList, nil)
		if err != nil {
			return nil, nil, err
		}
		kind := ""
		switch {
		case d.model.Awaiting != nil:
			kind = *d.model.Awaiting + "_waiting"
		case d.model.ReviewDecision != nil && *d.model.ReviewDecision == string(ports.DecisionApproved):
			kind = "approved"
		default:
			continue
		}
		targets := open.arTargets[ar.ID]
		arTargets[ar.ID] = targets
		if len(targets) > 0 && !slices.ContainsFunc(targets, active) {
			continue
		}
		it := WorkItem{Kind: kind, SubjectID: ar.ID}
		if len(targets) > 0 {
			it.NodeID = targets[0]
		}
		items = append(items, it)
	}

	// 回答済みの人への依頼。
	refs := b.usedAnswers(ctx, repo, v, open)
	for _, hr := range open.hrs {
		if hr.Answer == nil || !active(hr.TargetNode) {
			continue
		}
		done, err := b.answerUsed(ctx, repo, v, refs, hr)
		if err != nil {
			return nil, nil, err
		}
		if !done {
			items = append(items, WorkItem{Kind: "human_request_answered", SubjectID: hr.ID, NodeID: hr.TargetNode})
		}
	}

	order := map[string]int{}
	for i, k := range workKinds {
		order[k] = i
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, c := items[i], items[j]
		if order[a.Kind] != order[c.Kind] {
			return order[a.Kind] < order[c.Kind]
		}
		if a.NodeID != c.NodeID {
			return a.NodeID < c.NodeID
		}
		return a.SubjectID < c.SubjectID
	})
	return items, arTargets, nil
}

// answerRefPattern は、回答の添付を読む API の URL と、人への依頼を読む API の URL である（design/files/artifacts.md の「人への依頼の回答」）。
// 検証結果は、この URL の link で回答を引用する。URL が依頼の識別子を持つので、issue ごとにコメントを読まずに引用を決められる。
var answerRefPattern = regexp.MustCompile(`/human-requests/([1-9][0-9]*)(?:/attachments/[0-9a-f]{64})?(?:$|[/?#])`)

// answerRefs は、検証結果が引用している回答の、依頼の識別子である。
type answerRefs struct {
	ids map[string]bool
}

func (r answerRefs) add(v tree.VerdictContent) {
	for _, a := range v.Artifacts {
		if a.Kind != "link" {
			continue
		}
		if m := answerRefPattern.FindStringSubmatch(a.URI); m != nil {
			r.ids[m[1]] = true
		}
	}
}

func (r answerRefs) cites(hr ports.HumanRequest) bool {
	return r.ids[hr.ID]
}

// usedAnswers は、回答を引用している検証結果を、1 度だけ集める。
//   - 今の main の木の検証結果（反映された）
//   - open の apply request の、先頭のコミットの差分の検証結果のファイル（背景情報など、ほかのファイルは見ない）
func (b *Backend) usedAnswers(ctx context.Context, repo string, v *view, open *openState) answerRefs {
	refs := answerRefs{ids: map[string]bool{}}
	for _, tv := range v.tree.Verdicts {
		refs.add(tv.VerdictContent)
	}
	for _, ar := range open.ars {
		br, err := b.Snap.Branch(ctx, repo, ar.HeadRevision)
		if err != nil {
			continue
		}
		for _, e := range br.Diff.Entries {
			if e.File == nil || e.File.Content == nil || path.Base(path.Dir(e.Path)) != tree.VerdictsDir {
				continue
			}
			if vc, err := tree.ParseVerdict([]byte(*e.File.Content)); err == nil {
				refs.add(vc)
			}
		}
	}
	return refs
}

// answerUsed は、回答済みの人への依頼が、作業から外れるかを返す（docs/work/README.md、design/backend/compute.md）。
//   - その回答を引用した検証結果が、反映されたか、open の apply request の差分にある。
//   - 対象ノードを最後に変えた反映のコミットが、回答より後である。反映のコミットは、対象ノードを変えたコミットを新しい順にたどり、
//     worker が、merge の pull request を見つけた、最初のコミットである。
func (b *Backend) answerUsed(ctx context.Context, repo string, v *view, refs answerRefs, hr ports.HumanRequest) (bool, error) {
	if refs.cites(hr) {
		return true, nil
	}
	n := v.main.SnapshotTree.Nodes[hr.TargetNode]
	if n == nil {
		return false, nil
	}
	used := false
	err := b.walkNodeChanges(ctx, repo, hr.TargetNode, n.LastChanged, func(_ snapshot.Rev, c snapshot.Commit, _ snapshot.NodeChange) bool {
		if snapshot.ApplyRequestOf(c) == "" {
			return true
		}
		used = c.CommittedAt.After(hr.Answer.CreatedAt)
		return false
	})
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	return used, err
}

// ListWork は、着手できる作業を返す。カーソルは、計算したコミットに結びつける（design/api/paging.md）。
func (b *Backend) ListWork(ctx context.Context, c Caller, repo string, q WorkQuery) (WorkPage, error) {
	limit, err := CheckLimit(q.Limit)
	if err != nil {
		return WorkPage{}, err
	}
	for _, k := range q.Kinds {
		if !slices.Contains(workKinds, k) {
			return WorkPage{}, fail(CodeValidationFailed, "kind %q を知らない", k)
		}
	}
	rev, offset, err := decodeWorkCursor(q.Cursor)
	if err != nil {
		return WorkPage{}, err
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return WorkPage{}, err
	}
	v, err := b.mainAt(ctx, repo, rev)
	if err != nil {
		return WorkPage{}, err
	}
	open, err := b.readOpen(ctx, repo, ref, v)
	if err != nil {
		return WorkPage{}, err
	}
	all, arTargets, err := b.work(ctx, repo, ref, v, open, q.IncludeClaimed)
	if err != nil {
		return WorkPage{}, err
	}
	out := WorkPage{Commit: v.revision(), Validity: v.validity(), Counts: map[string]int{}, Items: []WorkItem{}}
	var filtered []WorkItem
	for _, it := range all {
		if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, it.Kind) {
			continue
		}
		if q.Subtree != "" && !b.inSubtreeItem(v, it, arTargets, q.Subtree) {
			continue
		}
		filtered = append(filtered, it)
		out.Counts[it.Kind]++
	}
	for i := offset; i < len(filtered) && len(out.Items) < limit; i++ {
		out.Items = append(out.Items, filtered[i])
	}
	if next := offset + len(out.Items); next < len(filtered) {
		c := encodeWorkCursor(v.revision(), next)
		out.NextCursor = &c
	}
	return out, nil
}

func (b *Backend) inSubtreeItem(v *view, it WorkItem, arTargets map[string][]string, root string) bool {
	if ts, ok := arTargets[it.SubjectID]; ok {
		return slices.ContainsFunc(ts, func(t string) bool { return v.inSubtree(root, t) })
	}
	return it.NodeID != "" && v.inSubtree(root, it.NodeID)
}

func encodeWorkCursor(rev string, offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(rev + "\x00" + strconv.Itoa(offset)))
}

func decodeWorkCursor(cursor string) (string, int, error) {
	if cursor == "" {
		return "", 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", 0, fail(CodeValidationFailed, "cursor が正しくない")
	}
	rev, off, ok := bytes.Cut(raw, []byte{0})
	n, err := strconv.Atoi(string(off))
	if !ok || err != nil || n < 0 {
		return "", 0, fail(CodeValidationFailed, "cursor が正しくない")
	}
	return string(rev), n, nil
}

// GetCompletion は、作業の終わりを判定する（docs/work/README.md の「作業の終わりの判定」）。
func (b *Backend) GetCompletion(ctx context.Context, c Caller, repo string) (Completion, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return Completion{}, err
	}
	v, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return Completion{}, err
	}
	root := v.tree.Root()
	if root == nil {
		return Completion{}, fail(CodeNotFound, "根の仮説がない")
	}
	open, err := b.readOpen(ctx, repo, ref, v)
	if err != nil {
		return Completion{}, err
	}
	all, arTargets, err := b.work(ctx, repo, ref, v, open, false)
	if err != nil {
		return Completion{}, err
	}
	evidence := func(id string) bool { n := v.main.SnapshotTree.Nodes[id]; return n != nil && n.Evidence }
	out := Completion{Commit: v.revision(), Validity: v.validity(), RootValue: v.main.SnapshotTree.Nodes[root.ID].Value}
	out.Decided = out.RootValue != nil
	for _, it := range all {
		if ts, ok := arTargets[it.SubjectID]; ok {
			if slices.ContainsFunc(ts, evidence) {
				out.RemainingWork++
			}
		} else if evidence(it.NodeID) {
			out.RemainingWork++
		}
	}
	// 対象のわからない（worker が追いついていない）レビュー待ち・修正待ちの apply request があれば、根拠部分木にあるかもしれないので、終わりとしない。
	unknown := slices.ContainsFunc(all, func(it WorkItem) bool {
		ts, ok := arTargets[it.SubjectID]
		return ok && len(ts) == 0
	})
	out.Done = out.Decided && out.RemainingWork == 0 && !unknown
	return out, nil
}

// citationChanged は、検証結果 tv の部分木の引用の引用元が、引用したあとに変わったかを返す（docs/tree/verdict.md の「引用元の変化」）。
// 引用した時点は、検証結果のファイルが加わった main のコミットである（design/files/revisions.md の「引用した時点」）。
// 加わったコミットがわからなければ、変わったものとみなす。
func citationChanged(v *view, tv *tree.Verdict, node string) bool {
	added := fileAdded(v, tv.Hypothesis.ID, tv.Path)
	if added == nil || added.Number == 0 {
		return true
	}
	return v.main.SnapshotTree.SubtreeChangedAfter(node, added.Number)
}
