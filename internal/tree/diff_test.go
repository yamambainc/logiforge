package tree

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func wantOK(t *testing.T, res Result) {
	t.Helper()
	if !res.OK() {
		t.Fatalf("拒まれた: %v", res.Rejected)
	}
}

func wantRejected(t *testing.T, res Result, r Rule) {
	t.Helper()
	if !hasRule(res.Rejected, r) {
		t.Fatalf("want %s, got %v", r, rules(res.Rejected))
	}
}

func opKinds(ops []Operation) []string {
	var out []string
	for _, op := range ops {
		out = append(out, op.Kind)
	}
	return out
}

func TestNoChange(t *testing.T) {
	b, _ := base()
	res := mustCheck(t, b, b.clone(), Context{})
	wantOK(t, res)
	if len(res.Operations) != 0 {
		t.Fatalf("ops: %v", res.Operations)
	}
}

func TestStoryCreate(t *testing.T) {
	b, d := base()
	h := b.clone()
	sd := h.story(d[H1], S2, "新しいストーリー")
	hd := h.hyp(sd, H3, "新しい子")
	h.put(hd+"/"+BackgroundFile, []byte("背景\n"))
	res := mustCheck(t, b, h, Context{})
	wantOK(t, res)
	want := []Operation{{
		Kind: OpStoryCreate, StoryID: S2, ParentHypothesisID: H1, Title: "新しいストーリー", Summary: "概要",
		Hypotheses: []NewHypothesis{{ID: H3, Statement: "新しい子", Background: "背景\n"}},
	}}
	if !reflect.DeepEqual(res.Operations, want) {
		t.Fatalf("got %+v", res.Operations)
	}
}

func TestStoryCreateRejected(t *testing.T) {
	cases := map[string]struct {
		prepare func(b *fb, d map[string]string) // base に足す
		change  func(h *fb, d map[string]string) // head に足す
		rule    Rule
	}{
		"検証結果を持つ仮説の下": {
			func(b *fb, d map[string]string) { b.verdict(d[H1], V1, true, "") },
			func(h *fb, d map[string]string) { h.hyp(h.story(d[H1], S2, "t"), H3, "x") },
			RuleStoryUnderVerdict,
		},
		"inactive な部分木の下": {
			func(b *fb, d map[string]string) { b.inactivate(d[S1], "理由") },
			func(h *fb, d map[string]string) { h.hyp(h.story(d[H1], S2, "t"), H3, "x") },
			RuleStoryUnderInactive,
		},
		"すでにあるストーリーに子仮説を足す": {
			func(*fb, map[string]string) {},
			func(h *fb, d map[string]string) { h.hyp(d[S1], H3, "x") },
			RuleStorySetChanged,
		},
		"子仮説のないストーリー": {
			func(*fb, map[string]string) {},
			func(h *fb, d map[string]string) { h.story(d[H1], S2, "t") },
			RuleStoryWithoutHypothesis,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, d := base()
			c.prepare(b, d)
			h := b.clone()
			c.change(h, d)
			wantRejected(t, mustCheck(t, b, h, Context{}), c.rule)
		})
	}
}

func TestStoryInactivate(t *testing.T) {
	b, d := base()
	h := b.clone()
	h.inactivate(d[S1], "理由\n")
	res := mustCheck(t, b, h, Context{})
	wantOK(t, res)
	want := []Operation{{Kind: OpStoryInactivate, StoryID: S1, Reason: "理由\n"}}
	if !reflect.DeepEqual(res.Operations, want) {
		t.Fatalf("got %+v", res.Operations)
	}
}

func TestVerdictSubmit(t *testing.T) {
	b, d := base()
	h := b.clone()
	h.verdict(d[H1], V1, true, "")
	res := mustCheck(t, b, h, Context{})
	wantOK(t, res)
	if got := opKinds(res.Operations); !reflect.DeepEqual(got, []string{OpVerdictSubmit}) {
		t.Fatalf("got %v", got)
	}
	if op := res.Operations[0]; op.HypothesisID != H1 || op.Verdict.ID != V1 || !op.Verdict.Value {
		t.Fatalf("got %+v", op)
	}
}

func TestVerdictSupersedes(t *testing.T) {
	b, d := base()
	b.verdict(d[H1], V1, true, "")

	t.Run("有効な検証結果を置き換える", func(t *testing.T) {
		h := b.clone()
		h.verdict(d[H1], V2, false, V1)
		wantOK(t, mustCheck(t, b, h, Context{}))
	})
	t.Run("ほかの仮説の検証結果を指す", func(t *testing.T) {
		h := b.clone()
		h.verdict(d[H2], V2, false, V1)
		wantRejected(t, mustCheck(t, b, h, Context{}), RuleSupersedesNotValid)
	})
	t.Run("同じ差分で加えた検証結果を指す", func(t *testing.T) {
		b2, d2 := base()
		h := b2.clone()
		h.verdict(d2[H1], V1, true, "")
		h.verdict(d2[H1], V2, false, V1)
		wantRejected(t, mustCheck(t, b2, h, Context{}), RuleSupersedesNotValid)
	})
}

