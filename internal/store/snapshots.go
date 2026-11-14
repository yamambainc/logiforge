package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
)

// GetCommit は、コミットの記録を読む。なければ ErrNotFound。
func GetCommit(ctx context.Context, r Reader, repo, sha string) (snapshot.Commit, error) {
	b, err := r.Get(ctx, CommitKey(repo, sha))
	if err != nil {
		return snapshot.Commit{}, err
	}
	var d snapshot.CommitDoc
	if err := snapshot.Decode(b, &d); err != nil {
		return snapshot.Commit{}, err
	}
	return d.Commit, nil
}

// GetMain は、main のコミットの記録と木を読む。どちらかがなければ ErrNotFound。
func GetMain(ctx context.Context, r Reader, repo, sha string) (*snapshot.Main, error) {
	c, err := GetCommit(ctx, r, repo, sha)
	if err != nil {
		return nil, err
	}
	if c.Kind != snapshot.KindMain {
		return nil, ErrNotFound
	}
	b, err := r.Get(ctx, SnapshotKey(repo, sha))
	if err != nil {
		return nil, err
	}
	var d snapshot.TreeDoc
	if err := snapshot.Decode(b, &d); err != nil {
		return nil, err
	}
	return &snapshot.Main{FormatVersion: snapshot.FormatVersion, Commit: c, Tree: d.Tree}, nil
}

// GetBranch は、ブランチの先頭のスナップショットを読む。
func GetBranch(ctx context.Context, r Reader, repo, sha string) (*snapshot.Branch, error) {
	c, err := GetCommit(ctx, r, repo, sha)
	if err != nil {
		return nil, err
	}
	if c.Kind != snapshot.KindBranch {
		return nil, ErrNotFound
	}
	b, err := r.Get(ctx, SnapshotKey(repo, sha))
	if err != nil {
		return nil, err
	}
	var d snapshot.DiffDoc
	if err := snapshot.Decode(b, &d); err != nil {
		return nil, err
	}
	return &snapshot.Branch{FormatVersion: snapshot.FormatVersion, Commit: c, Diff: d.Diff}, nil
}

// PutMain は、main のコミットの記録と木を置く。木を先に置き、コミットの記録をあとに置く。
func PutMain(ctx context.Context, w Writer, repo string, m *snapshot.Main) error {
	if err := PutTree(ctx, w, repo, m.Commit.Revision, &m.Tree); err != nil {
		return err
	}
	return PutCommit(ctx, w, repo, m.Commit)
}

// PutCommit は、main のコミットの記録だけを置く。main のすべてのコミットに置く（design/backend/snapshot.md）。
func PutCommit(ctx context.Context, w Writer, repo string, c snapshot.Commit) error {
	b, err := snapshot.Encode(&snapshot.CommitDoc{FormatVersion: snapshot.FormatVersion, Commit: c})
	if err != nil {
		return err
	}
	return w.Put(ctx, CommitKey(repo, c.Revision), b)
}

// PutTree は、main のコミットの木を置く。木を作るのは、latest、ブランチの base、API で指定されたリビジョンだけである。
func PutTree(ctx context.Context, w Writer, repo, sha string, t *snapshot.Tree) error {
	b, err := snapshot.Encode(&snapshot.TreeDoc{FormatVersion: snapshot.FormatVersion, Tree: *t})
	if err != nil {
		return err
	}
	return w.Put(ctx, SnapshotKey(repo, sha), b)
}

