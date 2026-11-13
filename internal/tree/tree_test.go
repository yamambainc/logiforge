package tree

import "testing"

func TestBuildAlternatingTree(t *testing.T) {
	b, _ := base()
	tr := Build(b.files)
	if len(tr.Problems) != 0 {
		t.Fatalf("problems: %+v", tr.Problems)
	}
	h1 := tr.Hypotheses[H1]
	if tr.Root().ID != H0 || h1.Depth != 2 || h1.Parent.ID != S1 {
		t.Fatalf("木の形が違う: %+v", h1)
	}
}

func TestBuildProblems(t *testing.T) {
	cases := map[string]struct {
		mutate func(b *fb, d map[string]string)
		rule   Rule
	}{
		"仮説の下に仮説":  {func(b *fb, d map[string]string) { b.hyp(d[H0], H3, "x") }, RuleAlternation},
		"木の外のファイル": {func(b *fb, _ map[string]string) { b.put(".github/workflows/x.yml", []byte("x")) }, RuleOutsideTree},
		"シンボリックリンク": {func(b *fb, d map[string]string) {
			b.files[d[H1]+"/background.md"] = File{Path: d[H1] + "/background.md", Mode: "120000", Size: 3, Content: []byte("../")}
		}, RuleNotRegularFile},
		"大きすぎて読まなかった": {func(b *fb, d map[string]string) {
			b.files[d[H1]+"/background.md"] = File{Path: d[H1] + "/background.md", Mode: RegularMode, Size: 1 << 30}
		}, RuleTooLarge},
		"artifact の名前が中身と違う": {func(b *fb, _ map[string]string) {
			b.put(ArtifactPath(SHA256Hex([]byte("a"))), []byte("b"))
		}, RuleArtifactName},
		"id がディレクトリ名と違う": {func(b *fb, d map[string]string) {
			b.put(d[S1]+"/"+StoryFile, MarshalStory(StoryContent{ID: S2, Title: "t", Summary: "s"}))
		}, RuleParse},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, d := base()
			c.mutate(b, d)
			if tr := Build(b.files); !hasRule(tr.Problems, c.rule) {
				t.Fatalf("want %s, got %v", c.rule, rules(tr.Problems))
			}
		})
	}
}

func TestInactiveAndValidVerdicts(t *testing.T) {
	b, d := base()
	b.verdict(d[H1], V1, true, "")
	b.verdict(d[H1], V2, false, V1)
	b.inactivate(d[S1], "論理に欠陥")
	tr := Build(b.files)
	h1 := tr.Hypotheses[H1]
	if !HypothesisInactive(h1) || HypothesisInactive(tr.Hypotheses[H0]) {
		t.Fatal("inactive の判定が違う")
	}
	if v := h1.ValidVerdicts(); len(v) != 1 || v[0].ID != V2 {
		t.Fatalf("有効な検証結果が違う: %+v", v)
	}
}
