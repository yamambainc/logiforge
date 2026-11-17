package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/snapshot"
)

// GetCommit は、コミットの記録を読む。main のコミットはフォーマットバージョンの接頭辞の下の記録から、ブランチの先頭はブランチのスナップショットから読む。
// なければ ErrNotFound。
func GetCommit(ctx context.Context, r Reader, repo, sha string) (snapshot.Commit, error) {
	b, err := r.Get(ctx, CommitKey(repo, sha))
	if errors.Is(err, ErrNotFound) {
		br, berr := GetBranch(ctx, r, repo, sha)
		if berr != nil {
			return snapshot.Commit{}, berr
		}
		return br.Commit, nil
	}
	if err != nil {
		return snapshot.Commit{}, err
	}
	var d snapshot.CommitDoc
	if err := snapshot.Decode(b, &d); err != nil {
		return snapshot.Commit{}, err
	}
	return d.Commit, nil
}

// GetMain は、main のコミットの記録とスナップショットツリーを読む。どちらかがなければ ErrNotFound。
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
	var d snapshot.SnapshotTreeDoc
	if err := snapshot.Decode(b, &d); err != nil {
		return nil, err
	}
	return &snapshot.Main{FormatVersion: snapshot.FormatVersion, Commit: c, SnapshotTree: d.SnapshotTree}, nil
}

// GetBranch は、ブランチの先頭のスナップショットを読む。古いフォーマットバージョンのものも読む（snapshot.MinBranchFormatVersion から）。
func GetBranch(ctx context.Context, r Reader, repo, sha string) (*snapshot.Branch, error) {
	b, err := r.Get(ctx, BranchSnapshotKey(repo, sha))
	if err != nil {
		return nil, err
	}
	var br snapshot.Branch
	if err := snapshot.DecodeOld(b, &br, snapshot.MinBranchFormatVersion); err != nil {
		return nil, err
	}
	if br.Commit.Kind != snapshot.KindBranch {
		return nil, ErrNotFound
	}
	return &br, nil
}

// PutCommit は、main のコミットの記録だけを置く。main のすべてのコミットに置く（design/backend/snapshot.md）。
func PutCommit(ctx context.Context, w Writer, repo string, c snapshot.Commit) error {
	b, err := snapshot.Encode(&snapshot.CommitDoc{FormatVersion: snapshot.FormatVersion, Commit: c})
	if err != nil {
		return err
	}
	return w.Put(ctx, CommitKey(repo, c.Revision), b)
}

// PutSnapshotTree は、main のコミットのスナップショットツリーを置く。main のすべてのコミットに置く（design/backend/snapshot.md の「すべてのコミットに作る」）。
func PutSnapshotTree(ctx context.Context, w Writer, repo, sha string, t *snapshot.SnapshotTree) error {
	b, err := snapshot.Encode(&snapshot.SnapshotTreeDoc{FormatVersion: snapshot.FormatVersion, SnapshotTree: *t})
	if err != nil {
		return err
	}
	return w.Put(ctx, SnapshotKey(repo, sha), b)
}

// PutMainIndex は、main の並びの索引の、番号 number のキーに、その SHA を書く。main が書き換えられたときは上書きする。
func PutMainIndex(ctx context.Context, w Writer, repo string, number int, sha string) error {
	return w.Put(ctx, MainIndexKey(repo, number), []byte(sha+"\n"))
}

// GetMainIndex は、main の並びの索引から、番号 number の main のコミットの SHA を読む。なければ ErrNotFound。
func GetMainIndex(ctx context.Context, r Reader, repo string, number int) (string, error) {
	b, err := r.Get(ctx, MainIndexKey(repo, number))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// HasSnapshotTree は、main のコミットのスナップショットツリーがあるかを返す。
func HasSnapshotTree(ctx context.Context, r Reader, repo, sha string) (bool, error) {
	_, err := r.Get(ctx, SnapshotKey(repo, sha))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// PutBranch は、ブランチの先頭のスナップショット（コミットの記録と差分）を、フォーマットバージョンの接頭辞の外に 1 つにまとめて置く。
func PutBranch(ctx context.Context, w Writer, repo string, b *snapshot.Branch) error {
	b.FormatVersion = snapshot.FormatVersion
	data, err := snapshot.Encode(b)
	if err != nil {
		return err
	}
	return w.Put(ctx, BranchSnapshotKey(repo, b.Commit.Revision), data)
}

// legacyBranchPrefixes は、ブランチのスナップショットを版の下に置いていた、古いフォーマットバージョンの接頭辞である。
var legacyBranchPrefixes = []string{"v4/"}

// MoveLegacyBranchSnapshots は、古いフォーマットバージョンの下に置いていたブランチのスナップショットを、版の外（BranchSnapshotKey）に移す。
// ブランチの履歴に出てくる先頭のコミットについて、移していないものだけを移す。何度動かしても、結果は同じになる。
// 古いフォーマットバージョンの接頭辞の下のものは消さない。移した数を返す。
func MoveLegacyBranchSnapshots(ctx context.Context, w Writer, repo string) (int, error) {
	names, _, err := GetBranchIndex(ctx, w, repo)
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, name := range names {
		h, _, err := GetBranchHistory(ctx, w, repo, name)
		if err != nil {
			return moved, err
		}
		for _, e := range h.Entries {
			if e.Head == "" {
				continue
			}
			if _, err := w.Get(ctx, BranchSnapshotKey(repo, e.Head)); err == nil {
				continue
			} else if !errors.Is(err, ErrNotFound) {
				return moved, err
			}
			for _, prefix := range legacyBranchPrefixes {
				br, err := readLegacyBranch(ctx, w, prefix, repo, e.Head)
				if errors.Is(err, ErrNotFound) {
					continue
				}
				if err != nil {
					return moved, err
				}
				if err := PutBranch(ctx, w, repo, br); err != nil {
					return moved, err
				}
				moved++
				break
			}
		}
	}
	return moved, nil
}

// readLegacyBranch は、古いフォーマットバージョンの下の、コミットの記録と差分を読み、1 つのブランチのスナップショットにする。
func readLegacyBranch(ctx context.Context, r Reader, prefix, repo, sha string) (*snapshot.Branch, error) {
	cb, err := r.Get(ctx, prefix+repo+"/commits/"+sha)
	if err != nil {
		return nil, err
	}
	var c snapshot.CommitDoc
	if err := snapshot.DecodeOld(cb, &c, snapshot.MinBranchFormatVersion); err != nil {
		return nil, err
	}
	if c.Commit.Kind != snapshot.KindBranch {
		return nil, ErrNotFound
	}
	db, err := r.Get(ctx, prefix+repo+"/snapshots/"+sha)
	if err != nil {
		return nil, err
	}
	var d snapshot.DiffDoc
	if err := snapshot.DecodeOld(db, &d, snapshot.MinBranchFormatVersion); err != nil {
		return nil, err
	}
	return &snapshot.Branch{FormatVersion: snapshot.FormatVersion, Commit: c.Commit, Diff: d.Diff}, nil
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