func TestVerdictInInactive(t *testing.T) {
	t.Run("同じ差分で inactive にした、葉の仮説", func(t *testing.T) {
		b, d := base()
		h := b.clone()
		h.inactivate(d[S1], "理由")
		h.verdict(d[H1], V1, false, "")
		wantOK(t, mustCheck(t, b, h, Context{}))
	})
	t.Run("同じ差分で inactive にした、葉でない仮説", func(t *testing.T) {
		b, d := base()
		d[S2] = b.story(d[H1], S2, "孫のストーリー")
		b.hyp(d[S2], H3, "孫")
		h := b.clone()
		h.inactivate(d[S1], "理由")
		h.verdict(d[H1], V1, false, "")
		wantRejected(t, mustCheck(t, b, h, Context{}), RuleVerdictInInactive)
	})
	t.Run("前から inactive：WasLeafBeforeInactivation で決める", func(t *testing.T) {
		b, d := base()
		b.inactivate(d[S1], "理由")
		h := b.clone()
		h.verdict(d[H1], V1, false, "")
		h.verdict(d[H2], V2, false, "")
		var asked []string
		ctx := Context{WasLeafBeforeInactivation: func(id string) (bool, error) {
			asked = append(asked, id)
			return id == H1, nil
		}}
		res := mustCheck(t, b, h, ctx)
		if len(res.Rejected) != 1 || res.Rejected[0].Rule != RuleVerdictInInactive || res.Rejected[0].Node != V2 {
			t.Fatalf("got %v", res.Rejected)
		}
		if len(asked) != 2 {
			t.Fatalf("asked %v", asked)
		}
	})
	t.Run("問い合わせの誤りを返す", func(t *testing.T) {
		b, d := base()
		b.inactivate(d[S1], "理由")
		h := b.clone()
		h.verdict(d[H1], V1, false, "")
		boom := errors.New("boom")
		_, err := CheckChange(b.files, h.files, Context{WasLeafBeforeInactivation: func(string) (bool, error) { return false, boom }})
		if !errors.Is(err, boom) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestBackground(t *testing.T) {
	b, d := base()
	b.put(d[H1]+"/"+BackgroundFile, []byte("一行目\n"))

	t.Run("末尾への追加", func(t *testing.T) {
		h := b.clone()
		h.put(d[H1]+"/"+BackgroundFile, []byte("一行目\n二行目\n"))
		res := mustCheck(t, b, h, Context{})
		wantOK(t, res)
		want := []Operation{{Kind: OpNodeEdit, HypothesisID: H1, AppendBackground: "二行目\n"}}
		if !reflect.DeepEqual(res.Operations, want) {
			t.Fatalf("got %+v", res.Operations)
		}
	})
	t.Run("書き換え", func(t *testing.T) {
		h := b.clone()
		h.put(d[H1]+"/"+BackgroundFile, []byte("書き換えた\n"))
		wantRejected(t, mustCheck(t, b, h, Context{}), RuleBackgroundNotAppend)
	})
	t.Run("ないところに足す", func(t *testing.T) {
		h := b.clone()
		h.put(d[H2]+"/"+BackgroundFile, []byte("新しい背景\n"))
		res := mustCheck(t, b, h, Context{})
		wantOK(t, res)
		if res.Operations[0].AppendBackground != "新しい背景\n" {
			t.Fatalf("got %+v", res.Operations)
		}
	})
}

func TestArtifacts(t *testing.T) {
	t.Run("検証結果と一緒に加える", func(t *testing.T) {
		b, d := base()
		h := b.clone()
		a := h.artifact("data")
		h.verdict(d[H1], V1, true, "", a)
		wantOK(t, mustCheck(t, b, h, Context{}))
	})
	t.Run("参照されない artifact", func(t *testing.T) {
		b, _ := base()
		h := b.clone()
		h.artifact("data")
		wantRejected(t, mustCheck(t, b, h, Context{}), RuleArtifactUnreferenced)
	})
	t.Run("参照した artifact のファイルがない", func(t *testing.T) {
		b, d := base()
		h := b.clone()
		h.verdict(d[H1], V1, true, "", ArtifactRef{Kind: "file", Name: "x", SHA256: SHA256Hex([]byte("none"))})
		wantRejected(t, mustCheck(t, b, h, Context{}), RuleArtifactCitation)
	})
	t.Run("base にある artifact を引用する", func(t *testing.T) {
		b, d := base()
		a := b.artifact("old")
		b.verdict(d[H2], V2, true, "", a)
		h := b.clone()
		v := VerdictContent{ID: V1, Value: true, Report: "r", Citations: []Citation{{TargetKind: "artifact", SHA256: a.SHA256}}}
		h.put(d[H1]+"/"+VerdictsDir+"/"+V1+".md", MarshalVerdict(v))
		wantOK(t, mustCheck(t, b, h, Context{}))
	})
	t.Run("ない artifact を引用する", func(t *testing.T) {
		b, d := base()
		h := b.clone()
		v := VerdictContent{ID: V1, Value: true, Report: "r", Citations: []Citation{{TargetKind: "artifact", SHA256: SHA256Hex([]byte("none"))}}}
		h.put(d[H1]+"/"+VerdictsDir+"/"+V1+".md", MarshalVerdict(v))
		wantRejected(t, mustCheck(t, b, h, Context{}), RuleArtifactCitation)
	})
}

func TestSubtreeCitation(t *testing.T) {
	b, d := base()
	rev := strings.Repeat("a", 40)
	h := b.clone()
	v := VerdictContent{ID: V1, Value: true, Report: "r", Citations: []Citation{{TargetKind: "subtree", NodeID: H2, Commit: rev}}}
	h.put(d[H1]+"/"+VerdictsDir+"/"+V1+".md", MarshalVerdict(v))

	exists := func(want bool) Context {
		return Context{SubtreeExists: func(node, r string) (bool, error) { return want && node == H2 && r == rev, nil }}
	}
	wantOK(t, mustCheck(t, b, h, exists(true)))
	wantRejected(t, mustCheck(t, b, h, exists(false)), RuleSubtreeCitation)
	wantOK(t, mustCheck(t, b, h, Context{})) // 問い合わせがなければ省く
}

func TestIDCollisionWithOtherTree(t *testing.T) {
	b, d := base()
	h := b.clone()
	h.hyp(h.story(d[H1], S2, "t"), H3, "x")

	other, od := base()
	other.hyp(other.story(od[H2], S3, "t"), H3, "main で先に使われた")
	res := mustCheck(t, b, h, Context{Others: []*Tree{Build(other.files)}})
	if len(res.Rejected) != 1 || res.Rejected[0].Rule != RuleIDCollision || res.Rejected[0].Node != H3 {
		t.Fatalf("got %v", res.Rejected)
	}
}

func TestChangeNotAllowed(t *testing.T) {
	none := func(*fb, map[string]string) {}
	cases := map[string]struct {
		prepare func(b *fb, d map[string]string) // base に足す
		change  func(h *fb, d map[string]string) // head に足す
	}{
		"主文の書き換え": {none, func(h *fb, d map[string]string) {
			h.put(d[H1]+"/"+HypothesisFile, MarshalHypothesis(HypothesisContent{ID: H1, Statement: "書き換えた"}))
		}},
		"ストーリーの削除": {none, func(h *fb, d map[string]string) {
			h.del(d[H1] + "/" + HypothesisFile)
			h.del(d[H2] + "/" + HypothesisFile)
			h.del(d[S1] + "/" + StoryFile)
		}},
		"inactivated.md を消す": {
			func(b *fb, d map[string]string) { b.inactivate(d[S1], "理由") },
			func(h *fb, d map[string]string) { h.del(d[S1] + "/" + InactivatedFile) },
		},
		"検証結果の書き換え": {
			func(b *fb, d map[string]string) { b.verdict(d[H1], V1, true, "") },
			func(h *fb, d map[string]string) { h.verdict(d[H1], V1, false, "") },
		},
		"木の外のファイル": {none, func(h *fb, _ map[string]string) { h.put("ci/config.yml", []byte("on: push\n")) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b, d := base()
			c.prepare(b, d)
			h := b.clone()
			c.change(h, d)
			wantRejected(t, mustCheck(t, b, h, Context{}), RuleChangeNotAllowed)
		})
	}
}

func TestNotRegularFileRejected(t *testing.T) {
	b, d := base()
	h := b.clone()
	p := d[H1] + "/" + BackgroundFile
	h.put(p, []byte("背景\n"))
	f := h.files[p]
	f.Mode = "100755"
	h.files[p] = f
	wantRejected(t, mustCheck(t, b, h, Context{}), RuleNotRegularFile)
}

// base ですでに破れている条件は、拒まずに警告にする（design/backend/checks.md）。
func TestAlreadyBrokenIsWarning(t *testing.T) {
	b, d := base()
	b.verdict(d[H1], V1, true, "")
	b.verdict(d[H1], V2, false, "") // 有効な検証結果が 2 つ
	h := b.clone()
	h.put(d[H2]+"/"+BackgroundFile, []byte("直すための背景\n"))
	res := mustCheck(t, b, h, Context{})
	wantOK(t, res)
	if !hasRule(res.Warnings, RuleMultipleValidVerdicts) {
		t.Fatalf("warnings: %v", res.Warnings)
	}

	// 同じ規則でも、別のノードで新しく破れれば拒む。
	h2 := b.clone()
	h2.verdict(d[H2], "v-aaaaaaaaaa", true, "")
	h2.verdict(d[H2], "v-bbbbbbbbbb", false, "")
	wantRejected(t, mustCheck(t, b, h2, Context{}), RuleMultipleValidVerdicts)
}

func TestOperationsAreSorted(t *testing.T) {
	b, d := base()
	h := b.clone()
	h.put(d[H2]+"/"+BackgroundFile, []byte("背景\n"))
	h.verdict(d[H2], V1, true, "")
	h.hyp(h.story(d[H1], S2, "t"), H3, "x")
	res := mustCheck(t, b, h, Context{})
	wantOK(t, res)
	want := []string{OpStoryCreate, OpVerdictSubmit, OpNodeEdit}
	if got := opKinds(res.Operations); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

// すでに破れているノードは、悪くならなければ警告、悪くなれば拒む。
func TestAlreadyBrokenGetsWorse(t *testing.T) {
	b, d := base()
	b.verdict(d[H1], V1, true, "")
	b.verdict(d[H1], V2, false, "") // 有効な検証結果が 2 つ

	// 1 つを置き換えて 2 つのままなら、悪くなっていないので警告にする。
	h := b.clone()
	h.verdict(d[H1], "v-aaaaaaaaaa", true, V2)
	res := mustCheck(t, b, h, Context{})
	wantOK(t, res)
	if !hasRule(res.Warnings, RuleMultipleValidVerdicts) {
		t.Fatalf("warnings: %v", res.Warnings)
	}

	// 3 つにすれば、拒む。
	h2 := b.clone()
	h2.verdict(d[H1], "v-aaaaaaaaaa", true, "")
	res2 := mustCheck(t, b, h2, Context{})
	wantRejected(t, res2, RuleMultipleValidVerdicts)
	if v := res2.Rejected[0]; !reflect.DeepEqual(v.IDs, []string{V1, V2, "v-aaaaaaaaaa"}) {
		t.Fatalf("ids: %v", v.IDs)
	}
}

// 有効な検証結果が 2 つになった仮説は、まとめて差し替えて直せる。
func TestSupersedeSeveral(t *testing.T) {
	b, d := base()
	b.verdict(d[H1], V1, true, "")
	b.verdict(d[H1], V2, false, "")
	h := b.clone()
	h.verdictMulti(d[H1], "v-aaaaaaaaaa", true, []string{V1, V2})
	res := mustCheck(t, b, h, Context{})
	wantOK(t, res)
	if len(res.Warnings) != 0 {
		t.Fatalf("直ったのに警告がある: %v", res.Warnings)
	}
	if got := Build(h.files).Hypotheses[H1].ValidVerdicts(); len(got) != 1 || got[0].ID != "v-aaaaaaaaaa" {
		t.Fatalf("valid: %v", got)
	}

	// 一覧の 1 つでも有効でなければ拒む。
	h2 := b.clone()
	h2.verdict(d[H2], V3, true, "")
	h2.verdictMulti(d[H1], "v-aaaaaaaaaa", true, []string{V1, V3})
	wantRejected(t, mustCheck(t, b, h2, Context{}), RuleSupersedesNotValid)
}

func TestChangedPathsUsesBlobSHA(t *testing.T) {
	b := newFB().put("tree/a.md", []byte("x"))
	h := b.clone()
	f := h.files["tree/a.md"]
	f.Content = nil // 中身を読まなくても、SHA が同じなら変わっていない
	h.files["tree/a.md"] = f
	if got := ChangedPaths(b.files, h.files); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	f.SHA = blobSHA([]byte("y"))
	h.files["tree/a.md"] = f
	if got := ChangedPaths(b.files, h.files); !reflect.DeepEqual(got, []string{"tree/a.md"}) {
		t.Fatalf("got %v", got)
	}
}

func TestProblemsOrderIsStable(t *testing.T) {
	b, _ := base()
	for _, p := range []string{"z.txt", "a.txt", "m.txt", "tree/x.txt", "tree/b.txt", "artifacts/q.txt", "artifacts/c.txt"} {
		b.put(p, []byte("x"))
	}
	first := Build(b.files).Problems
	for i := 0; i < 20; i++ {
		if got := Build(b.files).Problems; !reflect.DeepEqual(got, first) {
			t.Fatalf("順が変わった:\n%v\n%v", first, got)
		}
	}
}

func TestInactivatedNeedsReason(t *testing.T) {
	b, d := base()
	h := b.clone()
	h.inactivate(d[S1], " \n")
	wantRejected(t, mustCheck(t, b, h, Context{}), RuleParse)
}
