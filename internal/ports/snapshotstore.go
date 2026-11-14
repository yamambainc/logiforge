package ports

import (
	"context"
	"errors"

	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
)

// ErrUnavailable は、外のもの（worker、git）に頼めないこと（止まっている、応答がない）を表す。
var ErrUnavailable = errors.New("worker に頼めない")

// ErrSnapshotPending は、worker がスナップショットを作り終えていないことを表す（取り込みは続いている）。
var ErrSnapshotPending = errors.New("スナップショットが、まだない")

// SnapshotStore は、スナップショットの置き場のポートである（design/backend/ports/snapshot-store.md）。
// 置き場を読むことだけを持つ。書く操作も、worker に頼む操作も持たない（worker は、バックエンドの workerclient で直接呼ぶ）。
// ないものは、ErrNotFound を返す。
type SnapshotStore interface {
	Repositories(ctx context.Context) ([]string, error)
	// Latest は、今の main の SHA を返す。repository がなければ ErrNotFound。
	Latest(ctx context.Context, repo string) (string, error)
	Commit(ctx context.Context, repo, sha string) (snapshot.Commit, error)
	Main(ctx context.Context, repo, sha string) (*snapshot.Main, error)
	Branch(ctx context.Context, repo, sha string) (*snapshot.Branch, error)
	BranchHistory(ctx context.Context, repo, branch string) (store.BranchHistory, error)
	Artifact(ctx context.Context, repo, sha256 string) ([]byte, error)
}
