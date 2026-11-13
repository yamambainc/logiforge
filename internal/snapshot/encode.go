package snapshot

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
)

// Encode は、スナップショットを、gzip で圧縮した JSON にする。置き場には、この形で置く。
// 同じスナップショットからは、同じバイト列になる。ただし、gzip の出力は Go の版によって変わりうるので、
// 同じ Go の版で作ったときに限る。中身の同じさは、JSON を読んで比べる。
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

// Decode は、Encode したものを読む。
func Decode(data []byte, v any) error {
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
	var got int
	switch s := v.(type) {
	case *Main:
		got = s.FormatVersion
	case *Branch:
		got = s.FormatVersion
	default:
		return nil
	}
	if got != FormatVersion {
		return fmt.Errorf("snapshot: 形の版が %d で、%d でない", got, FormatVersion)
	}
	return nil
}
