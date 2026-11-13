package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
)

// 作業の種類（docs/work/README.md）。並びは、この順である。
var workKinds = []string{"undecided_leaf", "needs_reconsideration", "review_waiting", "fix_waiting", "human_request_answered", "citation_changed"}

// WorkQuery は、着手できる作業の問い合わせである。
type WorkQuery struct {
	Subtree        string
	Kinds          []string
	IncludeClaimed bool
	Cursor         string
	Limit          int
}

// work は、木と、GitForge から読んだ提案と人への依頼から、着手できる作業を計算する。
// 返すのは、active なノードに関わる作業だけである。並びは、種類、ノードの識別子、subject_id の順。
func (b *Backend) work(ctx context.Context, repo, ref string, v *view, open *openState, includeClaimed bool) ([]WorkItem, map[string][]string, error) {
	var items []WorkItem
	arTargets := map[string][]string{}
	active := func(id string) bool {
		n := v.main.Tree.Nodes[id]
		return n != nil && !n.Inactive
	}
	claimed := func(id string) bool { return !includeClaimed && len(open.claims.ars[id]) > 0 }
	waitingHuman := func(id string) bool { return len(open.claims.hrs[id]) > 0 }

	for _, id := range sortedKeys(v.tree.Hypotheses) {
		n := v.main.Tree.Nodes[id]
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
			if c.TargetKind != "subtree" {
				continue
			}
			num := v.main.Tree.HistoryNumber(c.Revision)
			if num == 0 || v.main.Tree.SubtreeChangedAfter(c.NodeID, num) {
				items = append(items, WorkItem{Kind: "citation_changed", SubjectID: vid, NodeID: h.ID})
				break
			}
		}
	}

	// レビュー待ちと修正待ちの提案。
	for _, ar := range open.ars {
		d, err := b.applyRequest(ctx, repo, ref, ar, false)
		if err != nil {
			return nil, nil, err
		}
		if d.model.Awaiting == nil {
			continue
		}
		targets := open.arTargets[ar.ID]
		arTargets[ar.ID] = targets
		if len(targets) > 0 && !slices.ContainsFunc(targets, active) {
			continue
		}
		it := WorkItem{Kind: *d.model.Awaiting + "_waiting", SubjectID: ar.ID}
		if len(targets) > 0 {
			it.NodeID = targets[0]
		}
		items = append(items, it)
	}

	// 回答済みの人への依頼。
	for _, hr := range open.hrs {
		if hr.Answer == nil || !active(hr.TargetNode) {
			continue
		}
		done, err := b.answerUsed(ctx, repo, v, open, hr)
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

// answerUsed は、回答済みの人への依頼が、作業から外れるかを返す（docs/work/README.md）。
//   - 回答を引用した apply request が open である（版の差分に、依頼の識別子が書かれている）。
//   - 回答のあとに、対象ノードを変えた反映がある（反映された、回答を引用した apply request を含む）。
func (b *Backend) answerUsed(ctx context.Context, repo string, v *view, open *openState, hr ports.HumanRequest) (bool, error) {
	for _, ar := range open.ars {
		br, err := b.Snap.Branch(ctx, repo, ar.HeadRevision)
		if err != nil {
			continue
		}
		for _, e := range br.Diff.Entries {
			if e.File != nil && e.File.Content != nil && strings.Contains(*e.File.Content, hr.ID) {
				return true, nil
			}
		}
	}
	n := v.main.Tree.Nodes[hr.TargetNode]
	if n == nil || len(n.Changes) == 0 {
		return false, nil
	}
	last := n.Changes[len(n.Changes)-1]
	c, err := b.Snap.Commit(ctx, repo, last.Revision)
	if err != nil {
		return false, nil
	}
	return trailerValue(c.Trailers, "Logiforge-Apply-Request") != "" && c.CommittedAt.After(hr.Answer.CreatedAt), nil
}

// ListWork は、着手できる作業を返す。カーソルは、計算したリビジョンに結びつける（design/api/README.md の「ページング」）。
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
	out := WorkPage{Revision: v.revision(), Counts: map[string]int{}, Items: []WorkItem{}}
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
	evidence := func(id string) bool { n := v.main.Tree.Nodes[id]; return n != nil && n.Evidence }
	out := Completion{Revision: v.revision(), RootValue: v.main.Tree.Nodes[root.ID].Value}
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
	out.Done = out.Decided && out.RemainingWork == 0
	return out, nil
}
