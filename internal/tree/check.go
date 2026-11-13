package tree

import (
	"fmt"
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
	if len(t.Roots) != 1 {
		add(RuleSingleRoot, TreeDir, "根の仮説が %d 個ある", len(t.Roots))
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
			add(RuleMultipleValidVerdicts, id, "有効な検証結果が %d 個ある", len(valid))
		}
		if len(valid) > 0 && len(h.ActiveStories()) > 0 {
			add(RuleVerdictWithActiveStory, id, "検証結果と active な子ストーリーを同時に持つ")
		}
		for _, v := range h.Verdicts {
			if v.Supersedes == "" {
				continue
			}
			target, ok := t.Verdicts[v.Supersedes]
			if !ok || target.Hypothesis != h {
				add(RuleSupersedes, v.ID, "supersedes が、同じ仮説の検証結果を指していない")
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

// NewlyBroken は、head で破れていて、base では破れていなかったものを返す。比べる単位は「規則 × ノード」である。
// もう 1 つの戻り値は、base でもすでに破れていたもので、警告にする。
func NewlyBroken(base, head []Violation) (newly, existing []Violation) {
	seen := map[string]bool{}
	for _, v := range base {
		seen[v.key()] = true
	}
	for _, v := range head {
		if seen[v.key()] {
			existing = append(existing, v)
		} else {
			newly = append(newly, v)
		}
	}
	return newly, existing
}
