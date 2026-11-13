package snapshot

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
)

// Encode は、スナップショットを、gzip で圧縮した JSON にする。置き場には、この形で置く。
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
	return dec.Decode(v)
}
