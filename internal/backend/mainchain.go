package backend

import (
	"context"
	"errors"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
)

// mainNumber は、sha が、latest（v）までの main の並びにあれば、その番号を返す。なければ 0
// （design/backend/snapshot/main.md の「コミットの情報から引くもの」）。
// コミットの情報の番号が latest の番号以下で、かつ main の並びの索引のその番号が sha であるときだけ、並びにあるとする。
func (b *Backend) mainNumber(ctx context.Context, v *view, sha string) (int, error) {
	if sha == v.revision() {
		return v.main.Commit.Number, nil
	}
	c, err := b.Snap.Commit(ctx, v.repo, sha)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if c.Kind != snapshot.KindMain || c.Number < 1 || c.Number > v.main.Commit.Number {
		return 0, nil
	}
	got, err := b.Snap.MainAt(ctx, v.repo, c.Number)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if got != sha {
		return 0, nil
	}
	return c.Number, nil
}

// walkNodeChanges は、ノード id を変えた main のコミットを、from から新しい順に、コミットの情報の previous をたどって fn に渡す
// （design/backend/snapshot/main.md の「ノードを変えたコミット」）。fn が false を返すか、最初のコミットに着いたら終わる。
func (b *Backend) walkNodeChanges(ctx context.Context, repo, id string, from snapshot.Rev, fn func(r snapshot.Rev, c snapshot.Commit, ch snapshot.NodeChange) bool) error {
	for r := from; r.Number > 0; {
		c, err := b.Snap.Commit(ctx, repo, r.Revision)
		if err != nil {
			return err
		}
		var ch snapshot.NodeChange
		for _, x := range c.Changes {
			if x.Node == id {
				ch = x
				break
			}
		}
		if !fn(r, c, ch) || ch.Previous == nil {
			return nil
		}
		r = *ch.Previous
	}
	return nil
}
