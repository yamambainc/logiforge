package snapstore

import (
	"context"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/store"
)

type comments struct {
	all   []pullreq.Comment
	since []time.Time
}

func (c *comments) RepoID(context.Context, string) (string, error) { return "id", nil }
func (c *comments) CommentsSince(_ context.Context, _ string, since time.Time) ([]pullreq.Comment, error) {
	c.since = append(c.since, since)
	var out []pullreq.Comment
	for _, x := range c.all {
		if !x.UpdatedAt.Before(since) {
			out = append(out, x)
		}
	}
	return out, nil
}

// 記録の控えには、署名の合う記録だけを足す。書き換えられても消されても、控えには最初のものが残る（design/backend/verify.md）。
func TestLogSyncer(t *testing.T) {
	ctx := context.Background()
	keys, _ := pullreq.KeyringFromSecrets([]string{"k1"}, []string{"s"})
	rec := func(n int, kind string) string {
		text, err := keys.Format(pullreq.Record{Trailers: []pullreq.Trailer{{Key: pullreq.KeyRecord, Value: kind}, {Key: pullreq.KeyApplyRequest, Value: "apr_1"}}}, pullreq.Scope{RepoID: "id", Number: n})
		if err != nil {
			t.Fatal(err)
		}
		return text
	}
	t0 := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	src := &comments{all: []pullreq.Comment{
		{ID: 1, IssueNumber: 3, Body: rec(3, pullreq.RecordApplyRequest), CreatedAt: t0, UpdatedAt: t0},
		{ID: 2, IssueNumber: 3, Body: "ただのコメント", CreatedAt: t0, UpdatedAt: t0},
		{ID: 3, IssueNumber: 3, Body: rec(4, pullreq.RecordClose), CreatedAt: t0, UpdatedAt: t0}, // ほかの場所の署名
	}}
	st := &store.Dir{Root: t.TempDir()}
	s := LogSyncer{Source: src, Keys: keys.VerifyOnly(), Store: st}
	if err := s.Sync(ctx, "o/q"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRecords(ctx, st, "o/q", 3)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("控え：%+v", got)
	}
	// 書き換え（署名が合わなくなる）と、新しい記録。控えは最初のものを持ち、新しいものを足す。
	t1 := t0.Add(time.Minute)
	src.all[0].Body, src.all[0].UpdatedAt = "書き換えた", t1
	src.all = append(src.all, pullreq.Comment{ID: 4, IssueNumber: 3, Body: rec(3, pullreq.RecordClose), CreatedAt: t1, UpdatedAt: t1})
	if err := s.Sync(ctx, "o/q"); err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(-time.Second); !src.since[1].Equal(want) {
		t.Fatalf("2 回目は、前に読んだ位置の 1 秒前から読む：%v", src.since[1])
	}
	got, _ = store.GetRecords(ctx, st, "o/q", 3)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 4 {
		t.Fatalf("控え：%+v", got)
	}
	if got[0].Body == "書き換えた" {
		t.Fatal("控えが書き換えられた")
	}
	// バックエンドは、控えを GitForge のコメントと合わせて読む。
	logged, _ := RecordLog{Store: st}.Comments(ctx, "o/q", 3)
	if len(logged) != 2 || logged[0].ID != 1 {
		t.Fatalf("RecordLog：%+v", logged)
	}
}
