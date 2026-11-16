package ports

import (
	"context"
	"errors"

	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
)

// ErrUnavailable は、GitForge の git を読めないこと（止まっている、応答がない）を表す。
var ErrUnavailable = errors.New("GitForge の git を読めない")

// SnapshotStore は、スナップショットの置き場のポートである（design/backend/ports/snapshot-store.md）。
// 置き場を読むことだけを持つ。書く操作は持たない。バックエンドは worker を呼ばない（design/backend/worker.md の「形」）。
// ないものは、ErrNotFound を返す。
type SnapshotStore interface {
	Repositories(ctx context.Context) ([]string, error)
	// Latest は、今の main の SHA を返す。repository がなければ ErrNotFound。
	Latest(ctx context.Context, repo string) (string, error)
	Commit(ctx context.Context, repo, sha string) (snapshot.Commit, error)
	Main(ctx context.Context, repo, sha string) (*snapshot.Main, error)
	// MainAt は、main の並びの索引から、番号 number の main のコミットの SHA を返す（design/backend/snapshot.md の「コミットの記録から引くもの」）。
	MainAt(ctx context.Context, repo string, number int) (string, error)
	Branch(ctx context.Context, repo, sha string) (*snapshot.Branch, error)
	BranchHistory(ctx context.Context, repo, branch string) (store.BranchHistory, error)
	Artifact(ctx context.Context, repo, sha256 string) ([]byte, error)
}
