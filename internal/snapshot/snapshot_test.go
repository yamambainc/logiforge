package snapshot

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

const (
	H0 = "h-0000000000"
	S1 = "s-1111111111"
	H1 = "h-3333333333"
	H2 = "h-4444444444"
	S2 = "s-2222222222"
	H3 = "h-5555555555"
	V1 = "v-7777777777"
)

type fs tree.Files

func blobSHA(b []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func (f fs) put(p string, b []byte) {
	f[p] = tree.File{Path: p, Mode: tree.RegularMode, SHA: blobSHA(b), Size: int64(len(b)), Content: b}
}

func (f fs) clone() fs {
	c := fs{}
	for k, v := range f {
		c[k] = v
	}
	return c
}

const (
	dH0 = "tree/" + H0
	dS1 = dH0 + "/" + S1
	dH1 = dS1 + "/" + H1
	dH2 = dS1 + "/" + H2
)

// baseFiles は、根 H0 の下に S1（H1、H2）がある木である。
func baseFiles() fs {
	f := fs{}
	f.put(dH0+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H0, Statement: "根"}))
	f.put(dS1+"/story.md", tree.MarshalStory(tree.StoryContent{ID: S1, Title: "t", Summary: "s"}))
	f.put(dH1+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H1, Statement: "子1"}))
	f.put(dH2+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H2, Statement: "子2"}))
	return f
}

func info(rev string, ar string) CommitInfo {
	ci := CommitInfo{Revision: rev, CommittedAt: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)}
	if ar != "" {
		ci.Trailers = []Trailer{{Key: TrailerApplyRequest, Value: ar}}
	}
	return ci
}

func rev(n int) string { return fmt.Sprintf("%040d", n) }

func mustMain(t *testing.T, f fs, ci CommitInfo, parent *Main) *Main {
	t.Helper()
	m, err := BuildMain(tree.Files(f), ci, parent)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func numbers(rs []Rev) []int {
	var out []int
	for _, r := range rs {
		out = append(out, r.Number)
	}
	return out
}

func TestBuildMainChangesAndHistory(t *testing.T) {
	f1 := baseFiles()
	m1 := mustMain(t, f1, info(rev(1), ""), nil)
	if m1.Commit.Number != 1 || m1.Commit.Parent != nil || len(m1.Commit.Warnings) != 0 {
		t.Fatalf("commit 1: %+v", m1.Commit)
	}
	if got := numbers(m1.Tree.Nodes[H2].Changes); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("H2: %v", got)
	}

	// H1 に検証結果を出す。
	f2 := f1.clone()
	f2.put(dH1+"/verdicts/"+V1+".md", tree.MarshalVerdict(tree.VerdictContent{ID: V1, Value: true, Report: "r\n"}))
	m2 := mustMain(t, f2, info(rev(2), "apr_2"), m1)
	if want := []NodeChange{{H1, FileVerdict}}; !reflect.DeepEqual(m2.Commit.Changes, want) {
		t.Fatalf("changes: %v", m2.Commit.Changes)
	}
	if got := numbers(m2.Tree.Nodes[H1].Changes); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("H1: %v", got)
	}
	if got := numbers(m2.Tree.Nodes[H2].Changes); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("H2: %v", got)
	}
	if v := m2.Tree.Nodes[H1].Value; v == nil || !*v || !m2.Tree.Nodes[H1].Leaf {
		t.Fatalf("H1 の値: %+v", m2.Tree.Nodes[H1])
	}
	if want := []HistoryEntry{{Number: 1, Revision: rev(1)}, {Number: 2, Revision: rev(2), ApplyRequest: "apr_2"}}; !reflect.DeepEqual(m2.Tree.History, want) {
		t.Fatalf("history: %v", m2.Tree.History)
	}

	// S1 を inactive にすると、S1、親の H0、子孫の H1、H2 が変わる。
	f3 := f2.clone()
	f3.put(dS1+"/inactivated.md", []byte("理由\n"))
	m3 := mustMain(t, f3, info(rev(3), "apr_3"), m2)
	for _, id := range []string{S1, H0, H1, H2} {
		if rs := m3.Tree.Nodes[id].Changes; rs[len(rs)-1].Number != 3 {
			t.Fatalf("%s: %v", id, rs)
		}
	}
	if !m3.Tree.Nodes[H1].Inactive {
		t.Fatal("H1 が inactive でない")
	}

	// 反映の記録のないコミットは警告にする。
	f4 := f3.clone()
	f4.put("README.md", []byte("x"))
	m4 := mustMain(t, f4, info(rev(4), ""), m3)
	if h := m4.Tree.History[3]; !hasRule(m4.Commit.Warnings, RuleNoApplyRequest) || !hasRule(m4.Commit.Warnings, tree.RuleOutsideTree) || !h.NoApplyRequest || !h.NewlyBroken {
		t.Fatalf("warnings: %v %+v", m4.Commit.Warnings, h)
	}

	// 前から破れている条件は、次のコミットの警告にしない。Tree.Broken に残る。
	f5 := f4.clone()
	f5.put(dH2+"/background.md", []byte("背景\n"))
	m5 := mustMain(t, f5, info(rev(5), "apr_5"), m4)
	if len(m5.Commit.Warnings) != 0 || !hasRule(m5.Tree.Broken, tree.RuleOutsideTree) || m5.Tree.History[4].NewlyBroken {
		t.Fatalf("warnings: %v broken: %v", m5.Commit.Warnings, m5.Tree.Broken)
	}
}

