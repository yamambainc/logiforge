// Package snapstore は、スナップショットの置き場のポートのアダプタである（design/adapters/snapshot-store.md）。
// 置き場を読むだけで、書かない。
package snapstore

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
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

func (a *Adapter) MainAt(ctx context.Context, repo string, number int) (string, error) {
	sha, err := store.GetMainIndex(ctx, a.Store, repo, number)
	return sha, notFound(err)
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

func (a *Adapter) Broken(ctx context.Context, repo string) (*store.Broken, error) {
	return store.GetBroken(ctx, a.Store, repo)
}

func (a *Adapter) Scanned(ctx context.Context, repo string) (string, error) {
	return store.GetScanned(ctx, a.Store, repo)
}

func (a *Adapter) MigratedTo(ctx context.Context, repo string) (string, error) {
	return store.GetMigratedTo(ctx, a.Store, repo)
}

// RecordLog は、worker の記録の控えを、pullreq.Adapter に読ませる（design/backend/verify/records.md）。
type RecordLog struct{ Store store.Reader }

func (l RecordLog) Comments(ctx context.Context, repoID string, number int) ([]pullreq.Comment, error) {
	logged, err := store.GetRecords(ctx, l.Store, repoID, number)
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Comment, 0, len(logged))
	for _, c := range logged {
		out = append(out, pullreq.Comment{ID: c.ID, IssueNumber: c.Issue, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	return out, nil
}

// CommentSource は、GitForge の repository のコメントを読む（worker の読むだけのトークンで）。
type CommentSource interface {
	RepoID(ctx context.Context, gitForgeRef string) (string, error)
	// CommentsSince は、repository のコメントのうち、since のあとに作られたか変わったものを返す。since がゼロならすべて。
	CommentsSince(ctx context.Context, gitForgeRef string, since time.Time) ([]pullreq.Comment, error)
}

// LogSyncer は、記録の控えに、GitForge の新しい記録のうち署名の合うものを足す（design/backend/verify/records.md）。
// worker が、見回りのたびに呼ぶ。
type LogSyncer struct {
	Source CommentSource
	Keys   pullreq.Keyring
	Store  store.Writer
	Log    *slog.Logger
}

// sinceSlack は、前に読んだ位置から戻って読む時間である。GitForge の時刻は秒の単位なので、同じ秒に変わったコメントを読み落とさないため。
const sinceSlack = time.Second

func (s LogSyncer) Sync(ctx context.Context, gitForgeRef string) error {
	// 控えは、名前でなく repository の識別子で分ける（store.RecordsKey）。
	repoID, err := s.Source.RepoID(ctx, gitForgeRef)
	if err != nil {
		return err
	}
	cursor, err := store.GetRecordsCursor(ctx, s.Store, repoID)
	if err != nil {
		return err
	}
	since := cursor
	if !since.IsZero() {
		since = since.Add(-sinceSlack)
	}
	cs, err := s.Source.CommentsSince(ctx, gitForgeRef, since)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return nil
	}
	recs, _ := pullreq.Records(s.Keys, repoID, cs)
	byIssue := map[int][]store.LoggedComment{}
	var issues []int
	for _, r := range recs {
		c := r.Comment
		if _, ok := byIssue[c.IssueNumber]; !ok {
			issues = append(issues, c.IssueNumber)
		}
		byIssue[c.IssueNumber] = append(byIssue[c.IssueNumber], store.LoggedComment{ID: c.ID, Issue: c.IssueNumber, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	for _, n := range issues {
		changed, err := store.AppendRecords(ctx, s.Store, repoID, n, byIssue[n])
		if err != nil {
			return err
		}
		for _, c := range changed {
			// 署名の合う、中身の違う記録は、同じ id では起きないはずである（署名の合う記録を、ほかの署名の合う記録に書き換えた）。最初のものを残す。
			s.logger().Warn("記録の控えと、中身の違う記録がある。控えのものを使う", "gitforge_ref", gitForgeRef, "number", n, "comment", c.ID)
		}
	}
	next := cursor
	for _, c := range cs {
		t := c.UpdatedAt
		if t.IsZero() {
			t = c.CreatedAt
		}
		if t.After(next) {
			next = t
		}
	}
	if next.Equal(cursor) {
		return nil
	}
	return store.PutRecordsCursor(ctx, s.Store, repoID, next)
}

func (s LogSyncer) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}
