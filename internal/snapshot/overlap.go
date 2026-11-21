package snapshot

import (
	"github.com/yamambainc/logiforge/internal/tree"
)

// 反映のあとに、worker が確かめる警告（design/backend/worker/patrol.md の「すること」）。
const (
	// RuleOverlap は、反映した apply request の対象ノードが、その base のあとに、別の反映で変わっていたことである。
	RuleOverlap tree.Rule = "overlap"
	// RuleOverlapUnchecked は、反映した先頭のコミットか base が見つからず、重なりを確かめられなかったことである。
	RuleOverlapUnchecked tree.Rule = "overlap_unchecked"
)

// ApplyRequestOf は、main のコミットを merge のコミットに持つ pull request（apply request）の識別子を返す。なければ空。
// worker が、取り込むときに、反映した pull request の一覧で探したものである（design/backend/invalid-main.md の「どの apply request の反映か」）。
func ApplyRequestOf(c Commit) string { return c.ApplyRequest }

// VersionCommit は、反映のコミットの記録から、反映した先頭のコミットの SHA を返す。なければ空。
func VersionCommit(c Commit) string { return c.MergedHead }
