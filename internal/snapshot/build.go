package snapshot

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// RuleNoApplyRequest は、反映の記録（Logiforge-Apply-Request:）のない main のコミットの警告である。
const RuleNoApplyRequest tree.Rule = "no_apply_request"

// TrailerApplyRequest は、反映のコミットの、apply request の識別子の trailer である（design/files/revisions.md）。
const TrailerApplyRequest = "Logiforge-Apply-Request"

// BuildMain は、main のコミットのスナップショットを作る。parent は、first-parent のスナップショット（最初のリビジョンでは nil）。
// files は、そのコミットの木のファイルである。artifact は中身か SHA256 を持つ。
func BuildMain(files tree.Files, info CommitInfo, parent *Main) (*Main, error) {
	t := tree.Build(files)
	vals := tree.Compute(t)
	m := &Main{Commit: Commit{
		Revision:    info.Revision,
		Number:      1,
		Kind:        KindMain,
		CommittedAt: info.CommittedAt,
		Trailers:    info.Trailers,
	}}
	var parentFiles tree.Files
	if parent != nil {
		p := parent.Commit.Revision
		m.Commit.Parent = &p
		m.Commit.Number = parent.Commit.Number + 1
		var err error
		if parentFiles, err = parent.Tree.Files(); err != nil {
			return nil, err
		}
	}

	// ノードと、そのファイル。
	m.Tree.Nodes = map[string]*Node{}
	owner := map[string]*Node{} // ディレクトリから、ファイルを持つノードを引く
	for _, id := range sortedKeys(t.Hypotheses) {
		h := t.Hypotheses[id]
		n := &Node{ID: id, Kind: NodeHypothesis, Dir: h.Dir, Value: vals.Hypothesis[id].Ptr(),
			Leaf: vals.Leaf[id], NeedsReconsideration: vals.NeedsReconsideration[id],
			Inactive: tree.HypothesisInactive(h), Evidence: vals.Evidence[id]}
		if h.Parent != nil {
			n.Parent = h.Parent.ID
		}
		m.Tree.Nodes[id] = n
		owner[h.Dir] = n
		owner[h.Dir+"/"+tree.VerdictsDir] = n
	}
	for _, id := range sortedKeys(t.Stories) {
		s := t.Stories[id]
		n := &Node{ID: id, Kind: NodeStory, Dir: s.Dir, Parent: s.Parent.ID, Value: vals.Story[id].Ptr(),
			Inactive: tree.StoryInactive(s), Evidence: vals.Evidence[id]}
		m.Tree.Nodes[id] = n
		owner[s.Dir] = n
	}
	for _, p := range sortedKeys(files) {
		f := files[p]
		switch {
		case strings.HasPrefix(p, tree.ArtifactsDir+"/"):
			m.Tree.Artifacts = append(m.Tree.Artifacts, fromTreeFile(f, true))
		case fileKind(owner[path.Dir(p)], p) != "":
			n := owner[path.Dir(p)]
			n.Files = append(n.Files, fromTreeFile(f, false))
		default:
			m.Tree.Other = append(m.Tree.Other, fromTreeFile(f, false))
		}
	}

	// 親から直接変わったファイルと、ノードの変更の一覧。
	changed := tree.ChangedPaths(parentFiles, files)
	affected := map[string]bool{}
	seen := map[NodeChange]bool{}
	for _, p := range changed {
		n := owner[path.Dir(p)]
		kind := fileKind(n, p)
		if kind == "" {
			continue
		}
		if c := (NodeChange{Node: n.ID, File: kind}); !seen[c] {
			seen[c] = true
			m.Commit.Changes = append(m.Commit.Changes, c)
		}
		for _, id := range affectedNodes(t, n.ID, kind) {
			affected[id] = true
		}
	}
	rev := Rev{Number: m.Commit.Number, Revision: info.Revision}
	for id, n := range m.Tree.Nodes {
		if parent != nil {
			if pn := parent.Tree.Nodes[id]; pn != nil {
				n.Changes = append(n.Changes, pn.Changes...)
			}
		}
		if affected[id] {
			n.Changes = append(n.Changes, rev)
		}
		if n.Changes == nil {
			n.Changes = []Rev{}
		}
	}

	// 警告。
	ar := trailer(info.Trailers, TrailerApplyRequest)
	if ar == "" && parent != nil {
		m.Commit.Warnings = append(m.Commit.Warnings, tree.Violation{Rule: RuleNoApplyRequest, Node: info.Revision, Detail: "反映の記録のないコミット"})
	}
	m.Commit.Warnings = append(m.Commit.Warnings, tree.Check(t)...)

	if parent != nil {
		m.Tree.History = append(m.Tree.History, parent.Tree.History...)
	}
	m.Tree.History = append(m.Tree.History, HistoryEntry{Number: m.Commit.Number, Revision: info.Revision, ApplyRequest: ar, Warned: len(m.Commit.Warnings) > 0})
	return m, nil
}