func TestInitTrailerIsNotWarned(t *testing.T) {
	m1 := mustMain(t, baseFiles(), info(rev(1), ""), nil)
	f2 := baseFiles()
	f2.put(dH2+"/background.md", []byte("x\n"))
	ci := info(rev(2), "")
	ci.Trailers = []Trailer{{Key: TrailerInit, Value: "1"}}
	if m2 := mustMain(t, f2, ci, m1); len(m2.Commit.Warnings) != 0 {
		t.Fatalf("warnings: %v", m2.Commit.Warnings)
	}
}

func TestSubtreeChangedAfter(t *testing.T) {
	f1 := baseFiles()
	m1 := mustMain(t, f1, info(rev(1), ""), nil)
	f2 := f1.clone()
	f2.put(dH2+"/background.md", []byte("x\n"))
	m2 := mustMain(t, f2, info(rev(2), "apr_2"), m1)
	cases := []struct {
		node   string
		number int
		want   bool
	}{
		{S1, 1, true},  // 子孫の H2 が 2 で変わった
		{H1, 1, false}, // H1 の部分木は変わっていない
		{H2, 2, false},
		{H0, 1, true},
		{"h-zzzzzzzzzz", 1, true}, // ない
	}
	for _, c := range cases {
		if got := m2.Tree.SubtreeChangedAfter(c.node, c.number); got != c.want {
			t.Fatalf("%s after %d: %v", c.node, c.number, got)
		}
	}
}

func TestEncodeIsStableAndVersioned(t *testing.T) {
	m := mustMain(t, baseFiles(), info(rev(1), ""), nil)
	a, _ := Encode(m)
	b, _ := Encode(m)
	if string(a) != string(b) {
		t.Fatal("2 回の Encode が違う")
	}
	m.FormatVersion = FormatVersion - 1
	old, _ := Encode(m)
	var got Main
	if err := Decode(old, &got); err == nil {
		t.Fatal("古い形の版を読んでしまった")
	}
}

func hasRule(vs []tree.Violation, r tree.Rule) bool {
	for _, v := range vs {
		if v.Rule == r {
			return true
		}
	}
	return false
}

