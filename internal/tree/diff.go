package tree

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Operation は、差分から復元した操作である（docs/apply-request/README.md の 4 種類）。
// 操作とファイルの変化は 1 対 1 に対応する（design/backend/README.md の「操作と差分」）。
type Operation struct {
	Kind string `json:"kind"`
	// story.create
	StoryID            string          `json:"story_id,omitempty"`
	ParentHypothesisID string          `json:"parent_hypothesis_id,omitempty"`
	Title              string          `json:"title,omitempty"`
	Summary            string          `json:"summary,omitempty"`
	Hypotheses         []NewHypothesis `json:"hypotheses,omitempty"`
	// story.inactivate
	Reason string `json:"reason,omitempty"`
	// verdict.submit と node.edit
	HypothesisID string `json:"hypothesis_id,omitempty"`
	// verdict.submit
	Verdict *VerdictContent `json:"verdict,omitempty"`
	// node.edit
	AppendBackground string `json:"append_background,omitempty"`
}

// NewHypothesis は、story.create で作る子仮説である。
type NewHypothesis struct {
	ID         string `json:"id"`
	Statement  string `json:"statement"`
	Background string `json:"background,omitempty"`
}

// 操作の種類。
const (
	OpStoryCreate     = "story.create"
	OpStoryInactivate = "story.inactivate"
	OpVerdictSubmit   = "verdict.submit"
	OpNodeEdit        = "node.edit"
)

// Context は、差分の検査に要る、ファイルの外の情報である。nil の項目は、その検査を省く。
type Context struct {
	// Others は、新しい識別子がぶつかってはいけない、base のほかの木である（今の main など）。
	Others []*Tree
	// WasLeafBeforeInactivation は、仮説が、祖先のストーリーが inactive になる直前に葉だったかを返す。
	WasLeafBeforeInactivation func(hypothesisID string) (bool, error)
	// SubtreeExists は、main のリビジョンの並びにあるリビジョンに、そのノードがあるかを返す。
	SubtreeExists func(nodeID, revision string) (bool, error)
}

// Result は、差分の検査の結果である。
type Result struct {
	// Rejected は、その差分が新しく破った規則である。1 つでもあれば、差分を受け付けない。
	Rejected []Violation
	// Warnings は、base でもすでに破れていた規則である。差分は拒まない。
	Warnings   []Violation
	Operations []Operation
	Head       *Tree
}

// OK は、拒む違反がないかを返す。
func (r Result) OK() bool { return len(r.Rejected) == 0 }

