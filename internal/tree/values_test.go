package tree

import "testing"

// leafValue は、子 1 の葉に値を付ける。null なら検証結果を付けない。
func setLeaf(b *fb, dir, vid string, v Value) {
	switch v {
	case True:
		b.verdict(dir, vid, true, "")
	case False:
		b.verdict(dir, vid, false, "")
	}
}

func TestStoryIsKleeneAND(t *testing.T) {
	vals := []Value{True, Null, False}
	want := [3][3]Value{
		{True, Null, False},
		{Null, Null, False},
		{False, False, False},
	}
	for i, a := range vals {
		for j, c := range vals {
			b, d := base()
			setLeaf(b, d[H1], V1, a)
			setLeaf(b, d[H2], V2, c)
			got := Compute(Build(b.files)).Story[S1]
			if got != want[i][j] {
				t.Errorf("A=%v B=%v: got %v want %v", a, c, got, want[i][j])
			}
		}
	}
}

// twoStories は、H0 の下に S1（子 H1）と S2（子 H3）を置く。
func twoStories(s1, s2 Value) *fb {
	b := newFB()
	h0 := b.hyp(TreeDir, H0, "根")
	st1 := b.story(h0, S1, "1")
	h1 := b.hyp(st1, H1, "a")
	st2 := b.story(h0, S2, "2")
	h3 := b.hyp(st2, H3, "b")
	setLeaf(b, h1, V1, s1)
	setLeaf(b, h3, V2, s2)
	return b
}

func TestHypothesisIsORWithoutFalse(t *testing.T) {
	vals := []Value{True, Null, False}
	want := [3][3]Value{
		{True, True, True},
		{True, Null, Null},
		{True, Null, Null},
	}
	for i, a := range vals {
		for j, c := range vals {
			v := Compute(Build(twoStories(a, c).files))
			if got := v.Hypothesis[H0]; got != want[i][j] {
				t.Errorf("S1=%v S2=%v: got %v want %v", a, c, got, want[i][j])
			}
			if wantNR := a == False && c == False; v.NeedsReconsideration[H0] != wantNR {
				t.Errorf("S1=%v S2=%v: 要再検討が %v", a, c, v.NeedsReconsideration[H0])
			}
		}
	}
}

func TestInactiveStoryIsExcludedFromOR(t *testing.T) {
	b := twoStories(True, False)
	b.inactivate(TreeDir+"/"+H0+"/"+S1, "欠陥")
	v := Compute(Build(b.files))
	if v.Hypothesis[H0] != Null || !v.NeedsReconsideration[H0] {
		t.Fatalf("H0 = %v、要再検討 = %v", v.Hypothesis[H0], v.NeedsReconsideration[H0])
	}
}

// TestPropagationExample は、docs/tree/propagation.md の「例」を確かめる。
func TestPropagationExample(t *testing.T) {
	b := newFB()
	h0 := b.hyp(TreeDir, H0, "H0")
	s1 := b.story(h0, S1, "S1")
	h1 := b.hyp(s1, H1, "H1")
	h2 := b.hyp(s1, H2, "H2")
	s2 := b.story(h0, S2, "S2")
	h3 := b.hyp(s2, H3, "H3")
	h4 := b.hyp(s2, H4, "H4")
	s3 := b.story(h4, S3, "S3")
	h5 := b.hyp(s3, H5, "H5")
	b.verdict(h1, "v-0000000001", true, "")
	b.verdict(h2, "v-0000000002", false, "")
	b.verdict(h3, "v-0000000003", true, "")
	b.verdict(h5, "v-0000000005", true, "")
	v := Compute(Build(b.files))
	for id, want := range map[string]Value{H0: True, H4: True} {
		if v.Hypothesis[id] != want {
			t.Errorf("%s = %v", id, v.Hypothesis[id])
		}
	}
	for id, want := range map[string]Value{S1: False, S2: True, S3: True} {
		if v.Story[id] != want {
			t.Errorf("%s = %v", id, v.Story[id])
		}
	}
	for _, id := range []string{H1, H2, H3, H5} {
		if !v.Leaf[id] {
			t.Errorf("%s が葉でない", id)
		}
	}
	// 根拠部分木：H0 → S2 → H3、H4 → S3 → H5。S1 側は入らない。
	for _, id := range []string{H0, S2, H3, H4, S3, H5} {
		if !v.Evidence[id] {
			t.Errorf("%s が根拠部分木にない", id)
		}
	}
	for _, id := range []string{S1, H1, H2} {
		if v.Evidence[id] {
			t.Errorf("%s が根拠部分木にある", id)
		}
	}
}
