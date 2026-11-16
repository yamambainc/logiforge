package snapshot

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
)

// Encode は、スナップショットを、gzip で圧縮した JSON にする。置き場には、この形で置く。
// 同じスナップショットからは、同じバイト列になる。ただし、gzip の出力は Go のバージョンによって変わりうるので、
// 同じ Go のバージョンで作ったときに限る。中身の同じさは、JSON を読んで比べる。
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MinBranchFormatVersion は、読めるブランチのスナップショットの、いちばん古いフォーマットバージョンである。
// ブランチのスナップショットは、git から作り直せないので、フォーマットバージョンが上がっても、古い形のまま読む（design/backend/ports/snapshot-store.md）。
// 形を変えるときは、古い形も読めるようにするか、移す処理を足す。
const MinBranchFormatVersion = 4

// Decode は、Encode したものを読む。フォーマットバージョンが、今の版でなければ誤りにする。
func Decode(data []byte, v any) error {
	return decode(data, v, FormatVersion)
}

// DecodeOld は、Encode したものを、フォーマットバージョンが min から今の版までなら読む。ブランチのスナップショットと、古い版から移すときに使う。
func DecodeOld(data []byte, v any, min int) error {
	return decode(data, v, min)
}

func decode(data []byte, v any, min int) error {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	vd, ok := v.(versioned)
	if !ok {
		return nil
	}
	if got := vd.formatVersion(); got < min || got > FormatVersion {
		return fmt.Errorf("snapshot: フォーマットバージョンが %d で、%d から %d の間でない", got, min, FormatVersion)
	}
	return nil
}

type versioned interface{ formatVersion() int }

func (m *Main) formatVersion() int            { return m.FormatVersion }
func (b *Branch) formatVersion() int          { return b.FormatVersion }
func (d *CommitDoc) formatVersion() int       { return d.FormatVersion }
func (d *SnapshotTreeDoc) formatVersion() int { return d.FormatVersion }
func (d *DiffDoc) formatVersion() int         { return d.FormatVersion }

// CommitDoc は、置き場の <repository>/commits/<SHA> に置く、コミットの記録である。
type CommitDoc struct {
	FormatVersion int    `json:"format_version"`
	Commit        Commit `json:"commit"`
}

// SnapshotTreeDoc は、置き場の <repository>/snapshots/<SHA> に置く、main のコミットのスナップショットツリーである。
type SnapshotTreeDoc struct {
	FormatVersion int          `json:"format_version"`
	SnapshotTree  SnapshotTree `json:"snapshot_tree"`
}

// DiffDoc は、置き場の <repository>/snapshots/<SHA> に置く、ブランチの先頭の差分である。
type DiffDoc struct {
	FormatVersion int  `json:"format_version"`
	Diff          Diff `json:"diff"`
}
