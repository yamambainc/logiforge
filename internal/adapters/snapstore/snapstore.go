// Package snapstore は、スナップショットの置き場のポートのアダプタである（design/adapters/snapshot-store.md）。
// 置き場を読むだけで、書かない。
package snapstore

import (
	"context"
	"errors"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
)

// Adapter は、ports.SnapshotStore を実装する。
type Adapter struct {
	// Store は、置き場を読む。バックエンドは、読む鍵だけを持つ。
	Store store.Reader
}

var _ ports.SnapshotStore = (*Adapter)(nil)

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return ports.ErrNotFound
	}
	return err
}

func (a *Adapter) Repositories(ctx context.Context) ([]string, error) {
	return store.GetRepositories(ctx, a.Store)
}

func (a *Adapter) Latest(ctx context.Context, repo string) (string, error) {
	sha, err := store.GetLatest(ctx, a.Store, repo)
	if err != nil {
		return "", err
	}
	if sha == "" {
		return "", ports.ErrNotFound
	}
	return sha, nil
}

func (a *Adapter) Commit(ctx context.Context, repo, sha string) (snapshot.Commit, error) {
	c, err := store.GetCommit(ctx, a.Store, repo, sha)
	return c, notFound(err)
}

func (a *Adapter) Main(ctx context.Context, repo, sha string) (*snapshot.Main, error) {
	m, err := store.GetMain(ctx, a.Store, repo, sha)
	return m, notFound(err)
}

func (a *Adapter) Branch(ctx context.Context, repo, sha string) (*snapshot.Branch, error) {
	b, err := store.GetBranch(ctx, a.Store, repo, sha)
	return b, notFound(err)
}

func (a *Adapter) BranchHistory(ctx context.Context, repo, branch string) (store.BranchHistory, error) {
	h, _, err := store.GetBranchHistory(ctx, a.Store, repo, branch)
	return h, err
}

func (a *Adapter) Artifact(ctx context.Context, repo, sha256 string) ([]byte, error) {
	b, err := a.Store.Get(ctx, store.ArtifactKey(repo, sha256))
	return b, notFound(err)
}
