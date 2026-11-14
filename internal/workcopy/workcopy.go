// Package workcopy は、git の木から、ブランチの差分のスナップショットを作るコードと、
// バックエンドが持つ、repository ごとの作業用の git の複製である（design/backend/apply-request.md の「作成」）。
// 差分のスナップショットを作るコードは、worker とバックエンドが同じものを使う。
package workcopy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yamambainc/hypothesis-verification-harness/internal/gitx"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// CommitInfo は、git から読んだコミットを、スナップショットを作るための形にする。
func CommitInfo(c gitx.Commit) snapshot.CommitInfo {
	info := snapshot.CommitInfo{Revision: c.SHA, CommittedAt: c.CommittedAt}
	for _, t := range c.Trailers {
		info.Trailers = append(info.Trailers, snapshot.Trailer{Key: t.Key, Value: t.Value})
	}
	return info
}

// BranchSnapshot は、base から head への差分のスナップショットを作る。読むのは、変わったファイルだけである。
// files は、head での、加わったか変わったファイルである（artifact の中身を置くのに使う）。
func BranchSnapshot(ctx context.Context, g *gitx.Repo, base, head string, maxSize int64) (*snapshot.Branch, tree.Files, error) {
	c, err := g.ReadCommit(ctx, head)
	if err != nil {
		return nil, nil, err
	}
	changes, files, err := g.ChangedFiles(ctx, base, head, maxSize)
	if err != nil {
		return nil, nil, err
	}
	var deltas []snapshot.Delta
	for _, ch := range changes {
		d := snapshot.Delta{Path: ch.Path, Status: snapshot.Modified, BaseSHA: ch.OldSHA}
		switch ch.Status {
		case "A":
			d.Status, d.BaseSHA = snapshot.Added, ""
		case "D":
			d.Status = snapshot.Deleted
		}
		deltas = append(deltas, d)
	}
	b, err := snapshot.BuildBranch(base, deltas, files, CommitInfo(c))
	return b, files, err
}

// Cache は、バックエンドが持つ、repository ごとの作業用の git の複製である。
//   - 初めて要るときに 1 度だけ clone し（空の bare リポジトリに fetch する）、そのあとの要求では fetch するだけにする。
//   - 複製は正本ではない。消えても、次の要求で作り直す。プロセスごとに持ち、プロセスの間で共有しない。
//   - 同じ repository への要求は、repository ごとのロックで 1 つずつ通す。fetch と読むことが、互いに混ざらないようにするため。
type Cache struct {
	// Dir は、複製を置くディレクトリである。一時的なディスクに、プロセスごとに作る（NewCache）。
	Dir string
	// Env は、git に渡す環境変数（読むだけの資格情報の credential helper と、使える転送の方式）である。
	Env []string
	// URL は、GitForge の repository の参照から、git の URL を返す。
	URL         func(gitForgeRef string) string
	MaxFileSize int64

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewCache は、base の下に、このプロセスだけのディレクトリを作り、Cache を返す。
func NewCache(base string, env []string, url func(string) string) (*Cache, error) {
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(base, "workcopy-")
	if err != nil {
		return nil, err
	}
	return &Cache{Dir: dir, Env: env, URL: url, MaxFileSize: tree.MaxArtifactSize}, nil
}

func (c *Cache) lock(repo string) func() {
	c.mu.Lock()
	if c.locks == nil {
		c.locks = map[string]*sync.Mutex{}
	}
	l, ok := c.locks[repo]
	if !ok {
		l = &sync.Mutex{}
		c.locks[repo] = l
	}
	c.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// repo は、repository の複製を返す。なければ作る（空の bare リポジトリ。最初の fetch が clone の代わりになる）。
func (c *Cache) repo(ctx context.Context, name string) (*gitx.Repo, error) {
	dir := filepath.Join(c.Dir, name+".git")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if _, err := gitx.Init(ctx, dir); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &gitx.Repo{Dir: dir, Env: c.Env}, nil
}

// ReadBranch は、main とブランチを fetch し、ブランチの先頭と main の分かれた点（base）を求め、
// base からの差分のスナップショットを返す。ブランチがなければ ports.ErrNotFound。
func (c *Cache) ReadBranch(ctx context.Context, repo, gitForgeRef, branch string) (*snapshot.Branch, error) {
	if !validBranch(branch) {
		return nil, ports.ErrNotFound
	}
	defer c.lock(repo)()
	g, err := c.repo(ctx, repo)
	if err != nil {
		return nil, err
	}
	if err := g.FetchBranches(ctx, c.URL(gitForgeRef), "main", branch); err != nil {
		if errors.Is(err, gitx.ErrNoRef) {
			return nil, ports.ErrNotFound
		}
		return nil, fmt.Errorf("%w: git fetch: %v", ports.ErrUnavailable, err)
	}
	heads, err := g.LocalHeads(ctx)
	if err != nil {
		return nil, err
	}
	head, mainSHA := heads[branch], heads["main"]
	if head == "" || mainSHA == "" {
		return nil, ports.ErrNotFound
	}
	base, err := g.MergeBase(ctx, mainSHA, head)
	if err != nil {
		return nil, err
	}
	b, _, err := BranchSnapshot(ctx, g, base, head, c.MaxFileSize)
	return b, err
}

// ReadFiles は、複製から、コミット sha の木のファイルを読む。ReadBranch で fetch したコミットだけを読める。
// 置き場に base の木がないときに、バックエンドが base の木を読むのに使う。
func (c *Cache) ReadFiles(ctx context.Context, repo, gitForgeRef, sha string) (tree.Files, error) {
	defer c.lock(repo)()
	g, err := c.repo(ctx, repo)
	if err != nil {
		return nil, err
	}
	return g.ReadFiles(ctx, sha, c.MaxFileSize)
}

// validBranch は、refspec に入れてよいブランチの名前かを返す。git の ref の名前の規則のうち、refspec で意味を持つ文字を除く。
func validBranch(b string) bool {
	if b == "" || strings.HasPrefix(b, "-") || strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") || strings.Contains(b, "..") {
		return false
	}
	return !strings.ContainsAny(b, ":*?[\\^~ \t\n")
}