// CheckChange は、base から head への差分を検査し、操作を復元する（design/backend/checks.md）。
func CheckChange(base, head Files, ctx Context) (Result, error) {
	bt, ht := Build(base), Build(head)
	res := Result{Head: ht}
	reject := func(r Rule, node, format string, args ...any) {
		res.Rejected = append(res.Rejected, Violation{Rule: r, Node: node, Detail: fmt.Sprintf(format, args...)})
	}

	newly, existing := NewlyBroken(Check(bt), Check(ht))
	res.Rejected = append(res.Rejected, newly...)
	res.Warnings = existing

	explained := map[string]bool{}
	var ops []Operation

	// 新しいストーリー（story.create）。
	for _, id := range sortedKeys(ht.Stories) {
		s := ht.Stories[id]
		if _, ok := bt.Stories[id]; ok {
			continue
		}
		explained[s.Dir+"/"+StoryFile] = true
		op := Operation{Kind: OpStoryCreate, StoryID: id, ParentHypothesisID: s.Parent.ID, Title: s.Title, Summary: s.Summary}
		for _, h := range s.Hypotheses {
			op.Hypotheses = append(op.Hypotheses, NewHypothesis{ID: h.ID, Statement: h.Statement, Background: h.Background})
			explained[h.Dir+"/"+HypothesisFile] = true
			explained[h.Dir+"/"+BackgroundFile] = true
		}
		ops = append(ops, op)
		parent := s.Parent
		if len(parent.ValidVerdicts()) > 0 {
			reject(RuleStoryUnderVerdict, id, "検証結果を持つ仮説の下に、ストーリーを作った")
		}
		if HypothesisInactive(parent) {
			reject(RuleStoryUnderInactive, id, "inactive な部分木の下に、ストーリーを作った")
		}
	}
	// 新しい仮説は、新しいストーリーの子でなければならない。
	for _, id := range sortedKeys(ht.Hypotheses) {
		h := ht.Hypotheses[id]
		if _, ok := bt.Hypotheses[id]; ok || h.Parent == nil {
			continue
		}
		if _, ok := bt.Stories[h.Parent.ID]; ok {
			reject(RuleStorySetChanged, id, "すでにあるストーリーに、子仮説を足した")
		}
	}
	// inactive にしたストーリー（story.inactivate）。
	newlyInactivated := map[string]bool{}
	for _, id := range sortedKeys(ht.Stories) {
		s := ht.Stories[id]
		if !s.Inactivated {
			continue
		}
		if b, ok := bt.Stories[id]; ok && b.Inactivated {
			continue
		}
		newlyInactivated[id] = true
		explained[s.Dir+"/"+InactivatedFile] = true
		ops = append(ops, Operation{Kind: OpStoryInactivate, StoryID: id, Reason: s.Reason})
	}
	// 新しい検証結果（verdict.submit）。
	addedArtifactRefs := map[string]bool{}
	for _, id := range sortedKeys(ht.Verdicts) {
		v := ht.Verdicts[id]
		if _, ok := bt.Verdicts[id]; ok {
			continue
		}
		explained[v.Path] = true
		vc := v.VerdictContent
		ops = append(ops, Operation{Kind: OpVerdictSubmit, HypothesisID: v.Hypothesis.ID, Verdict: &vc})
		if len(v.Supersedes) > 0 {
			valid := map[string]bool{}
			if bh := bt.Hypotheses[v.Hypothesis.ID]; bh != nil {
				for _, bv := range bh.ValidVerdicts() {
					valid[bv.ID] = true
				}
			}
			for _, sid := range v.Supersedes {
				if !valid[sid] {
					reject(RuleSupersedesNotValid, id, "supersedes の %s が、同じ仮説のその時点で有効な検証結果でない", sid)
				}
			}
		}
		if err := checkVerdictInInactive(v, bt, newlyInactivated, ctx, reject); err != nil {
			return res, err
		}
		for _, a := range v.Artifacts {
			if a.Kind != "file" {
				continue
			}
			addedArtifactRefs[a.SHA256] = true
			if _, ok := ht.Artifacts[a.SHA256]; !ok {
				reject(RuleArtifactCitation, id, "artifact %s のファイルがない", a.SHA256)
			}
		}
		for _, c := range v.Citations {
			switch c.TargetKind {
			case "artifact":
				if _, ok := ht.Artifacts[c.SHA256]; !ok {
					reject(RuleArtifactCitation, id, "引用した artifact %s がない", c.SHA256)
				}
			case "subtree":
				if ctx.SubtreeExists == nil {
					continue
				}
				ok, err := ctx.SubtreeExists(c.NodeID, c.Revision)
				if err != nil {
					return res, err
				}
				if !ok {
					reject(RuleSubtreeCitation, id, "引用した部分木 %s@%s がない", c.NodeID, c.Revision)
				}
			}
		}
	}
	// 背景情報の追加（node.edit）。
	for _, id := range sortedKeys(ht.Hypotheses) {
		h := ht.Hypotheses[id]
		b, ok := bt.Hypotheses[id]
		if !ok || h.Background == b.Background {
			continue
		}
		explained[h.Dir+"/"+BackgroundFile] = true
		if !strings.HasPrefix(h.Background, b.Background) {
			reject(RuleBackgroundNotAppend, id, "background.md の末尾への追加でない")
			continue
		}
		ops = append(ops, Operation{Kind: OpNodeEdit, HypothesisID: id, AppendBackground: h.Background[len(b.Background):]})
	}
	// 新しい artifact は、加えた検証結果から参照されている。
	for _, sha := range sortedKeys(ht.Artifacts) {
		if _, ok := bt.Artifacts[sha]; ok {
			continue
		}
		explained[ArtifactPath(sha)] = true
		if !addedArtifactRefs[sha] {
			reject(RuleArtifactUnreferenced, sha, "加えた artifact が、加えた検証結果から参照されていない")
		}
	}
	// 識別子の衝突（base のほかの木）。
	for _, other := range ctx.Others {
		for _, op := range ops {
			for _, nid := range newIDs(op) {
				if _, ok := other.Hypotheses[nid]; ok {
					reject(RuleIDCollision, nid, "識別子が、ほかの木の仮説とぶつかる")
				} else if _, ok := other.Stories[nid]; ok {
					reject(RuleIDCollision, nid, "識別子が、ほかの木のストーリーとぶつかる")
				} else if _, ok := other.Verdicts[nid]; ok {
					reject(RuleIDCollision, nid, "識別子が、ほかの木の検証結果とぶつかる")
				}
			}
		}
	}
	// どの操作にもあたらないファイルの変化。
	for _, p := range ChangedPaths(base, head) {
		if !explained[p] {
			reject(RuleChangeNotAllowed, p, "4 種類の操作として読めない変化")
		}
	}

	sortOperations(ops)
	res.Operations = ops
	sort.SliceStable(res.Rejected, func(i, j int) bool { return res.Rejected[i].key() < res.Rejected[j].key() })
	return res, nil
}

