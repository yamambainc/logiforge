package snapshot

import (
	"path"
	"sort"
	"strings"

	"github.com/yamambainc/logiforge/internal/ids"
	"github.com/yamambainc/logiforge/internal/tree"
)

// Index は、main のコミットを古い順に 1 つずつ取り込むときの、直前のコミットまでの状態である
// （design/backend/snapshot.md の「すべてのコミットに作る」）。
// 直前のコミットのスナップショットツリーから作り（IndexFromSnapshotTree）、Record でコミットを 1 つ足し、BuildSnapshotTree でそのコミットのスナップショットツリーを作る。
type Index struct {
	// head は、最後に足したコミットである。まだなければ番号は 0。
	head Rev
	// last は、ノードごとの、そのノードを最後に変えたコミットである。
	last map[string]Rev
	// inactivatedAt は、ストーリーごとの、inactivated.md が加わったコミットの番号である。
	inactivatedAt map[string]int
	// verdictAdded は、検証結果のファイルのパスごとの、そのファイルが加わったコミットである。
	verdictAdded map[string]AddedIn
	// nodeDirs は、これまでに見たノードのディレクトリから、ノードの識別子を引く。
	// inactive 化で「変わった」とみなす子孫を、パスの接頭辞で求めるために使う。
	nodeDirs map[string]string
	// warned は、警告のあるコミットの一覧である（番号の順）。
	warned []WarnedEntry
}

// NewIndex は、空の Index を返す。最初のコミットから取り込むときに使う。
func NewIndex() *Index {
	return &Index{last: map[string]Rev{}, inactivatedAt: map[string]int{}, verdictAdded: map[string]AddedIn{}, nodeDirs: map[string]string{}}
}

// IndexFromSnapshotTree は、main のコミット head のスナップショットツリーから、そのコミットまでの Index を作る。
func IndexFromSnapshotTree(head Commit, t *SnapshotTree) *Index {
	x := NewIndex()
	x.head = Rev{Number: head.Number, Revision: head.Revision}
	x.warned = append([]WarnedEntry(nil), t.Warned...)
	for id, n := range t.Nodes {
		x.nodeDirs[n.Dir] = id
		x.last[id] = n.LastChanged
		if n.InactivatedAt > 0 {
			x.inactivatedAt[id] = n.InactivatedAt
		}
		for _, f := range n.Files {
			if f.Added != nil {
				x.verdictAdded[f.Path] = *f.Added
			}
		}
	}
	return x
}

// Head は、最後に足したコミットを返す。まだなければ番号は 0。
func (x *Index) Head() Rev { return x.head }

// Record は、main のコミットの記録を、親から変わったパス（git の diff-tree の結果）だけで作り、Index に足す。
// 警告は、反映の記録がないことだけを付ける。新しく破れた不変条件と、worker の警告は、AddNewlyBroken と AddWorkerWarnings で足す。
func (x *Index) Record(info CommitInfo, changedPaths []string) Commit {
	c := Commit{Revision: info.Revision, Number: x.head.Number + 1, Kind: KindMain, CommittedAt: info.CommittedAt.UTC(), Trailers: info.Trailers}
	if x.head.Number > 0 {
		p := x.head.Revision
		c.Parent = &p
	}
	files := map[string][]FileChange{}
	paths := append([]string(nil), changedPaths...)
	sort.Strings(paths)
	for _, p := range paths {
		if node, kind := classify(p); kind != "" {
			files[node] = append(files[node], FileChange{File: kind, Path: p})
			x.nodeDirs[nodeDir(p, kind)] = node
		}
	}
	rev := Rev{Number: c.Number, Revision: c.Revision}
	ar := trailer(info.Trailers, TrailerApplyRequest)
	if info.ApplyRequest != "" {
		ar = info.ApplyRequest
		c.ApplyRequest = ar
	}
	for _, id := range sortedKeys(x.affected(files)) {
		ch := NodeChange{Node: id, Files: files[id]}
		if prev, ok := x.last[id]; ok {
			ch.Previous = &prev
		}
		c.Changes = append(c.Changes, ch)
		x.last[id] = rev
		// 加わったコミットは、まだ値がないときだけ書く。書き直したコミットに動かさない。消えたら、BuildSnapshotTree で消す。
		for _, f := range ch.Files {
			switch f.File {
			case FileInactivated:
				if _, ok := x.inactivatedAt[id]; !ok {
					x.inactivatedAt[id] = c.Number
				}
			case FileVerdict:
				if _, ok := x.verdictAdded[f.Path]; !ok {
					x.verdictAdded[f.Path] = AddedIn{Number: c.Number, Revision: c.Revision, ApplyRequest: ar}
				}
			}
		}
	}
	x.head = rev
	unauthorized := ar == "" && c.Parent != nil && trailer(info.Trailers, TrailerInit) == ""
	if info.Authorized != nil {
		unauthorized = !*info.Authorized
	}
	if unauthorized {
		detail := "反映の記録のないコミット"
		if info.AuthorizationDetail != "" {
			detail = "反映によるものでない：" + info.AuthorizationDetail
		}
		x.warn(&c, func(e *WarnedEntry) { e.NoApplyRequest = true },
			tree.Violation{Rule: RuleNoApplyRequest, Node: info.Revision, Detail: detail})
	}
	return c
}

