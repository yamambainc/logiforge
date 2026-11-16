package backend

import (
	"context"
	"errors"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// At は、木を読む時点である。どちらも空なら、今の main（latest）。
type At struct {
	// Commit は、読む main のコミットの SHA。
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
		return b.mainAt(ctx, repo, at.Commit)
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
	if !targetsMain(ar) {
		return nil, fail(CodeOverlayUnsupported, "apply request %s の向き先が main でない（%s）。Logiforge は検査しない", id, ar.Target)
	}
	latest, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return nil, err
	}
	br, err := b.Snap.Branch(ctx, repo, ar.HeadRevision)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fail(CodeSnapshotPending, "先頭のコミット %s のスナップショットが、まだない。少し待って送り直す", ar.HeadRevision)
	}
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
	t := tree.Build(head)
	x := snapshot.IndexFromSnapshotTree(latest.main.Commit, &latest.main.SnapshotTree)
	st := snapshot.BuildSnapshotTreeOf(t, head, x)
	m := &snapshot.Main{FormatVersion: snapshot.FormatVersion, Commit: latest.main.Commit, SnapshotTree: *st}
	return &view{repo: repo, main: m, tree: t, files: head}, nil
}