func newIDs(op Operation) []string {
	switch op.Kind {
	case OpStoryCreate:
		out := []string{op.StoryID}
		for _, h := range op.Hypotheses {
			out = append(out, h.ID)
		}
		return out
	case OpVerdictSubmit:
		return []string{op.Verdict.ID}
	}
	return nil
}

func checkVerdictInInactive(v *Verdict, bt *Tree, newlyInactivated map[string]bool, ctx Context, reject func(Rule, string, string, ...any)) error {
	h := v.Hypothesis
	var nearest *Story
	for s := h.Parent; s != nil; s = s.Parent.Parent {
		if s.Inactivated {
			nearest = s
			break
		}
	}
	if nearest == nil {
		return nil
	}
	var wasLeaf bool
	if newlyInactivated[nearest.ID] {
		bh, ok := bt.Hypotheses[h.ID]
		wasLeaf = ok && len(bh.ActiveStories()) == 0
	} else {
		if ctx.WasLeafBeforeInactivation == nil {
			return nil
		}
		var err error
		if wasLeaf, err = ctx.WasLeafBeforeInactivation(h.ID); err != nil {
			return err
		}
	}
	if !wasLeaf {
		reject(RuleVerdictInInactive, v.ID, "inactive になる前に葉でなかった仮説に、検証結果を出した")
	}
	return nil
}

// ChangedPaths は、base と head で、加わったか、変わったか、消えたパスを返す。
func ChangedPaths(base, head Files) []string {
	var out []string
	for _, p := range sortedKeys(head) {
		hf := head[p]
		bf, ok := base[p]
		if !ok || !sameFile(bf, hf) {
			out = append(out, p)
		}
	}
	for _, p := range sortedKeys(base) {
		if _, ok := head[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// sameFile は、2 つのファイルが同じかを返す。両方に blob の SHA があれば、SHA で比べる。
func sameFile(a, b File) bool {
	if a.Mode != b.Mode {
		return false
	}
	if a.SHA != "" && b.SHA != "" {
		return a.SHA == b.SHA
	}
	return a.Size == b.Size && bytes.Equal(a.Content, b.Content)
}

var opOrder = map[string]int{OpStoryCreate: 0, OpStoryInactivate: 1, OpVerdictSubmit: 2, OpNodeEdit: 3}

func sortOperations(ops []Operation) {
	sort.SliceStable(ops, func(i, j int) bool {
		if opOrder[ops[i].Kind] != opOrder[ops[j].Kind] {
			return opOrder[ops[i].Kind] < opOrder[ops[j].Kind]
		}
		return opKey(ops[i]) < opKey(ops[j])
	})
}

func opKey(op Operation) string {
	switch op.Kind {
	case OpStoryCreate, OpStoryInactivate:
		return op.StoryID
	case OpVerdictSubmit:
		return op.Verdict.ID
	}
	return op.HypothesisID
}