// HasTree は、main のコミットの木があるかを返す。
func HasTree(ctx context.Context, r Reader, repo, sha string) (bool, error) {
	_, err := r.Get(ctx, SnapshotKey(repo, sha))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// PutBranch は、ブランチの先頭のスナップショットを置く。
func PutBranch(ctx context.Context, w Writer, repo string, b *snapshot.Branch) error {
	diff, err := snapshot.Encode(&snapshot.DiffDoc{FormatVersion: snapshot.FormatVersion, Diff: b.Diff})
	if err != nil {
		return err
	}
	if err := w.Put(ctx, SnapshotKey(repo, b.Commit.Revision), diff); err != nil {
		return err
	}
	c, err := snapshot.Encode(&snapshot.CommitDoc{FormatVersion: snapshot.FormatVersion, Commit: b.Commit})
	if err != nil {
		return err
	}
	return w.Put(ctx, CommitKey(repo, b.Commit.Revision), c)
}

// GetLatest は、今の main の SHA を読む。なければ空。
func GetLatest(ctx context.Context, r Reader, repo string) (string, error) {
	b, err := r.Get(ctx, LatestKey(repo))
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

// SetLatest は、latest を、今の値が old（空ならキーがない）のときだけ書く。
func SetLatest(ctx context.Context, w Writer, repo, sha, old string) error {
	var o []byte
	if old != "" {
		o = []byte(old + "\n")
	}
	return w.PutIf(ctx, LatestKey(repo), []byte(sha+"\n"), o)
}

// BranchEntry は、ブランチの履歴の 1 項目である（design/backend/snapshot.md の「ブランチの履歴」）。
type BranchEntry struct {
	Head    string    `json:"head,omitempty"`
	Base    string    `json:"base,omitempty"`
	SeenAt  time.Time `json:"seen_at"`
	Deleted bool      `json:"deleted,omitempty"`
}

// BranchHistory は、ブランチの履歴である。先頭のコミットが変わった順に並ぶ。
type BranchHistory struct {
	FormatVersion int           `json:"format_version"`
	Branch        string        `json:"branch"`
	Entries       []BranchEntry `json:"entries"`
}

// GetBranchHistory は、ブランチの履歴を読む。なければ、空の履歴を返す。
// raw は、読んだときの中身で、PutBranchHistory の条件に渡す（なければ nil）。
func GetBranchHistory(ctx context.Context, r Reader, repo, branch string) (h BranchHistory, raw []byte, err error) {
	b, err := r.Get(ctx, BranchKey(repo, branch))
	if errors.Is(err, ErrNotFound) {
		return BranchHistory{FormatVersion: snapshot.FormatVersion, Branch: branch}, nil, nil
	}
	if err != nil {
		return BranchHistory{}, nil, err
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return BranchHistory{}, nil, err
	}
	return h, b, nil
}

// PutBranchHistory は、ブランチの履歴を、今の中身が raw のときだけ書く（条件付きの書き込み）。
// worker は 1 つだけ動かす前提だが、誤って 2 つ動いても、履歴の項目を落とさないようにするため。
func PutBranchHistory(ctx context.Context, w Writer, repo string, h BranchHistory, raw []byte) error {
	h.FormatVersion = snapshot.FormatVersion
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return w.PutIf(ctx, BranchKey(repo, h.Branch), b, raw)
}

// GetBranchIndex は、履歴のあるブランチの名前の一覧を読む（名前の順）。raw は、PutBranchIndex の条件に渡す。
func GetBranchIndex(ctx context.Context, r Reader, repo string) (names []string, raw []byte, err error) {
	b, err := r.Get(ctx, BranchIndexKey(repo))
	if errors.Is(err, ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return names, b, json.Unmarshal(b, &names)
}

// PutBranchIndex は、ブランチの名前の一覧を、今の中身が raw のときだけ書く。
func PutBranchIndex(ctx context.Context, w Writer, repo string, names []string, raw []byte) error {
	b, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return w.PutIf(ctx, BranchIndexKey(repo), b, raw)
}

// GetRepositories は、latest がある repository の名前の一覧を読む。
func GetRepositories(ctx context.Context, r Reader) ([]string, error) {
	b, err := r.Get(ctx, RepositoriesKey)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	return names, json.Unmarshal(b, &names)
}

// PutRepositories は、repository の名前の一覧を書く。
func PutRepositories(ctx context.Context, w Writer, names []string) error {
	b, err := json.Marshal(names)
	if err != nil {
		return err
	}
	return w.Put(ctx, RepositoriesKey, b)
}