// affected は、ノードのファイルの変化から、「変わった」とみなすノードを求める（design/backend/merge.md の「対象ノードの変化」）。
//   - 仮説のファイル：その仮説
//   - story.md：そのストーリーと、親の仮説
//   - inactivated.md：そのストーリーと、親の仮説と、子孫のすべてのノード
func (x *Index) affected(files map[string][]FileChange) map[string]bool {
	out := map[string]bool{}
	for node, fs := range files {
		out[node] = true
		for _, f := range fs {
			if f.File != FileStory && f.File != FileInactivated {
				continue
			}
			dir := nodeDir(f.Path, f.File)
			if parent := path.Base(path.Dir(dir)); ids.HypothesisPattern.MatchString(parent) {
				out[parent] = true
			}
			if f.File == FileInactivated {
				for d, id := range x.nodeDirs {
					if strings.HasPrefix(d, dir+"/") {
						out[id] = true
					}
				}
			}
		}
	}
	return out
}

// warn は、最後に足したコミット c に警告を足し、警告の一覧の c の項目に印を付ける。
func (x *Index) warn(c *Commit, mark func(*WarnedEntry), vs ...tree.Violation) {
	if len(vs) == 0 {
		return
	}
	c.Warnings = append(c.Warnings, vs...)
	if n := len(x.warned); n == 0 || x.warned[n-1].Number != c.Number {
		x.warned = append(x.warned, WarnedEntry{Number: c.Number, Revision: c.Revision})
	}
	mark(&x.warned[len(x.warned)-1])
}

// AddNewlyBroken は、このコミットで新しく破れた不変条件を、最後に足したコミットの警告にする。
func (x *Index) AddNewlyBroken(c *Commit, vs ...tree.Violation) {
	x.warn(c, func(e *WarnedEntry) { e.NewlyBroken = true }, vs...)
}

// AddWorkerWarnings は、対象ノードの重なりや main の書き換えなど、worker が見つけた警告を、最後に足したコミットの警告にする。
func (x *Index) AddWorkerWarnings(c *Commit, vs ...tree.Violation) {
	x.warn(c, func(e *WarnedEntry) { e.WorkerWarned = true }, vs...)
}

// Warned は、警告のあるコミットの一覧を返す。スナップショットツリーの Warned に入れる。
func (x *Index) Warned() []WarnedEntry { return append([]WarnedEntry(nil), x.warned...) }

