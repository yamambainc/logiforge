package snapshot

import (
	"strings"

	"github.com/yamambainc/logiforge/internal/tree"
)

// 反映のあとに、worker が確かめる警告（design/backend/worker/patrol.md の「すること」）。
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

// ApplyRequestOf は、main のコミットを反映した apply request の識別子を返す。反映によらないコミット（警告 no_apply_request）なら空。
// worker が判定で見つけたものがなければ、trailer（Logiforge-Apply-Request:）を使う。
func ApplyRequestOf(c Commit) string {
	for _, w := range c.Warnings {
		if w.Rule == RuleNoApplyRequest {
			return ""
		}
	}
	if c.ApplyRequest != "" {
		return c.ApplyRequest
	}
	return trailer(c.Trailers, TrailerApplyRequest)
}

// VersionCommit は、反映のコミットの記録から、反映した先頭のコミットの SHA を返す。なければ空。
func VersionCommit(c Commit) string {
	f := strings.Fields(trailer(c.Trailers, TrailerVersion))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}
