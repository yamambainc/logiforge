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

	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
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
//
// git から作り直せるもの（コミットの記録、木、latest、repositories）は、形の版の接頭辞（例：`v4/`）の下に置く。
// 形の版が変わったら、新しい版の worker は、空の接頭辞から作り直す。古い版は消さない。
// 作り直せないもの（ブランチの履歴とその一覧、ブランチのスナップショット）と、形を持たないもの（artifact の中身）は、
// 版の外の `keep/` の下に置く。
// 置き場の直下には、版の接頭辞と `keep/` だけを置く。repository の名前（`v4` など）と、ぶつからないようにするため。

// Prefix は、今の形の版の接頭辞である。
var Prefix = fmt.Sprintf("v%d/", snapshot.FormatVersion)

func CommitKey(repo, sha string) string   { return Prefix + repo + "/commits/" + sha }
func SnapshotKey(repo, sha string) string { return Prefix + repo + "/snapshots/" + sha }
func LatestKey(repo string) string        { return Prefix + repo + "/latest" }

// RepositoriesKey は、latest がある repository の名前の一覧のキーである。
var RepositoriesKey = Prefix + "repositories"

// ArtifactKey は、artifact の中身のキーである。中身は形を持たないので、版の外に置く。
func ArtifactKey(repo, sha256 string) string { return KeepPrefix + repo + "/artifacts/" + sha256 }

// KeepPrefix は、版の外に置くものの接頭辞である。
const KeepPrefix = "keep/"

// BranchKey は、ブランチの履歴のキーである。ブランチの名前は、`/` を含みうるので、パスとしてエスケープする。
func BranchKey(repo, branch string) string {
	return KeepPrefix + repo + "/branches/" + url.PathEscape(branch)
}

// BranchSnapshotKey は、ブランチの先頭のスナップショットのキーである。コミットの記録と差分を、1 つにまとめて置く。
// 消えたブランチの版は git から作り直せないので、版の外に置き、中に自分の形の版を持たせる。
func BranchSnapshotKey(repo, sha string) string { return KeepPrefix + repo + "/branch-snapshots/" + sha }

// BranchIndexKey は、履歴のあるブランチの名前の一覧のキーである。消えたブランチを見つけるのに使う。
func BranchIndexKey(repo string) string { return KeepPrefix + repo + "/branch-index" }
