package snapshot

import (
	"path"
	"sort"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ids"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// Index は、main のコミットの記録を、最初のリビジョンから順に重ねて求める、history とノードの変更の一覧である
// （design/backend/snapshot.md の「コミットの記録と木」）。木は組み立てない。
// コミットの記録は、親から変わったパスだけで作る。木（Tree）は、Index とそのコミットのファイルから作る。
type Index struct {
	// History は、最初のリビジョンから、最後に足したコミットまでの、main のコミットの一覧である（番号の順）。
	History []HistoryEntry
	changes map[string][]Rev
	// nodeDirs は、これまでに見たノードのディレクトリから、ノードの識別子を引く。
	// inactive 化で「変わった」とみなす子孫を、パスの接頭辞で求めるために使う。
	nodeDirs map[string]string
	numbers  map[string]int
}

// NewIndex は、空の Index を返す。最初のリビジョンから重ねるときに使う。
func NewIndex() *Index {
	return &Index{changes: map[string][]Rev{}, nodeDirs: map[string]string{}, numbers: map[string]int{}}
}

// IndexFromTree は、main のコミットの木から、そのコミットまでの Index を作る。
func IndexFromTree(t *Tree) *Index {
	x := NewIndex()
	x.History = append([]HistoryEntry(nil), t.History...)
	for _, h := range t.History {
		x.numbers[h.Revision] = h.Number
	}
	for id, n := range t.Nodes {
		x.nodeDirs[n.Dir] = id
		if len(n.Changes) > 0 {
			x.changes[id] = append([]Rev(nil), n.Changes...)
		}
	}
	return x
}

// Number は、main のコミットの番号を返す。なければ 0。
func (x *Index) Number(revision string) int { return x.numbers[revision] }

// Changes は、ノードを変えた main のコミットの一覧を返す（番号の順）。
func (x *Index) Changes(node string) []Rev { return x.changes[node] }

// Record は、main のコミットの記録を、親から変わったパス（git の diff-tree の結果）だけで作り、Index に足す。
// 警告は、反映の記録がないことだけを付ける。新しく破れた不変条件と、worker の警告は、AddNewlyBroken と AddWorkerWarnings で足す。
func (x *Index) Record(info CommitInfo, changedPaths []string) Commit {
	c := Commit{Revision: info.Revision, Number: len(x.History) + 1, Kind: KindMain, CommittedAt: info.CommittedAt.UTC(), Trailers: info.Trailers}
	if n := len(x.History); n > 0 {
		p := x.History[n-1].Revision
		c.Parent = &p
	}
	paths := append([]string(nil), changedPaths...)
	sort.Strings(paths)
	for _, p := range paths {
		if node, kind := classify(p); kind != "" {
			c.Changes = append(c.Changes, NodeChange{Node: node, File: kind, Path: p})
		}
	}
	ar := trailer(info.Trailers, TrailerApplyRequest)
	noAR := ar == "" && c.Parent != nil && trailer(info.Trailers, TrailerInit) == ""
	if noAR {
		c.Warnings = append(c.Warnings, tree.Violation{Rule: RuleNoApplyRequest, Node: info.Revision, Detail: "反映の記録のないコミット"})
	}
	x.add(c, HistoryEntry{Number: c.Number, Revision: c.Revision, ApplyRequest: ar, NoApplyRequest: noAR})
	return c
}

// Replay は、置き場から読んだコミットの記録を、Index に足す。記録を作り直さずに、history とノードの変更の一覧を求めるときに使う。
// 記録は、最初のリビジョンから順に渡す。
func (x *Index) Replay(c Commit) {
	e := HistoryEntry{Number: c.Number, Revision: c.Revision, ApplyRequest: trailer(c.Trailers, TrailerApplyRequest)}
	for _, v := range c.Warnings {
		switch v.Rule {
		case RuleNoApplyRequest:
			e.NoApplyRequest = true
		case RuleOverlap, RuleOverlapUnchecked, RuleMainRewritten:
			e.WorkerWarned = true
		default:
			e.NewlyBroken = true
		}
	}
	x.add(c, e)
}

// add は、コミットの記録の、ノードのファイルの変化から、ノードの変更の一覧を足す（design/backend/merge.md の「対象ノードの変化」）。
//   - 仮説のファイル：その仮説
//   - story.md：そのストーリーと、親の仮説
//   - inactivated.md：そのストーリーと、親の仮説と、子孫のすべてのノード
func (x *Index) add(c Commit, e HistoryEntry) {
	x.History = append(x.History, e)
	x.numbers[c.Revision] = c.Number
	for _, ch := range c.Changes {
		x.nodeDirs[nodeDir(ch)] = ch.Node
	}
	affected := map[string]bool{}
	for _, ch := range c.Changes {
		affected[ch.Node] = true
		if ch.File != FileStory && ch.File != FileInactivated {
			continue
		}
		dir := nodeDir(ch)
		if parent := path.Base(path.Dir(dir)); ids.HypothesisPattern.MatchString(parent) {
			affected[parent] = true
		}
		if ch.File == FileInactivated {
			for d, id := range x.nodeDirs {
				if strings.HasPrefix(d, dir+"/") {
					affected[id] = true
				}
			}
		}
	}
	rev := Rev{Number: c.Number, Revision: c.Revision}
	for id := range affected {
		x.changes[id] = append(x.changes[id], rev)
	}
}

// AddNewlyBroken は、このコミットで新しく破れた不変条件を、最後に足したコミットの警告にする。
func (x *Index) AddNewlyBroken(c *Commit, vs ...tree.Violation) {
	if len(vs) == 0 {
		return
	}
	c.Warnings = append(c.Warnings, vs...)
	x.History[len(x.History)-1].NewlyBroken = true
}

// AddWorkerWarnings は、対象ノードの重なりや main の書き換えなど、worker が見つけた警告を、最後に足したコミットの警告にする。
func (x *Index) AddWorkerWarnings(c *Commit, vs ...tree.Violation) {
	if len(vs) == 0 {
		return
	}
	c.Warnings = append(c.Warnings, vs...)
	x.History[len(x.History)-1].WorkerWarned = true
}

// targets は、このコミットで反映した操作の対象ノードを、変わったファイルから求める（design/backend/merge.md の「対象ノードの変化」）。
//   - 検証結果と背景情報：その仮説
//   - 新しいストーリー：親の仮説
//   - inactive 化：そのストーリー
func (x *Index) targets(c Commit) []string {
	set := map[string]bool{}
	for _, ch := range c.Changes {
		switch ch.File {
		case FileVerdict, FileBackground, FileInactivated:
			set[ch.Node] = true
		case FileStory:
			// 新しいストーリーは、変更の一覧がこのコミットだけである。
			rs := x.changes[ch.Node]
			parent := path.Base(path.Dir(nodeDir(ch)))
			if len(rs) == 1 && rs[0].Number == c.Number && ids.HypothesisPattern.MatchString(parent) {
				set[parent] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// OverlapWarnings は、最後に足したコミット c（反映のコミット）の対象ノードが、base（番号 baseNumber の main のコミット）のあとに、
// c より前の別の反映で変わっていれば、警告を返す。c 自身は除く。
func (x *Index) OverlapWarnings(c Commit, baseNumber int) []tree.Violation {
	var out []tree.Violation
	for _, id := range x.targets(c) {
		for _, r := range x.changes[id] {
			if r.Number > baseNumber && r.Number < c.Number {
				out = append(out, tree.Violation{Rule: RuleOverlap, Node: id, Detail: "対象ノードが、base のあとに " + r.Revision + " で変わっていた"})
				break
			}
		}
	}
	return out
}

// classify は、パスがノードのファイルなら、ノードの識別子とファイルの種類を返す。そうでなければ空。
// 木を組み立てずに、パスの形だけで決める（design/files/layout.md）。
func classify(p string) (node string, kind FileKind) {
	if !strings.HasPrefix(p, tree.TreeDir+"/") {
		return "", ""
	}
	dir, base := path.Dir(p), path.Base(p)
	name := path.Base(dir)
	switch {
	case base == tree.HypothesisFile && ids.HypothesisPattern.MatchString(name):
		return name, FileHypothesis
	case base == tree.BackgroundFile && ids.HypothesisPattern.MatchString(name):
		return name, FileBackground
	case name == tree.VerdictsDir && strings.HasSuffix(base, ".md") && ids.HypothesisPattern.MatchString(path.Base(path.Dir(dir))):
		return path.Base(path.Dir(dir)), FileVerdict
	case base == tree.StoryFile && ids.StoryPattern.MatchString(name):
		return name, FileStory
	case base == tree.InactivatedFile && ids.StoryPattern.MatchString(name):
		return name, FileInactivated
	}
	return "", ""
}

// nodeDir は、ノードのファイルの変化から、ノードのディレクトリを返す。
func nodeDir(ch NodeChange) string {
	d := path.Dir(ch.Path)
	if ch.File == FileVerdict {
		d = path.Dir(d)
	}
	return d
}

// BuildTree は、main のコミットのファイルと、そのコミットまでを足した Index から、木を作る。
// ノードの変更の一覧と history は、Index から写す。破れている不変条件は、木から求める。
func BuildTree(files tree.Files, x *Index) *Tree {
	t := tree.Build(files)
	vals := tree.Compute(t)
	out := &Tree{Nodes: map[string]*Node{}, History: append([]HistoryEntry(nil), x.History...)}
	owner := map[string]*Node{} // ディレクトリから、ファイルを持つノードを引く
	for _, id := range sortedKeys(t.Hypotheses) {
		h := t.Hypotheses[id]
		n := &Node{ID: id, Kind: NodeHypothesis, Dir: h.Dir, Value: vals.Hypothesis[id].Ptr(),
			Leaf: vals.Leaf[id], NeedsReconsideration: vals.NeedsReconsideration[id],
			Inactive: tree.HypothesisInactive(h), Evidence: vals.Evidence[id]}
		if h.Parent != nil {
			n.Parent = h.Parent.ID
		}
		out.Nodes[id] = n
		owner[h.Dir] = n
		owner[h.Dir+"/"+tree.VerdictsDir] = n
	}
	for _, id := range sortedKeys(t.Stories) {
		s := t.Stories[id]
		n := &Node{ID: id, Kind: NodeStory, Dir: s.Dir, Parent: s.Parent.ID, Value: vals.Story[id].Ptr(),
			Inactive: tree.StoryInactive(s), Evidence: vals.Evidence[id]}
		out.Nodes[id] = n
		owner[s.Dir] = n
	}
	for _, p := range sortedKeys(files) {
		f := files[p]
		switch {
		case strings.HasPrefix(p, tree.ArtifactsDir+"/"):
			out.Artifacts = append(out.Artifacts, fromTreeFile(f, true))
		case fileKind(owner[path.Dir(p)], p) != "":
			n := owner[path.Dir(p)]
			n.Files = append(n.Files, fromTreeFile(f, false))
		default:
			out.Other = append(out.Other, fromTreeFile(f, false))
		}
	}
	for id, n := range out.Nodes {
		n.Changes = append([]Rev{}, x.changes[id]...)
	}
	out.Broken = tree.Check(t)
	return out
}
