package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

// 認める apply request の 1 つの artifact にも、差分の検査の artifact の規則を当てる。
func TestRepairAcceptArtifactRules(t *testing.T) {
	ctx := context.Background()
	b := &Backend{}
	current := &view{main: &snapshot.Main{Commit: snapshot.Commit{Revision: "c1"}}, files: tree.Files{}}
	broken := &store.Broken{LastGood: "c0", First: "c1"}
	sum := strings.Repeat("a", 64)
	entry := func(path, sha string, size int64) *snapshot.Branch {
		return &snapshot.Branch{Diff: snapshot.Diff{Base: "c1", Entries: []snapshot.DiffEntry{{Path: path, File: &snapshot.File{Path: path, SHA256: sha, Size: size}}}}}
	}
	for name, c := range map[string]struct {
		br   *snapshot.Branch
		want string
	}{
		"正しい":      {entry(tree.ArtifactPath(sum), sum, 100), ""},
		"名前が違う":    {entry(tree.ArtifactPath(strings.Repeat("b", 64)), sum, 100), "sha256 でない"},
		"大きすぎる":    {entry(tree.ArtifactPath(sum), sum, tree.MaxArtifactSize+1), "上限"},
		"2 つ":      {&snapshot.Branch{Diff: snapshot.Diff{Base: "c1", Entries: append(entry(tree.ArtifactPath(sum), sum, 1).Diff.Entries, entry("tree/x", "", 1).Diff.Entries...)}}, "1 つ"},
		"base が違う": {&snapshot.Branch{Diff: snapshot.Diff{Base: "c0", Entries: entry(tree.ArtifactPath(sum), sum, 1).Diff.Entries}}, "分かれていない"},
	} {
		got, err := b.repairProblem(ctx, "q", KindRepairAccept, c.br, current, broken)
		if err != nil {
			t.Fatal(err)
		}
		if (c.want == "" && got != "") || (c.want != "" && !strings.Contains(got, c.want)) {
			t.Errorf("%s：%q（want %q）", name, got, c.want)
		}
	}
}
