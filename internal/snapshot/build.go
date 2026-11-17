package snapshot

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/yamambainc/logiforge/internal/tree"
)

// RuleNoApplyRequest は、反映の記録（Logiforge-Apply-Request:）のない main のコミットの警告である。
const RuleNoApplyRequest tree.Rule = "no_apply_request"

// TrailerApplyRequest は、反映のコミットの、apply request の識別子の trailer である（design/files/revisions.md）。
const TrailerApplyRequest = "Logiforge-Apply-Request"

// TrailerInit は、repository の用意で置いた最初のコミットの trailer である（design/adapters/github.md）。
const TrailerInit = "Logiforge-Init"

// TrailerRepair は、直すための apply request の反映のコミットの trailer である。値は RepairRevert か RepairAccept（design/backend/broken.md）。
const TrailerRepair = "Logiforge-Repair"

// 直すための apply request の種類。
const (
	RepairRevert = "revert"
	RepairAccept = "accept"
)

// BuildMain は、main のコミットの記録とスナップショットツリーを、一度に作る。parent は、first-parent のスナップショット（最初のコミットでは nil）。
// files は、そのコミットのファイルである。artifact は中身か SHA256 を持つ。
// 親から変わったパスは、親のスナップショットツリーのファイルと比べて求める。新しく破れた不変条件も、親と比べて警告にする。
// worker は、Index と BuildSnapshotTree で、コミットを 1 つずつ取り込む（design/backend/worker.md）。これは、その 1 コミット分をまとめたものである。
func BuildMain(files tree.Files, info CommitInfo, parent *Main) (*Main, error) {
	x := NewIndex()
	var parentFiles tree.Files
	var parentBroken []tree.Violation
	if parent != nil {
		x = IndexFromSnapshotTree(parent.Commit, &parent.SnapshotTree)
		var err error
		if parentFiles, err = parent.SnapshotTree.Files(); err != nil {
			return nil, err
		}
		parentBroken = parent.SnapshotTree.Broken
	}
	c := x.Record(info, tree.ChangedPaths(parentFiles, files))
	t := BuildSnapshotTree(files, x)
	newly, _ := tree.NewlyBroken(parentBroken, t.Broken)
	x.AddNewlyBroken(&c, newly...)
	t.Warned = x.Warned()
	return &Main{FormatVersion: FormatVersion, Commit: c, SnapshotTree: *t}, nil
}

// BuildBranch は、ブランチの先頭のコミットのスナップショットを作る。base は main と分かれた点のコミットの SHA である。
// deltas は base から先頭への差分、head は、先頭での、加わったか変わったファイルだけでよい（gitx の ChangedFiles）。
// base の木の全体は読まない。
func BuildBranch(baseSHA string, deltas []Delta, head tree.Files, info CommitInfo) (*Branch, error) {
	b := &Branch{
		FormatVersion: FormatVersion,
		Commit:        Commit{Revision: info.Revision, Parent: &baseSHA, Kind: KindBranch, CommittedAt: info.CommittedAt.UTC(), Trailers: info.Trailers},
		Diff:          Diff{Base: baseSHA, Entries: []DiffEntry{}},
	}
	sorted := append([]Delta(nil), deltas...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, d := range sorted {
		e := DiffEntry{Path: d.Path, Status: d.Status, BaseSHA: d.BaseSHA}
		if d.Status != Deleted {
			hf, ok := head[d.Path]
			if !ok {
				return nil, fmt.Errorf("snapshot: %s の先頭のファイルがない", d.Path)
			}
			f := fromTreeFile(hf, strings.HasPrefix(d.Path, tree.ArtifactsDir+"/"))
			e.File = &f
		}
		b.Diff.Entries = append(b.Diff.Entries, e)
		b.Commit.Paths = append(b.Commit.Paths, PathChange{Path: d.Path, Status: d.Status})
	}
	return b, nil
}

// SubtreeChangedAfter は、nodeID のノードか、その子孫のどれかが、number より大きい番号の main のコミットで変わったかを返す。
// 部分木の引用で、引用元の変化を判定するために使う（design/backend/snapshot.md の「ノードを変えたコミット」）。
// ノードがこのスナップショットツリーになければ、変わったものとみなす。
func (t *SnapshotTree) SubtreeChangedAfter(nodeID string, number int) bool {
	if _, ok := t.Nodes[nodeID]; !ok {
		return true
	}
	children := map[string][]string{}
	for id, n := range t.Nodes {
		if n.Parent != "" {
			children[n.Parent] = append(children[n.Parent], id)
		}
	}
	stack := []string{nodeID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if t.Nodes[id].LastChanged.Number > number {
			return true
		}
		stack = append(stack, children[id]...)
	}
	return false
}

// Files は、main のスナップショットツリーを、tree.Files に戻す。artifact は中身を持たず、SHA256 を持つ。
// バックエンドは、これと Overlay で、差分の検査に渡す木を作る。
func (t *SnapshotTree) Files() (tree.Files, error) {
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

// Overlay は、base のファイルに、ブランチの差分を重ねたファイルを返す。base は、差分の base でなくてもよい（反映の直前の、今の main）。
// 差分が変えたパスが、差分の base のあとに base の側でも変わっていれば、重ねられないので、そのパスを conflicts に返す。
// git の merge と同じく、両方で同じ中身になったパスは、重ねられる。
//
// ファイルの単位で比べるので、行の単位で 3 方向の merge をする git より厳しい。
// ただし、4 種類の操作（ファイルを加える操作と、background.md の末尾への追加）では、git と同じ結果になる。
// 同じ background.md の末尾に両方で足すと、git でも衝突するからである。
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

// Apply は、base に差分 d を、衝突を判定せずに重ねる。差分が変えたパスは d のファイルを、ほかのパスは base のファイルを使う
// （design/backend/snapshot.md の「向き先に重ねる」の、向き先が main でないとき。「ブランチのコミットのスナップショットツリー」でも使う）。
func Apply(base tree.Files, d Diff) (tree.Files, error) {
	head := make(tree.Files, len(base))
	for p, f := range base {
		head[p] = f
	}
	for _, e := range d.Entries {
		if e.File == nil {
			delete(head, e.Path)
			continue
		}
		f, err := e.File.toTreeFile()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Path, err)
		}
		head[e.Path] = f
	}
	return head, nil
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
