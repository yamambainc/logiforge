package tree

import (
	"reflect"
	"strings"
	"testing"
)

func TestHypothesisRoundTrip(t *testing.T) {
	c := HypothesisContent{ID: H0, Statement: "主文: 「引用」と\nコロン"}
	got, err := ParseHypothesis(MarshalHypothesis(c))
	if err != nil || got != c {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestStoryRoundTrip(t *testing.T) {
	c := StoryContent{ID: S1, Title: "タイトル", Summary: "概要"}
	got, err := ParseStory(MarshalStory(c))
	if err != nil || got != c {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestVerdictRoundTrip(t *testing.T) {
	c := VerdictContent{
		ID: V1, Value: true, Supersedes: []string{V2},
		Artifacts: []ArtifactRef{{Kind: "link", Name: "paper", URI: "https://example.com/a"}},
		Citations: []Citation{{TargetKind: "subtree", NodeID: H1, Commit: strings.Repeat("a", 40)}},
		Report:    "# レポート\n\n本文\n",
	}
	got, err := ParseVerdict(MarshalVerdict(c))
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFirstVerdictHasNoSupersedes(t *testing.T) {
	out := string(MarshalVerdict(VerdictContent{ID: V1, Value: false, Report: "r"}))
	if strings.Contains(out, "supersedes") {
		t.Fatalf("supersedes を書いている: %s", out)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct {
		parse   func([]byte) error
		content string
	}{
		"決まっていない項目":         {func(b []byte) error { _, err := ParseHypothesis(b); return err }, "---\nid: h-0000000000\nstatement: a\nvalue: true\n---\n"},
		"front matter がない":  {func(b []byte) error { _, err := ParseStory(b); return err }, "本文だけ"},
		"閉じていない":            {func(b []byte) error { _, err := ParseStory(b); return err }, "---\nid: s\n"},
		"値が真偽値でない":          {func(b []byte) error { _, err := ParseVerdict(b); return err }, "---\nid: v-7777777777\nvalue: yes-ish\n---\nr"},
		"レポートがない":           {func(b []byte) error { _, err := ParseVerdict(b); return err }, "---\nid: v-7777777777\nvalue: true\n---\n"},
		"file の sha256 がない": {func(b []byte) error { _, err := ParseVerdict(b); return err }, "---\nid: v-7777777777\nvalue: true\nartifacts:\n  - kind: file\n    name: a\n---\nr"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if c.parse([]byte(c.content)) == nil {
				t.Fatal("誤りを返さなかった")
			}
		})
	}
}

func TestVerdictFieldChecks(t *testing.T) {
	cases := map[string]string{
		"artifact の name が空":     "artifacts:\n  - kind: link\n    name: \"\"\n    uri: https://example.com\n",
		"引用の node_id が識別子でない":    "citations:\n  - target_kind: subtree\n    node_id: x\n    revision: " + strings.Repeat("a", 40) + "\n",
		"引用の revision が SHA でない": "citations:\n  - target_kind: subtree\n    node_id: " + H1 + "\n    revision: main\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			src := "---\nid: " + V1 + "\nvalue: true\n" + extra + "---\nレポート\n"
			if _, err := ParseVerdict([]byte(src)); err == nil {
				t.Fatal("受け付けてしまった")
			}
		})
	}
}

func TestSupersedesForm(t *testing.T) {
	cases := map[string]string{
		"文字列":    "supersedes: " + V2 + "\n",
		"空の一覧":   "supersedes: []\n",
		"識別子でない": "supersedes: [x]\n",
		"重なり":    "supersedes: [" + V2 + ", " + V2 + "]\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			src := "---\nid: " + V1 + "\nvalue: true\n" + extra + "---\nレポート\n"
			if _, err := ParseVerdict([]byte(src)); err == nil {
				t.Fatal("受け付けてしまった")
			}
		})
	}
	src := "---\nid: " + V1 + "\nvalue: true\nsupersedes: [" + V2 + ", v-9999999999]\n---\nレポート\n"
	c, err := ParseVerdict([]byte(src))
	if err != nil || len(c.Supersedes) != 2 {
		t.Fatalf("got %+v, %v", c, err)
	}
}