// targets は、このコミットで反映した操作の対象ノードを、変わったファイルから求める（design/backend/merge.md の「対象ノードの変化」）。
//   - 検証結果と背景情報：その仮説
//   - 新しいストーリー：親の仮説
//   - inactive 化：そのストーリー
//
// 対象ノードごとに、そのノードを前に変えたコミット（c より前）を返す。
func targets(c Commit) map[string]*Rev {
	prev := map[string]*Rev{}
	for _, ch := range c.Changes {
		prev[ch.Node] = ch.Previous
	}
	out := map[string]*Rev{}
	for _, ch := range c.Changes {
		for _, f := range ch.Files {
			switch f.File {
			case FileVerdict, FileBackground, FileInactivated:
				out[ch.Node] = ch.Previous
			case FileStory:
				// 新しいストーリーは、前に変えたコミットがない。
				parent := path.Base(path.Dir(nodeDir(f.Path, f.File)))
				if ch.Previous == nil && ids.HypothesisPattern.MatchString(parent) {
					out[parent] = prev[parent]
				}
			}
		}
	}
	return out
}

// OverlapWarnings は、反映のコミット c の対象ノードが、base（番号 baseNumber の main のコミット）のあとに、
// c より前の別のコミットで変わっていれば、警告を返す（design/backend/worker.md の「確かめること」）。
func OverlapWarnings(c Commit, baseNumber int) []tree.Violation {
	ts := targets(c)
	var out []tree.Violation
	for _, id := range sortedKeys(ts) {
		if r := ts[id]; r != nil && r.Number > baseNumber {
			out = append(out, tree.Violation{Rule: RuleOverlap, Node: id, Detail: "対象ノードが、base のあとに " + r.Revision + " で変わっていた"})
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

// nodeDir は、ノードのファイルのパスから、ノードのディレクトリを返す。
func nodeDir(p string, kind FileKind) string {
	d := path.Dir(p)
	if kind == FileVerdict {
		d = path.Dir(d)
	}
	return d
}

// prune は、ファイルが消えた inactivated.md と検証結果の、加わったコミットを消す。また加わったら、そのコミットを書くため。
func (x *Index) prune(files tree.Files) {
	for p := range x.verdictAdded {
		if _, ok := files[p]; !ok {
			delete(x.verdictAdded, p)
		}
	}
	dirs := map[string]string{}
	for d, id := range x.nodeDirs {
		dirs[id] = d
	}
	for id := range x.inactivatedAt {
		if _, ok := files[dirs[id]+"/"+tree.InactivatedFile]; !ok {
			delete(x.inactivatedAt, id)
		}
	}
}

// BuildSnapshotTree は、main のコミットのファイルと、そのコミットまでを足した Index から、スナップショットツリーを作る。
// ノードを最後に変えたコミット、inactivated.md と検証結果が加わったコミット、警告のあるコミットの一覧は、Index から写す。破れている不変条件は、組み立てた木から求める。
// 木を組み立てるのは、ここの 1 回だけである。
func BuildSnapshotTree(files tree.Files, x *Index) *SnapshotTree {
	return BuildSnapshotTreeOf(tree.Build(files), files, x)
}

// BuildSnapshotTreeOf は、組み立て済みの木 t（files から tree.Build で作ったもの）から、スナップショットツリーを作る。
// 木を組み立て直さないので、呼ぶ側も t を使うとき（バックエンドの、反映したあとの木）に使う。
func BuildSnapshotTreeOf(t *tree.Tree, files tree.Files, x *Index) *SnapshotTree {
	x.prune(files)
	vals := tree.Compute(t)
	out := &SnapshotTree{Nodes: map[string]*Node{}, Warned: x.Warned()}
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
			Inactive: tree.StoryInactive(s), Evidence: vals.Evidence[id], InactivatedAt: x.inactivatedAt[id]}
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
			sf := fromTreeFile(f, false)
			if a, ok := x.verdictAdded[p]; ok {
				sf.Added = &a
			}
			n.Files = append(n.Files, sf)
		default:
			out.Other = append(out.Other, fromTreeFile(f, false))
		}
	}
	for id, n := range out.Nodes {
		n.LastChanged = x.last[id]
	}
	out.Broken = tree.Check(t)
	return out
}
