package backend

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

// 直すための apply request の種類（design/backend/broken.md の「直すための apply request」）。空は、ふつうの apply request。
const (
	KindChange       = "change"
	KindRepairRevert = "repair.revert"
	KindRepairAccept = "repair.accept"
)

// normalizeKind は、API の kind を、記録に書く値にする。ふつうの apply request は空。知らない値なら誤り。
func normalizeKind(k string) (string, error) {
	switch k {
	case "", KindChange:
		return "", nil
	case KindRepairRevert, KindRepairAccept:
		return k, nil
	}
	return "", fail(CodeValidationFailed, "kind は change、repair.revert、repair.accept のどれか")
}


// repairProblem は、直すための apply request の先頭のコミットが、反映の条件を満たすかを確かめ、満たさなければ理由を返す。
// current は今の main（壊れていても戻さない）、br は先頭のコミットのブランチのスナップショットである。
//   - revert：差分を今の main に重ねた木の全体が、最後に正しかった時点の木と同じである。
//   - accept：差分が空で、ブランチの base が今の main の先頭である。今の main の木に、tree/ と artifacts/ の外のファイルがない。
func (b *Backend) repairProblem(ctx context.Context, repo, kind string, br *snapshot.Branch, current *view, broken *store.Broken) (string, error) {
	if broken == nil {
		return "repository は壊れていない。直すための apply request は要らない", nil
	}
	switch kind {
	case KindRepairRevert:
		head, conflicts, err := snapshot.Overlay(current.files, br.Diff)
		if err != nil {
			return "", err
		}
		if len(conflicts) > 0 {
			return "今の main に重ねられない：" + strings.Join(conflicts, "、"), nil
		}
		good, err := b.mainAt(ctx, repo, broken.LastGood)
		if err != nil {
			return "", err
		}
		if !tree.SameFiles(head, good.files) {
			return fmt.Sprintf("重ねた木が、最後に正しかった時点 %s の木と同じでない", broken.LastGood), nil
		}
	case KindRepairAccept:
		// 先頭のコミットは、認めた記録の artifact を 1 つ加えるだけにする。空のコミットの pull request は、Forgejo で squash merge できないから。
		if len(br.Diff.Entries) != 1 || br.Diff.Entries[0].BaseSHA != "" || br.Diff.Entries[0].File == nil {
			return "認める apply request の先頭のコミットは、認めた記録の artifact を 1 つ加えるだけにする（logiforge repair accept が作る）", nil
		}
		// その 1 つにも、差分の検査の artifact の規則（名前が中身の sha256、大きさの上限）を当てる。
		e := br.Diff.Entries[0]
		if e.File.SHA256 == "" || e.Path != tree.ArtifactPath(e.File.SHA256) {
			return fmt.Sprintf("認めた記録 %s の名前が、中身の sha256 でない", e.Path), nil
		}
		if e.File.Size > tree.MaxArtifactSize {
			return fmt.Sprintf("認めた記録 %s が、大きさの上限を超える", e.Path), nil
		}
		if br.Diff.Base != current.revision() {
			return fmt.Sprintf("ブランチが、今の main の先頭 %s から分かれていない。今の main から作り直す", current.revision()), nil
		}
		for p := range current.files {
			if !strings.HasPrefix(p, tree.TreeDir+"/") && !strings.HasPrefix(p, "artifacts/") {
				return fmt.Sprintf("今の main に、木の置き場所の外のファイル %s がある。認めることはできず、戻すことでだけ直せる", p), nil
			}
		}
	}
	return "", nil
}

// BrokenLastGood は、repository が壊れていれば、最後に正しかった時点のコミットを返す。壊れていなければ空。
// API は、repository の木を読む応答に、ヘッダー Logiforge-Repository-Broken として付ける。
func (b *Backend) BrokenLastGood(ctx context.Context, repo string) (string, error) {
	br, err := b.Snap.Broken(ctx, repo)
	if err != nil || br == nil {
		return "", err
	}
	return br.LastGood, nil
}

// GetRepairFiles は、壊れた repository の、最後に正しかった時点の木の全体のファイルを返す（CLI の repair revert が使う）。
// ファイルは、スナップショットツリーと artifact の置き場から読む。壊れていなければ not_found。
func (b *Backend) GetRepairFiles(ctx context.Context, c Caller, repo string) (RepairFiles, error) {
	if _, err := b.access(ctx, c, repo); err != nil {
		return RepairFiles{}, err
	}
	br, err := b.Snap.Broken(ctx, repo)
	if err != nil {
		return RepairFiles{}, err
	}
	if br == nil {
		return RepairFiles{}, fail(CodeNotFound, "repository %s は壊れていない", repo)
	}
	v, err := b.mainAt(ctx, repo, br.LastGood)
	if err != nil {
		return RepairFiles{}, err
	}
	out := RepairFiles{Commit: br.LastGood, Files: []RepairFile{}}
	for p, f := range v.files {
		content := f.Content
		if content == nil && f.SHA256 != "" {
			if content, err = b.Snap.Artifact(ctx, repo, f.SHA256); err != nil {
				return RepairFiles{}, fmt.Errorf("artifact %s: %w", p, err)
			}
		}
		out.Files = append(out.Files, RepairFile{Path: p, Mode: f.Mode, ContentBase64: base64.StdEncoding.EncodeToString(content)})
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	return out, nil
}

// repairMaterial は、最後に正しかった時点から今の main（current）までの、ファイルの変化を返す。壊れていなければ nil。
func (b *Backend) repairMaterial(ctx context.Context, repo, kind string, current *view) (*RepairMaterial, error) {
	broken, err := b.Snap.Broken(ctx, repo)
	if err != nil || broken == nil {
		return nil, err
	}
	good, err := b.mainAt(ctx, repo, broken.LastGood)
	if err != nil {
		return nil, err
	}
	m := &RepairMaterial{Kind: kind, LastGoodCommit: broken.LastGood, MainCommit: current.revision(), Changes: []FileChange{}}
	text := func(f tree.File) *string {
		if f.Content == nil {
			return nil
		}
		s := string(f.Content)
		return &s
	}
	paths := map[string]bool{}
	for p := range good.files {
		paths[p] = true
	}
	for p := range current.files {
		paths[p] = true
	}
	for _, p := range sortedKeys(paths) {
		before, inGood := good.files[p]
		after, inMain := current.files[p]
		switch {
		case inGood && !inMain:
			m.Changes = append(m.Changes, FileChange{Path: p, Status: "deleted", Before: text(before)})
		case !inGood && inMain:
			m.Changes = append(m.Changes, FileChange{Path: p, Status: "added", After: text(after)})
		case !tree.SameFiles(tree.Files{p: before}, tree.Files{p: after}):
			m.Changes = append(m.Changes, FileChange{Path: p, Status: "modified", Before: text(before), After: text(after)})
		}
	}
	return m, nil
}