// BuildBranch は、ブランチの先頭のコミットのスナップショットを作る。base は main と分かれた点のコミットの SHA、
// baseFiles と headFiles は、その 2 つのコミットの木のファイルである。
func BuildBranch(baseSHA string, baseFiles, headFiles tree.Files, info CommitInfo) *Branch {
	b := &Branch{
		Commit: Commit{Revision: info.Revision, Parent: &baseSHA, Kind: KindBranch, CommittedAt: info.CommittedAt, Trailers: info.Trailers},
		Diff:   Diff{Base: baseSHA, Entries: []DiffEntry{}},
	}
	for _, p := range tree.ChangedPaths(baseFiles, headFiles) {
		e := DiffEntry{Path: p}
		bf, inBase := baseFiles[p]
		hf, inHead := headFiles[p]
		switch {
		case !inBase:
			e.Status = Added
		case !inHead:
			e.Status = Deleted
		default:
			e.Status = Modified
		}
		if inBase {
			e.BaseSHA = bf.SHA
		}
		if inHead {
			f := fromTreeFile(hf, strings.HasPrefix(p, tree.ArtifactsDir+"/"))
			e.File = &f
		}
		b.Diff.Entries = append(b.Diff.Entries, e)
		b.Commit.Paths = append(b.Commit.Paths, PathChange{Path: p, Status: e.Status})
	}
	return b
}

// Files は、main のスナップショットの木を、tree.Files に戻す。artifact は中身を持たず、SHA256 を持つ。
// バックエンドは、これと Overlay で、差分の検査に渡す木を作る。
func (t *Tree) Files() (tree.Files, error) {
	out := tree.Files{}
	add := func(f File) error {
		tf, err := f.toTreeFile()
		if err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		out[f.Path] = tf
		return nil
	}
	for _, n := range t.Nodes {
		for _, f := range n.Files {
			if err := add(f); err != nil {
				return nil, err
			}
		}
	}
	for _, list := range [][]File{t.Artifacts, t.Other} {
		for _, f := range list {
			if err := add(f); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// Overlay は、base の木に、ブランチの差分を重ねた木を返す。base は、差分の base でなくてもよい（反映の直前の、今の main）。
// 差分が変えたパスが、差分の base のあとに base の側でも変わっていれば、重ねられないので、そのパスを conflicts に返す。
// git の merge と同じく、両方で同じ中身になったパスは、重ねられる。
func Overlay(base tree.Files, d Diff) (head tree.Files, conflicts []string, err error) {
	head = tree.Files{}
	for p, f := range base {
		head[p] = f
	}
	for _, e := range d.Entries {
		cur, exists := base[e.Path]
		var want *tree.File
		if e.File != nil {
			f, err := e.File.toTreeFile()
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", e.Path, err)
			}
			want = &f
		}
		same := func() bool {
			return want != nil && exists && cur.SHA != "" && cur.SHA == want.SHA && cur.Mode == want.Mode
		}
		switch {
		case !exists && e.BaseSHA == "":
		case exists && cur.SHA == e.BaseSHA:
		case same():
		case !exists && want == nil:
		default:
			conflicts = append(conflicts, e.Path)
			continue
		}
		if want == nil {
			delete(head, e.Path)
		} else {
			head[e.Path] = *want
		}
	}
	sort.Strings(conflicts)
	return head, conflicts, nil
}

// fileKind は、p がノード n のファイルなら、その種類を返す。そうでなければ空。
func fileKind(n *Node, p string) FileKind {
	if n == nil {
		return ""
	}
	dir, base := path.Dir(p), path.Base(p)
	switch {
	case n.Kind == NodeHypothesis && dir == n.Dir && base == tree.HypothesisFile:
		return FileHypothesis
	case n.Kind == NodeHypothesis && dir == n.Dir && base == tree.BackgroundFile:
		return FileBackground
	case n.Kind == NodeHypothesis && dir == n.Dir+"/"+tree.VerdictsDir && strings.HasSuffix(base, ".md"):
		return FileVerdict
	case n.Kind == NodeStory && dir == n.Dir && base == tree.StoryFile:
		return FileStory
	case n.Kind == NodeStory && dir == n.Dir && base == tree.InactivatedFile:
		return FileInactivated
	}
	return ""
}

// affectedNodes は、ノードのファイルが変わったときに、「変わった」とみなすノードを返す（design/backend/merge.md の「対象ノードの変化」）。
//   - 仮説のファイル：その仮説
//   - story.md：そのストーリーと、親の仮説
//   - inactivated.md：そのストーリーと、親の仮説と、子孫のすべてのノード
func affectedNodes(t *tree.Tree, id string, kind FileKind) []string {
	switch kind {
	case FileHypothesis, FileBackground, FileVerdict:
		return []string{id}
	case FileStory:
		s := t.Stories[id]
		return []string{id, s.Parent.ID}
	case FileInactivated:
		s := t.Stories[id]
		out := []string{id, s.Parent.ID}
		var walk func(s *tree.Story)
		walk = func(s *tree.Story) {
			for _, h := range s.Hypotheses {
				out = append(out, h.ID)
				for _, c := range h.Stories {
					out = append(out, c.ID)
					walk(c)
				}
			}
		}
		walk(s)
		return out
	}
	return nil
}

func trailer(ts []Trailer, key string) string {
	for _, t := range ts {
		if t.Key == key {
			return t.Value
		}
	}
	return ""
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
