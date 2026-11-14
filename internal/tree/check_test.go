package tree

import (
	"strings"
	"testing"
)

func TestCheckInvariants(t *testing.T) {
	cases := map[string]struct {
		mutate func(b *fb, d map[string]string)
		rule   Rule
	}{
		"主文が 51 字": {func(b *fb, d map[string]string) { b.hyp(d[S1], H3, strings.Repeat("あ", 51)) }, RuleLength},
		"有効な検証結果が 2 つ": {func(b *fb, d map[string]string) {
			b.verdict(d[H1], V1, true, "")
			b.verdict(d[H1], V2, false, "")
		}, RuleMultipleValidVerdicts},
		"検証結果と active な子ストーリー": {func(b *fb, d map[string]string) { b.verdict(d[H0], V1, true, "") }, RuleVerdictWithActiveStory},
		"子仮説のないストーリー":          {func(b *fb, d map[string]string) { b.story(d[H1], S2, "空") }, RuleStoryWithoutHypothesis},
		"根が 2 つ":               {func(b *fb, _ map[string]string) { b.hyp(TreeDir, H5, "もう 1 つの根") }, RuleSingleRoot},
		"supersedes が別の仮説": {func(b *fb, d map[string]string) {
			b.verdict(d[H1], V1, true, "")
			b.verdict(d[H2], V2, true, V1)
		}, RuleSupersedes},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, d := base()
			c.mutate(b, d)
			if vs := Check(Build(b.files)); !hasRule(vs, c.rule) {
				t.Fatalf("want %s, got %v", c.rule, rules(vs))
			}
		})
	}
}

func TestCharCountUsesNFC(t *testing.T) {
	// 「が」を、か ＋ 濁点の結合文字で書くと、NFC で 1 文字になる。
	decomposed := strings.Repeat("が", 50)
	if n := CharCount(decomposed); n != 50 {
		t.Fatalf("got %d", n)
	}
}

func TestDepthLimit(t *testing.T) {
	b := newFB()
	dir := b.hyp(TreeDir, "h-0000000000", "根")
	for i := 1; i <= MaxDepth; i++ {
		sid := "s-" + pad(i)
		hid := "h-" + pad(i)
		dir = b.hyp(b.story(dir, sid, "t"), hid, "x")
	}
	vs := Check(Build(b.files))
	if !hasRule(vs, RuleDepth) {
		t.Fatalf("25 段目を見逃した: %v", rules(vs))
	}
}

func pad(i int) string {
	const digits = "0123456789"
	s := ""
	for n := i; n > 0; n /= 10 {
		s = string(digits[n%10]) + s
	}
	return strings.Repeat("0", 10-len(s)) + s
}

func TestNewlyBrokenComparesRuleAndNode(t *testing.T) {
	base := []Violation{{Rule: RuleLength, Node: H1}}
	head := []Violation{{Rule: RuleLength, Node: H1}, {Rule: RuleLength, Node: H2}}
	newly, existing := NewlyBroken(base, head)
	if len(newly) != 1 || newly[0].Node != H2 || len(existing) != 1 {
		t.Fatalf("newly=%v existing=%v", newly, existing)
	}
}