func TestRoundTrip(t *testing.T) {
	f := baseFiles()
	art := []byte("data")
	sha := tree.SHA256Hex(art)
	f.put(tree.ArtifactPath(sha), art)
	f.put(dH1+"/verdicts/"+V1+".md", tree.MarshalVerdict(tree.VerdictContent{ID: V1, Value: true, Report: "r\n",
		Artifacts: []tree.ArtifactRef{{Kind: "file", Name: "d", SHA256: sha}}}))
	f.put("bin.dat", []byte{0xff, 0xfe, 0x00})
	m := mustMain(t, f, info(rev(1), ""), nil)

	data, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	var got Main
	if err := Decode(data, &got); err != nil {
		t.Fatal(err)
	}
	files, err := got.Tree.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(f) {
		t.Fatalf("ファイルの数: %d と %d", len(files), len(f))
	}
	for p, want := range f {
		g := files[p]
		if g.SHA != want.SHA || g.Mode != want.Mode || g.Size != want.Size {
			t.Fatalf("%s: %+v", p, g)
		}
		if p == tree.ArtifactPath(sha) {
			if g.Content != nil || g.SHA256 != sha {
				t.Fatalf("artifact: %+v", g)
			}
		} else if string(g.Content) != string(want.Content) {
			t.Fatalf("%s の中身: %q", p, g.Content)
		}
	}
	// artifact の中身がなくても、木は同じに組み立てられる（bin.dat の、木の外の警告だけ）。
	tr := tree.Build(files)
	if len(tr.Problems) != 1 || tr.Problems[0].Rule != tree.RuleOutsideTree || len(tr.Artifacts) != 1 {
		t.Fatalf("problems: %v", tr.Problems)
	}
}

// バックエンドの道：main のスナップショット → Files、ブランチの差分を重ねる → 差分の検査。
func TestBranchOverlayAndCheck(t *testing.T) {
	base := baseFiles()
	base.put(dH2+"/background.md", []byte("一\n"))
	mainSnap := mustMain(t, base, info(rev(1), ""), nil)

	head := base.clone()
	head.put(dH1+"/"+S2+"/story.md", tree.MarshalStory(tree.StoryContent{ID: S2, Title: "新", Summary: "s"}))
	head.put(dH1+"/"+S2+"/"+H3+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H3, Statement: "孫"}))
	head.put(dH2+"/background.md", []byte("一\n二\n"))
	br := mustBranch(t, base, head, info(rev(10), ""))
	if len(br.Diff.Entries) != 3 || len(br.Commit.Paths) != 3 {
		t.Fatalf("diff: %+v", br.Diff.Entries)
	}

	// 同じ base に重ねると、ブランチの先頭と同じ木になる。
	mainFiles, err := mainSnap.Tree.Files()
	if err != nil {
		t.Fatal(err)
	}
	overlaid, conflicts, err := Overlay(mainFiles, br.Diff)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("overlay: %v %v", conflicts, err)
	}
	if got := tree.ChangedPaths(tree.Files(head), overlaid); len(got) != 0 {
		t.Fatalf("先頭と違う: %v", got)
	}
	res, err := tree.CheckChange(mainFiles, overlaid, tree.Context{})
	if err != nil || !res.OK() || len(res.Operations) != 2 {
		t.Fatalf("check: %+v %v", res, err)
	}

	// main が、関係のないところで進んでも重ねられる。
	newer := base.clone()
	newer.put(dH1+"/background.md", []byte("別\n"))
	nf, conflicts, _ := Overlay(tree.Files(newer), br.Diff)
	if len(conflicts) != 0 || string(nf[dH1+"/background.md"].Content) != "別\n" {
		t.Fatalf("conflicts: %v", conflicts)
	}

	// 同じ background.md が main でも変わっていれば、重ねられない。
	clash := base.clone()
	clash.put(dH2+"/background.md", []byte("一\n三\n"))
	if _, conflicts, _ := Overlay(tree.Files(clash), br.Diff); !reflect.DeepEqual(conflicts, []string{dH2 + "/background.md"}) {
		t.Fatalf("conflicts: %v", conflicts)
	}
}

