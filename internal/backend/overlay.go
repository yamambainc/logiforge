package backend

import (
	"context"
	"errors"
	"strings"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/tree"
)

// At は、木を読む時点である。どちらも空なら、今の main（latest）。
type At struct {
	// Commit は、読むコミットの SHA。main のコミットか、ブランチのスナップショットがあるブランチのコミット。
	Commit string
	// ApplyRequest は、反映したあとの木を読む、open の apply request の識別子。Commit と同時には指定できない。
	ApplyRequest string
}

// mainTarget は、Logiforge が検査し、反映する向き先である（design/backend/apply-request.md の「向き先」）。
const mainTarget = "main"

// targetsMain は、apply request の向き先が main かを返す。向き先がわからない（GitForge が返さない）ときは、main とみなす。
// バックエンドが作る pull request の向き先は、いつも main だからである。
func targetsMain(ar ports.ApplyRequest) bool { return ar.Target == "" || ar.Target == mainTarget }

// readAt は、木を読む API の view を返す。
func (b *Backend) readAt(ctx context.Context, repo, ref string, at At) (*view, error) {
	if at.ApplyRequest == "" {
		return b.commitAt(ctx, repo, at.Commit)
	}
	if at.Commit != "" {
		return nil, fail(CodeValidationFailed, "commit と apply_request は、同時に指定できない")
	}
	return b.afterView(ctx, repo, ref, at.ApplyRequest)
}

// afterView は、open の apply request の先頭のコミットの差分を、向き先の今の先頭（latest）のスナップショットツリーに重ねた、
// 反映したあとの view を返す（design/backend/snapshot.md の「向き先に重ねる」）。重ねた結果は保持しない。
func (b *Backend) afterView(ctx context.Context, repo, ref, id string) (*view, error) {
	ar, err := b.Forge.GetApplyRequest(ctx, ref, id)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fail(CodeNotFound, "apply request %s がない", id)
	}
	if err != nil {
		return nil, err
	}
	if ar.State != ports.StateOpen {
		return nil, fail(CodeNotOpen, "apply request %s は open でない", id)
	}
	br, err := b.Snap.Branch(ctx, repo, ar.HeadRevision)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fail(CodeSnapshotPending, "先頭のコミット %s のスナップショットが、まだない。少し待って送り直す", ar.HeadRevision)
	}
	if err != nil {
		return nil, err
	}
	if !targetsMain(ar) {
		return b.afterBranchView(ctx, repo, ar.Target, br)
	}
	latest, err := b.currentMain(ctx, repo)
	if err != nil {
		return nil, err
	}
	head, conflicts, err := snapshot.Overlay(latest.files, br.Diff)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, fail(CodeOverlayConflict, "今の main に重ねられない。衝突したパス：%s", strings.Join(conflicts, "、"))
	}
	return viewOf(latest, head, latest.main.Commit), nil
}

// afterBranchView は、向き先が main でない apply request の差分を、向き先のブランチの今の先頭に重ねた view を返す。
// 衝突は判定せず、検査もしない（design/backend/snapshot.md の「向き先に重ねる」）。応答のコミットは、向き先のブランチの今の先頭である。
func (b *Backend) afterBranchView(ctx context.Context, repo, target string, ar *snapshot.Branch) (*view, error) {
	h, err := b.Snap.BranchHistory(ctx, repo, target)
	if err != nil {
		return nil, err
	}
	if len(h.Entries) == 0 {
		return nil, fail(CodeSnapshotPending, "向き先のブランチ %s を、まだ取り込んでいない。少し待って送り直す", target)
	}
	last := h.Entries[len(h.Entries)-1]
	if last.Deleted || last.Head == "" {
		return nil, fail(CodeNotFound, "向き先のブランチ %s が消えた", target)
	}
	tv, err := b.branchView(ctx, repo, last.Head)
	if err != nil {
		return nil, err
	}
	return overlaid(tv, ar.Diff, tv.main.Commit)
}

// commitAt は、コミットの view を返す。main のコミットになければ、ブランチのコミットとして読む。空なら今の main。
func (b *Backend) commitAt(ctx context.Context, repo, revision string) (*view, error) {
	v, err := b.mainAt(ctx, repo, revision)
	if revision == "" || errCode(err) != CodeSnapshotPending {
		return v, err
	}
	_, berr := b.Snap.Branch(ctx, repo, revision)
	if errors.Is(berr, ports.ErrNotFound) {
		return nil, err // main のコミットとしても、ブランチのコミットとしても、まだない。待つ
	}
	if berr != nil {
		return nil, berr
	}
	return b.branchView(ctx, repo, revision)
}

// branchView は、ブランチのコミットのスナップショットツリーを、読むたびに作る（design/backend/snapshot.md の
// 「ブランチのコミットのスナップショットツリー」）。base の main の木に、ブランチの差分を重ねる。置き場には置かない。
func (b *Backend) branchView(ctx context.Context, repo, revision string) (*view, error) {
	br, err := b.Snap.Branch(ctx, repo, revision)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fail(CodeSnapshotPending, "ブランチのコミット %s のスナップショットが、まだない。少し待って送り直す", revision)
	}
	if err != nil {
		return nil, err
	}
	base, err := b.mainAt(ctx, repo, br.Diff.Base)
	if err != nil {
		return nil, err
	}
	return overlaid(base, br.Diff, br.Commit)
}

// overlaid は、base の view に差分を衝突なしで重ね、木を組み立てて値を計算した view を返す。コミットは commit にする。
func overlaid(base *view, d snapshot.Diff, commit snapshot.Commit) (*view, error) {
	head, err := snapshot.Apply(base.files, d)
	if err != nil {
		return nil, err
	}
	return viewOf(base, head, commit), nil
}

// viewOf は、base の view に重ねたファイル head から、木を組み立てて値を計算した view を返す。コミットは commit にする。
func viewOf(base *view, head tree.Files, commit snapshot.Commit) *view {
	t := tree.Build(head)
	x := snapshot.IndexFromSnapshotTree(base.main.Commit, &base.main.SnapshotTree)
	st := snapshot.BuildSnapshotTreeOf(t, head, x)
	m := &snapshot.Main{FormatVersion: snapshot.FormatVersion, Commit: commit, SnapshotTree: *st}
	return &view{repo: base.repo, main: m, tree: t, files: head}
}

// errCode は、エラーの code を返す。Logiforge のエラーでなければ空。
func errCode(err error) Code {
	if e := AsError(err); e != nil {
		return e.Code
	}
	return ""
}
