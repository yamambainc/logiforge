// Package store は、スナップショットの置き場である（design/backend/ports/snapshot-store.md）。
// キーの決まりと、スナップショットの読み書きと、手元のディレクトリの実装を持つ。
// 書くのは worker だけで、バックエンドは Reader だけを使う。
package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNotFound は、キーがないことを表す。
var ErrNotFound = errors.New("store: not found")

// ErrPrecondition は、条件付きの書き込みで、今の値が期待したものと違うことを表す。
var ErrPrecondition = errors.New("store: 今の値が期待したものと違う")

// Reader は、置き場を読む。
type Reader interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// Writer は、置き場に書く。worker だけが使う。
type Writer interface {
	Reader
	Put(ctx context.Context, key string, data []byte) error
	// PutIf は、今の値が old のときだけ書く。old が nil なら、キーがないときだけ書く。
	PutIf(ctx context.Context, key string, data, old []byte) error
}

// Dir は、手元のディレクトリの置き場である（design/adapters/snapshot-store.md の「手元のディレクトリ」）。
type Dir struct {
	Root string
	mu   sync.Mutex
}

var _ Writer = (*Dir)(nil)

func (d *Dir) path(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("store: キーの形が正しくない: %q", key)
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("store: キーの形が正しくない: %q", key)
		}
	}
	return filepath.Join(d.Root, filepath.FromSlash(key)), nil
}

func (d *Dir) Get(_ context.Context, key string) ([]byte, error) {
	p, err := d.path(key)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// Put は、一時ファイルに書いてから名前を付け替える。読む側が、書きかけのファイルを読まないようにするため。
func (d *Dir) Put(_ context.Context, key string, data []byte) error {
	p, err := d.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), p)
}

// PutIf は、同じプロセスの中でだけ、比べて書くことを不可分にする。worker は 1 つだけ動かす前提である（design/backend/worker.md）。
func (d *Dir) PutIf(ctx context.Context, key string, data, old []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	cur, err := d.Get(ctx, key)
	switch {
	case errors.Is(err, ErrNotFound):
		if old != nil {
			return ErrPrecondition
		}
	case err != nil:
		return err
	case old == nil || !bytes.Equal(cur, old):
		return ErrPrecondition
	}
	return d.Put(ctx, key, data)
}

// キーの決まり（design/backend/ports/snapshot-store.md の「置いてあるもの」）。

func CommitKey(repo, sha string) string      { return repo + "/commits/" + sha }
func SnapshotKey(repo, sha string) string    { return repo + "/snapshots/" + sha }
func ArtifactKey(repo, sha256 string) string { return repo + "/artifacts/" + sha256 }
func LatestKey(repo string) string           { return repo + "/latest" }

// BranchKey は、ブランチの履歴のキーである。ブランチの名前は、`/` を含みうるので、パスとしてエスケープする。
func BranchKey(repo, branch string) string { return repo + "/branches/" + url.PathEscape(branch) }

// RepositoriesKey は、latest がある repository の名前の一覧のキーである。
const RepositoriesKey = "repositories"
