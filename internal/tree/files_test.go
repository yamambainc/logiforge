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
		ID: V1, Value: true, Supersedes: V2,
		Artifacts: []ArtifactRef{{Kind: "link", Name: "paper", URI: "https://example.com/a"}},
		Citations: []Citation{{TargetKind: "subtree", NodeID: H1, Revision: strings.Repeat("a", 40)}},
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
