package tree

import (
	"fmt"
	"sort"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Rule は、検査の規則の名前である（design/backend/checks.md）。
type Rule string

// ファイルの形の規則。
const (
	RuleParse          Rule = "parse"
	RuleNotRegularFile Rule = "not_regular_file"
	RuleTooLarge       Rule = "too_large"
	RuleOutsideTree    Rule = "outside_tree"
	RuleArtifactName   Rule = "artifact_name"
	RuleDuplicateID    Rule = "duplicate_id"
)

// 木の形の規則（head の木で確かめること）。
const (
	RuleAlternation            Rule = "alternation"
	RuleDepth                  Rule = "depth"
	RuleSingleRoot             Rule = "single_root"
	RuleStoryWithoutHypothesis Rule = "story_without_hypothesis"
	RuleLength                 Rule = "length"
	RuleMultipleValidVerdicts  Rule = "multiple_valid_verdicts"
	RuleVerdictWithActiveStory Rule = "verdict_with_active_story"
	RuleSupersedes             Rule = "supersedes"
)

// 差分の規則。
const (
	RuleChangeNotAllowed     Rule = "change_not_allowed"
	RuleBackgroundNotAppend  Rule = "background_not_append"
	RuleStorySetChanged      Rule = "story_set_changed"
	RuleIDCollision          Rule = "id_collision"
	RuleStoryUnderVerdict    Rule = "story_under_verdict"
	RuleStoryUnderInactive   Rule = "story_under_inactive"
	RuleVerdictInInactive    Rule = "verdict_in_inactive"
	RuleSupersedesNotValid   Rule = "supersedes_not_valid"
	RuleArtifactUnreferenced Rule = "artifact_unreferenced"
	RuleArtifactCitation     Rule = "artifact_citation"
	RuleSubtreeCitation      Rule = "subtree_citation"
)

// 上限（docs/tree/README.md と design/files/parent-child.md）。
const (
	MaxDepth          = 24
	MaxStatementChars = 50
	MaxTitleChars     = 50
	MaxSummaryChars   = 300
)

// Violation は、破れた規則である。Rule と Node の組で、同じ違反かを見分ける。
type Violation struct {
	Rule   Rule   `json:"rule"`
	Node   string `json:"node"`
	Detail string `json:"detail"`
	// IDs は、数を数える規則で、問題になっている識別子である（整列済み）。
	// base でも破れていた違反で、head の IDs の数が base より多ければ、新しく破ったものとみなす。
	IDs []string `json:"ids,omitempty"`
}

func (v Violation) key() string { return string(v.Rule) + "\x00" + v.Node }

// CharCount は、Unicode NFC に正規化したあとのコードポイント数を返す（docs/tree/README.md）。
func CharCount(s string) int { return utf8.RuneCountInString(norm.NFC.String(s)) }

// Check は、木の不変条件を確かめ、破れたものをすべて返す。ファイルの形の誤り（Problems）も含める。
func Check(t *Tree) []Violation {
	out := append([]Violation(nil), t.Problems...)
	add := func(r Rule, node, format string, args ...any) {
		out = append(out, Violation{Rule: r, Node: node, Detail: fmt.Sprintf(format, args...)})
	}
	addIDs := func(r Rule, node string, ids []string, format string, args ...any) {
		sort.Strings(ids)
		out = append(out, Violation{Rule: r, Node: node, Detail: fmt.Sprintf(format, args...), IDs: ids})
	}
	if len(t.Roots) != 1 {
		var roots []string
		for _, r := range t.Roots {
			roots = append(roots, r.ID)
		}
		addIDs(RuleSingleRoot, TreeDir, roots, "根の仮説が %d 個ある", len(t.Roots))
	}
	for _, id := range sortedKeys(t.Hypotheses) {
		h := t.Hypotheses[id]
		if h.Depth > MaxDepth {
			add(RuleDepth, id, "仮説が %d 段目にある", h.Depth)
		}
		if n := CharCount(h.Statement); n > MaxStatementChars {
			add(RuleLength, id, "主文が %d 字ある", n)
		}
		valid := h.ValidVerdicts()
		if len(valid) > 1 {
			addIDs(RuleMultipleValidVerdicts, id, verdictIDs(valid), "有効な検証結果が %d 個ある", len(valid))
		}
		if active := h.ActiveStories(); len(valid) > 0 && len(active) > 0 {
			ids := verdictIDs(valid)
			for _, s := range active {
				ids = append(ids, s.ID)
			}
			addIDs(RuleVerdictWithActiveStory, id, ids, "検証結果と active な子ストーリーを同時に持つ")
		}
		for _, v := range h.Verdicts {
			for _, sid := range v.Supersedes {
				target, ok := t.Verdicts[sid]
				if !ok || target.Hypothesis != h {
					add(RuleSupersedes, v.ID, "supersedes の %s が、同じ仮説の検証結果を指していない", sid)
				}
			}
		}
	}
	for _, id := range sortedKeys(t.Stories) {
		s := t.Stories[id]
		if len(s.Hypotheses) == 0 {
			add(RuleStoryWithoutHypothesis, id, "子仮説がない")
		}
		if n := CharCount(s.Title); n > MaxTitleChars {
			add(RuleLength, id, "タイトルが %d 字ある", n)
		}
		if n := CharCount(s.Summary); n > MaxSummaryChars {
			add(RuleLength, id, "概要が %d 字ある", n)
		}
	}
	return out
}

func verdictIDs(vs []*Verdict) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.ID)
	}
	return out
}

// NewlyBroken は、head で破れていて、base では破れていなかったものを返す。比べる単位は「規則 × ノード」である。
// base でも破れていても、数を数える規則で、問題の識別子の数が base より多ければ、悪くなったので新しく破ったものとする。
// もう 1 つの戻り値は、base でもすでに破れていて、悪くなっていないもので、警告にする。
// 数で比べるのは、有効な検証結果を差し替えて直す差分（{V1, V2} → {V1, V3}）を拒まないためである。
func NewlyBroken(base, head []Violation) (newly, existing []Violation) {
	count := map[string]int{}
	for _, v := range base {
		if n, ok := count[v.key()]; !ok || len(v.IDs) > n {
			count[v.key()] = len(v.IDs)
		}
	}
	for _, v := range head {
		n, ok := count[v.key()]
		if ok && len(v.IDs) <= n {
			existing = append(existing, v)
		} else {
			newly = append(newly, v)
		}
	}
	return newly, existing
}
