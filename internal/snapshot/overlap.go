package snapshot

import (
	"sort"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// 反映のあとに、worker が確かめる警告（design/backend/worker.md の「すること」）。
const (
	// RuleOverlap は、反映した apply request の対象ノードが、その base のあとに、別の反映で変わっていたことである。
	RuleOverlap tree.Rule = "overlap"
	// RuleOverlapUnchecked は、版のコミットか base が見つからず、重なりを確かめられなかったことである。
	RuleOverlapUnchecked tree.Rule = "overlap_unchecked"
)

// TrailerVersion は、反映のコミットの、反映した版の trailer である（例：`3 <commit SHA>`）。
const TrailerVersion = "Logiforge-Version"

// HasTrailer は、コミットの記録の trailer の値を返す。なければ空。
func HasTrailer(c Commit, key string) string { return trailer(c.Trailers, key) }

// VersionCommit は、反映のコミットの記録から、反映した版のコミットの SHA を返す。なければ空。
func VersionCommit(c Commit) string {
	f := strings.Fields(trailer(c.Trailers, TrailerVersion))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// HistoryNumber は、main のコミットの番号を、この木の history から引く。なければ 0。
func (t *Tree) HistoryNumber(revision string) int {
	for _, h := range t.History {
		if h.Revision == revision {
			return h.Number
		}
	}
	return 0
}

// targets は、このコミットで反映した操作の対象ノードを、変わったファイルから求める（design/backend/merge.md の「対象ノードの変化」）。
//   - 検証結果と背景情報：その仮説
//   - 新しいストーリー：親の仮説
//   - inactive 化：そのストーリー
func (m *Main) targets() []string {
	set := map[string]bool{}
	for _, c := range m.Commit.Changes {
		n := m.Tree.Nodes[c.Node]
		if n == nil {
			continue
		}
		switch c.File {
		case FileVerdict, FileBackground:
			set[n.ID] = true
		case FileInactivated:
			set[n.ID] = true
		case FileStory:
			if len(n.Changes) == 1 && n.Changes[0].Number == m.Commit.Number && n.Parent != "" {
				set[n.Parent] = true
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

// OverlapWarnings は、このコミット（反映のコミット）の対象ノードが、base（番号 baseNumber の main のコミット）のあとに、
// このコミットより前の別の反映で変わっていれば、警告を返す。このコミット自身は除く。
func (m *Main) OverlapWarnings(baseNumber int) []tree.Violation {
	var out []tree.Violation
	for _, id := range m.targets() {
		for _, r := range m.Tree.Nodes[id].Changes {
			if r.Number > baseNumber && r.Number < m.Commit.Number {
				out = append(out, tree.Violation{Rule: RuleOverlap, Node: id, Detail: "対象ノードが、base のあとに " + r.Revision + " で変わっていた"})
				break
			}
		}
	}
	return out
}

// AddWarnings は、コミットの警告を足し、history の印も合わせる。
func (m *Main) AddWarnings(vs ...tree.Violation) {
	if len(vs) == 0 {
		return
	}
	m.Commit.Warnings = append(m.Commit.Warnings, vs...)
	last := &m.Tree.History[len(m.Tree.History)-1]
	last.WorkerWarned = true
}
