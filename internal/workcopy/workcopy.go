// Package workcopy は、git の木から、ブランチの差分のスナップショットを作るコードと、
// バックエンドが持つ、repository ごとの作業用の git の複製である（design/backend/apply-request/create.md の「作成」）。
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

	"github.com/yamambainc/logiforge/internal/gitx"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/tree"
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
//
// ディレクトリの repository（g.Prefix がある）では、base を、その時点の版（base までにディレクトリを変えた、いちばん新しいコミット）に
// 引いて、スナップショットの base にする。版のほかのコミットには、スナップショットツリーがないからである。ディレクトリの中身は、
// base とその版で同じなので、差分は変わらない（design/directory/history.md の「版」）。
func BranchSnapshot(ctx context.Context, g *gitx.Repo, base, head string, maxSize int64) (*snapshot.Branch, tree.Files, error) {
	label := base
	if g.Prefix != "" {
		v, err := g.LastChange(ctx, base)
		if err != nil {
			return nil, nil, err
		}
		if v != "" {
			label = v
		}
	}
	c, err := g.ReadCommit(ctx, head)
	if err != nil {
		return nil, nil, err
	}
	changes, outside, err := g.DiffAll(ctx, base, head)
	if err != nil {
		return nil, nil, err
	}
	files, err := g.ApplyChanges(ctx, nil, changes, maxSize)
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
	// 差分は、本当の base から読む（ディレクトリの外の変化を、base と版のあいだのコードのコミットと混ぜないため）。
	b, err := snapshot.BuildBranch(label, deltas, files, CommitInfo(c))
	if b != nil {
		b.Outside = outside
	}
	return b, files, err
}

// Cache は、バックエンドが持つ、repository ごとの作業用の git の複製である。
//   - 初めて要るときに 1 度だけ clone し（空の bare リポジトリに fetch する）、そのあとの要求では fetch するだけにする。
//   - 複製は正本ではない。消えても、次の要求で作り直す。プロセスごとに持ち、プロセスの間で共有しない。
//   - 同じ repository への要求は、repository ごとのロックで 1 つずつ通す。fetch と読むことが、互いに混ざらないようにするため。
type Cache struct {
	// Dir は、複製を置くディレクトリである。一時的なディスクに、プロセスごとに作る（NewCache）。
	Dir string
	// Env は、git に渡す環境変数（使える転送の方式など）である。
	Env []string
	// Credentials は、repository（gitforge_ref）を git で読むための資格情報の環境変数を返す。git を読む前に、毎回呼ぶ
	// （GitHub の installation token は切れるので）。nil なら、資格情報を渡さない。
	Credentials func(ctx context.Context, gitForgeRef string) ([]string, error)
	// URL は、GitForge の repository の参照から、git の URL を返す。
	URL         func(gitForgeRef string) string
	MaxFileSize int64

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewCache は、base の下に、このプロセスだけのディレクトリを作り、Cache を返す。
// 先に、base の下の古い複製（前のプロセスが、消さずに止まったもの）を消す。複製は使い捨てのキャッシュだからである。
// base は、プロセスごとの一時的なディスクに置く。ほかのプロセスと共有しない。
func NewCache(base string, env []string, url func(string) string) (*Cache, error) {
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	old, err := filepath.Glob(filepath.Join(base, "workcopy-*"))
	if err != nil {
		return nil, err
	}
	for _, d := range old {
		if err := os.RemoveAll(d); err != nil {
			return nil, fmt.Errorf("古い作業用の複製を消せない：%w", err)
		}
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
// ディレクトリの repository では、ディレクトリの中だけを読む（gitx.Repo.Prefix）。
func (c *Cache) repo(ctx context.Context, name, gitForgeRef string) (*gitx.Repo, error) {
	dir := filepath.Join(c.Dir, name+".git")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if _, err := gitx.Init(ctx, dir); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	_, prefix := ports.SplitRef(gitForgeRef)
	return &gitx.Repo{Dir: dir, Env: c.Env, Prefix: prefix}, nil
}

// fetch は、資格情報を付けて、GitForge から branches を fetch する。資格情報は、ネットワークを使うここでだけ求める。
// なぜ：手元の複製だけで読む処理（ReadFiles）を、GitForge の API（installation token の作り直し）に頼らせないため。
func (c *Cache) fetch(ctx context.Context, g *gitx.Repo, gitForgeRef string, branches ...string) error {
	if c.Credentials != nil {
		cred, err := c.Credentials(ctx, gitForgeRef)
		if err != nil {
			return fmt.Errorf("%w: git の資格情報：%v", ports.ErrUnavailable, err)
		}
		withCred := *g
		withCred.Env = append(append([]string(nil), g.Env...), cred...)
		g = &withCred
	}
	return g.FetchBranches(ctx, c.URL(gitForgeRef), branches...)
}

// ReadBranch は、main とブランチを fetch し、ブランチの先頭と main の分かれた点（base）を求め、
// base からの差分のスナップショットを返す。ブランチがなければ ports.ErrNotFound。
func (c *Cache) ReadBranch(ctx context.Context, repo, gitForgeRef, branch string) (*snapshot.Branch, error) {
	if !validBranch(branch) {
		return nil, ports.ErrNotFound
	}
	defer c.lock(repo)()
	g, err := c.repo(ctx, repo, gitForgeRef)
	if err != nil {
		return nil, err
	}
	if err := c.fetch(ctx, g, gitForgeRef, "main", branch); err != nil {
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
// 置き場に base のスナップショットツリーがないときに、バックエンドが base の木を読むのに使う。
func (c *Cache) ReadFiles(ctx context.Context, repo, gitForgeRef, sha string) (tree.Files, error) {
	defer c.lock(repo)()
	g, err := c.repo(ctx, repo, gitForgeRef)
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

// ReachedMain は、ディレクトリの repository で、worker が読み終えた main の先頭 scanned が、head と同じか、head より先かを返す。
// main を fetch して、git で確かめる（design/directory/main.md の「反映の順番」）。
func (c *Cache) ReachedMain(ctx context.Context, repo, gitForgeRef, head, scanned string) (bool, error) {
	if head == scanned {
		return true, nil
	}
	defer c.lock(repo)()
	g, err := c.repo(ctx, repo, gitForgeRef)
	if err != nil {
		return false, err
	}
	if err := c.fetch(ctx, g, gitForgeRef, "main"); err != nil {
		return false, fmt.Errorf("%w: git fetch: %v", ports.ErrUnavailable, err)
	}
	for _, sha := range []string{head, scanned} {
		if ok, err := g.HasCommit(ctx, sha); err != nil || !ok {
			return false, err
		}
	}
	return g.IsAncestor(ctx, head, scanned)
}