func TestBranchDiffEncodes(t *testing.T) {
	base := baseFiles()
	head := base.clone()
	art := []byte("data")
	head.put(tree.ArtifactPath(tree.SHA256Hex(art)), art)
	delete(head, dH2+"/hypothesis.md")
	br := mustBranch(t, base, head, info(rev(2), ""))
	data, err := Encode(br)
	if err != nil {
		t.Fatal(err)
	}
	var got Branch
	if err := Decode(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&got, br) {
		t.Fatalf("got %+v", got)
	}
	for _, e := range got.Diff.Entries {
		switch e.Status {
		case Added:
			if e.File.Content != nil || e.File.SHA256 == "" {
				t.Fatalf("artifact の中身を入れた: %+v", e.File)
			}
		case Deleted:
			if e.File != nil || e.BaseSHA == "" {
				t.Fatalf("deleted: %+v", e)
			}
		}
	}
}

// mustBranch は、gitx の ChangedFiles と同じく、差分と、先頭で変わったファイルだけからブランチのスナップショットを作る。
func mustBranch(t *testing.T, base, head fs, ci CommitInfo) *Branch {
	t.Helper()
	var deltas []Delta
	changed := tree.Files{}
	for _, p := range tree.ChangedPaths(tree.Files(base), tree.Files(head)) {
		d := Delta{Path: p}
		bf, inBase := base[p]
		hf, inHead := head[p]
		switch {
		case !inBase:
			d.Status = Added
		case !inHead:
			d.Status = Deleted
		default:
			d.Status = Modified
		}
		if inBase {
			d.BaseSHA = bf.SHA
		}
		if inHead {
			changed[p] = hf
		}
		deltas = append(deltas, d)
	}
	b, err := BuildBranch(rev(1), deltas, changed, ci)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Overlay の振る舞いを、パスの変わり方の組み合わせで網羅する。
func TestOverlayTable(t *testing.T) {
	a, b, c := []byte("a"), []byte("b"), []byte("c")
	file := func(content []byte) *File {
		f := fromTreeFile(tree.File{Path: "p", Mode: tree.RegularMode, SHA: blobSHA(content), Size: int64(len(content)), Content: content}, false)
		return &f
	}
	cases := []struct {
		name     string
		main     []byte // nil はファイルがない
		entry    DiffEntry
		conflict bool
		want     []byte // nil はファイルがない
	}{
		{"加える・main になし", nil, DiffEntry{Status: Added, File: file(b)}, false, b},
		{"加える・main に同じもの", b, DiffEntry{Status: Added, File: file(b)}, false, b},
		{"加える・main に別のもの", c, DiffEntry{Status: Added, File: file(b)}, true, nil},
		{"変える・main は base のまま", a, DiffEntry{Status: Modified, BaseSHA: blobSHA(a), File: file(b)}, false, b},
		{"変える・main も変えた", c, DiffEntry{Status: Modified, BaseSHA: blobSHA(a), File: file(b)}, true, nil},
		{"変える・main も同じに変えた", b, DiffEntry{Status: Modified, BaseSHA: blobSHA(a), File: file(b)}, false, b},
		{"変える・main で消えた", nil, DiffEntry{Status: Modified, BaseSHA: blobSHA(a), File: file(b)}, true, nil},
		{"消す・main は base のまま", a, DiffEntry{Status: Deleted, BaseSHA: blobSHA(a)}, false, nil},
		{"消す・main でも消えた", nil, DiffEntry{Status: Deleted, BaseSHA: blobSHA(a)}, false, nil},
		{"消す・main で変えた", c, DiffEntry{Status: Deleted, BaseSHA: blobSHA(a)}, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := fs{}
			if tc.main != nil {
				base.put("p", tc.main)
			}
			tc.entry.Path = "p"
			got, conflicts, err := Overlay(tree.Files(base), Diff{Entries: []DiffEntry{tc.entry}})
			if err != nil {
				t.Fatal(err)
			}
			if (len(conflicts) > 0) != tc.conflict {
				t.Fatalf("conflicts: %v", conflicts)
			}
			if tc.conflict {
				return
			}
			f, ok := got["p"]
			if (tc.want == nil) != !ok || (ok && string(f.Content) != string(tc.want)) {
				t.Fatalf("got %+v", f)
			}
		})
	}
}
