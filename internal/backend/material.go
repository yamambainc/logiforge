package backend

import (
	"context"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// GetReviewMaterial は、先頭のコミットの差分に対するレビューの材料を返す（docs/apply-request/review.md の「レビューの材料」）。説明欄は含めない。
//   - 対象ノード：操作の対象ノードの、今の main での状態と値
//   - 親仮説から根まで：対象ノードの親の仮説から、根までの仮説
//   - 兄弟ストーリー：story.create では親仮説の子ストーリー、story.inactivate では同じ親仮説のほかのストーリー、
//     verdict.submit と node.edit では、対象の仮説が属するストーリーと同じ親仮説を持つ、ほかのストーリー
//   - 引用：引用している部分木（引用したコミットのもの）と、artifact
func (b *Backend) GetReviewMaterial(ctx context.Context, c Caller, repo, id string) (ReviewMaterial, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return ReviewMaterial{}, err
	}
	ar, err := b.Forge.GetApplyRequest(ctx, ref, id)
	if err != nil {
		return ReviewMaterial{}, err
	}
	// 向き先が main でない apply request は、Logiforge は検査しないので、レビューの材料も作らない。
	if !targetsMain(ar) {
		return ReviewMaterial{}, fail(CodeOverlayUnsupported, "apply request %s の向き先が main でない（%s）。Logiforge は検査しない", id, ar.Target)
	}
	v, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return ReviewMaterial{}, err
	}
	d, err := b.applyRequest(ctx, repo, ref, ar, true, v)
	if err != nil {
		return ReviewMaterial{}, err
	}
	if d.branch == nil {
		return ReviewMaterial{}, fail(CodeSnapshotPending, "先頭のコミット %s のスナップショットが、まだない", d.current.Commit)
	}
	open, err := b.readOpen(ctx, repo, ref, v)
	if err != nil {
		return ReviewMaterial{}, err
	}
	m := ReviewMaterial{ApplyRequestID: id, Commit: d.current.Commit, Operations: d.model.Operations,
		Targets: []Node{}, Ancestors: []*Hypothesis{}, Siblings: []*Story{}, Citations: []MaterialCitation{}}
	seenT, seenA, seenS := map[string]bool{}, map[string]bool{}, map[string]bool{}
	addAncestors := func(h *tree.Hypothesis) {
		for ; h != nil; h = parentHyp(h) {
			if !seenA[h.ID] {
				seenA[h.ID] = true
				m.Ancestors = append(m.Ancestors, v.hypothesis(h, open.claims))
			}
		}
	}
	addSiblings := func(parent *tree.Hypothesis, except string) {
		if parent == nil {
			return
		}
		for _, s := range parent.Stories {
			if s.ID != except && !seenS[s.ID] {
				seenS[s.ID] = true
				m.Siblings = append(m.Siblings, v.story(s, open.claims))
			}
		}
	}
	for _, op := range m.Operations {
		var target string
		switch op.Kind {
		case tree.OpStoryCreate:
			target = op.ParentHypothesisID
			if h := v.tree.Hypotheses[target]; h != nil {
				addAncestors(parentHyp(h))
				addSiblings(h, "")
			}
		case tree.OpStoryInactivate:
			target = op.StoryID
			if s := v.tree.Stories[target]; s != nil {
				addAncestors(s.Parent)
				addSiblings(s.Parent, s.ID)
			}
		case tree.OpVerdictSubmit, tree.OpNodeEdit:
			target = op.HypothesisID
			if h := v.tree.Hypotheses[target]; h != nil && h.Parent != nil {
				addAncestors(h.Parent.Parent)
				addSiblings(h.Parent.Parent, h.Parent.ID)
			}
		}
		if n := v.node(target, open.claims); n != nil && !seenT[target] {
			seenT[target] = true
			m.Targets = append(m.Targets, n)
		}
		for _, ci := range op.Citations {
			mc := MaterialCitation{Citation: ci}
			switch ci.TargetKind {
			case "subtree":
				if cv, err := b.mainAt(ctx, repo, ci.Commit); err == nil {
					mc.Nodes = cv.subtree(ci.NodeID, -1, claims{})
				}
			case "artifact":
				// 同じ操作の artifact か、今の木の検証結果が参照する artifact から引く。
				mc.Artifact = findArtifact(op.Artifacts, ci.SHA256)
				for _, vid := range sortedKeys(v.tree.Verdicts) {
					if mc.Artifact != nil {
						break
					}
					mc.Artifact = findArtifact(v.tree.Verdicts[vid].Artifacts, ci.SHA256)
				}
			}
			m.Citations = append(m.Citations, mc)
		}
	}
	return m, nil
}

func parentHyp(h *tree.Hypothesis) *tree.Hypothesis {
	if h.Parent == nil {
		return nil
	}
	return h.Parent.Parent
}

func findArtifact(as []tree.ArtifactRef, sha string) *tree.ArtifactRef {
	for i := range as {
		if as[i].Kind == "file" && as[i].SHA256 == sha {
			a := as[i]
			return &a
		}
	}
	return nil
}
