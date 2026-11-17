package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
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

// Broken は、repository が壊れていることの記録である（design/backend/broken.md）。壊れていなければ、置き場にない（か、空）。
type Broken struct {
	// LastGood は、最後に正しかった時点のコミットの SHA。今の main の並びにないことがある（main の書き換えのあと）。
	LastGood string `json:"last_good"`
	// First は、壊れた最初のコミットの SHA（反映によらないコミットか、書き換えのあとの main の先頭）。
	First string `json:"first"`
	// Reason は、no_apply_request か main_rewritten。
	Reason string `json:"reason"`
}

// BrokenKey は、壊れていることの記録のキーである。git だけからは作り直せない（書き換えの前の latest を失う）ので、keep/ の下に置く。
func BrokenKey(repo string) string { return KeepPrefix + repo + "/broken" }

// GetBroken は、壊れていることの記録を読む。壊れていなければ nil。
func GetBroken(ctx context.Context, r Reader, repo string) (*Broken, error) {
	b, err := r.Get(ctx, BrokenKey(repo))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var x Broken
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	if x.LastGood == "" {
		return nil, nil
	}
	return &x, nil
}

// PutBroken は、壊れていることの記録を書く。nil なら、壊れていない（空の記録）にする。
func PutBroken(ctx context.Context, w Writer, repo string, x *Broken) error {
	if x == nil {
		x = &Broken{}
	}
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	return w.Put(ctx, BrokenKey(repo), b)
}

// LoggedComment は、worker が見た記録のコメントの控えの 1 つである（design/backend/verify.md の「記録の控え」）。
type LoggedComment struct {
	ID        int64     `json:"id"`
	Issue     int       `json:"issue"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// RecordsKey は、pull request か issue の記録の控えのキーである。GitForge の repository の識別子（署名の場所と同じ、
// 名前を変えても owner を移しても変わらないもの）で分ける。なぜ：名前で分けると、名前が変わったとき、控えが古いキーに取り残されるから。
func RecordsKey(repoID string, number int) string {
	return KeepPrefix + "records/" + url.PathEscape(repoID) + "/" + strconv.Itoa(number)
}

// RecordsCursorKey は、記録の控えの、次に読む位置（コメントの updated_at）のキーである。
func RecordsCursorKey(repoID string) string {
	return KeepPrefix + "records/" + url.PathEscape(repoID) + "/cursor"
}

// GetRecords は、記録の控えを読む。なければ空。
func GetRecords(ctx context.Context, r Reader, repoID string, number int) ([]LoggedComment, error) {
	b, err := r.Get(ctx, RecordsKey(repoID, number))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []LoggedComment
	return out, json.Unmarshal(b, &out)
}

// AppendRecords は、記録の控えに、まだない id のものだけを足す。控えからは消さない。同じ id で中身が違うものは足さず、返す。
func AppendRecords(ctx context.Context, w Writer, repoID string, number int, add []LoggedComment) (changed []LoggedComment, err error) {
	cur, err := GetRecords(ctx, w, repoID, number)
	if err != nil {
		return nil, err
	}
	have := map[int64]string{}
	for _, c := range cur {
		have[c.ID] = c.Body
	}
	n := len(cur)
	for _, c := range add {
		if body, ok := have[c.ID]; ok {
			if body != c.Body {
				changed = append(changed, c)
			}
			continue
		}
		have[c.ID] = c.Body
		cur = append(cur, c)
	}
	if len(cur) == n {
		return changed, nil
	}
	b, err := json.Marshal(cur)
	if err != nil {
		return nil, err
	}
	return changed, w.Put(ctx, RecordsKey(repoID, number), b)
}

// GetRecordsCursor は、記録の控えの、次に読む位置を読む。なければ時刻のゼロ。
func GetRecordsCursor(ctx context.Context, r Reader, repoID string) (time.Time, error) {
	b, err := r.Get(ctx, RecordsCursorKey(repoID))
	if errors.Is(err, ErrNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
}

// PutRecordsCursor は、記録の控えの、次に読む位置を書く。
func PutRecordsCursor(ctx context.Context, w Writer, repoID string, t time.Time) error {
	return w.Put(ctx, RecordsCursorKey(repoID), []byte(t.UTC().Format(time.RFC3339Nano)+"\n"))
}

// GetScanned は、ディレクトリの repository の scanned を読む。なければ空。
func GetScanned(ctx context.Context, r Reader, repo string) (string, error) {
	b, err := r.Get(ctx, ScannedKey(repo))
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

// PutScanned は、ディレクトリの repository の scanned を書く。その SHA までの版を置き終えてから書く。
func PutScanned(ctx context.Context, w Writer, repo, sha string) error {
	return w.Put(ctx, ScannedKey(repo), []byte(sha+"\n"))
}

// MigratedToKey は、別リポジトリ方式から移した元の repository の、移した先の名前のキーである（design/directory/migrate.md）。
// worker が、移した先の用意のコミットを取り込んだときに書く。git から作り直せないので keep/ に置く。
func MigratedToKey(repo string) string { return KeepPrefix + repo + "/migrated_to" }

// GetMigratedTo は、移した先の名前を読む。移していなければ空。
func GetMigratedTo(ctx context.Context, r Reader, repo string) (string, error) {
	b, err := r.Get(ctx, MigratedToKey(repo))
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

// PutMigratedTo は、移した先の名前を書く。
func PutMigratedTo(ctx context.Context, w Writer, repo, to string) error {
	return w.Put(ctx, MigratedToKey(repo), []byte(to+"\n"))
}
