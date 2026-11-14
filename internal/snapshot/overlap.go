package snapshot

import (
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// 反映のあとに、worker が確かめる警告（design/backend/worker.md の「すること」）。
const (
	// RuleOverlap は、反映した apply request の対象ノードが、その base のあとに、別の反映で変わっていたことである。
	RuleOverlap tree.Rule = "overlap"
	// RuleOverlapUnchecked は、反映した先頭のコミットか base が見つからず、重なりを確かめられなかったことである。
	RuleOverlapUnchecked tree.Rule = "overlap_unchecked"
	// RuleMainRewritten は、latest が新しい main の並びになく、main が書き換えられたことである。
	RuleMainRewritten tree.Rule = "main_rewritten"
)

// TrailerVersion は、反映のコミットの、反映した先頭のコミットの trailer である（例：`3 <commit SHA>`）。
const TrailerVersion = "Logiforge-Version"

// HasTrailer は、コミットの記録の trailer の値を返す。なければ空。
func HasTrailer(c Commit, key string) string { return trailer(c.Trailers, key) }

// VersionCommit は、反映のコミットの記録から、反映した先頭のコミットの SHA を返す。なければ空。
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
