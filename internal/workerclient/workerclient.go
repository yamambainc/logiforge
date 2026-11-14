// Package workerclient は、バックエンドが worker の内部 API を呼ぶクライアントである（design/backend/worker-api.md）。
// worker は、どの構成でも同じものなので、ポートにしない。バックエンドは、このクライアントを直接使う。
package workerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

// Client は、worker の内部 API のクライアントである。
type Client struct {
	// URL と Key は、worker の内部 API の宛先と鍵。
	URL  string
	Key  string
	HTTP *http.Client
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (c *Client) post(ctx context.Context, path string, in any) (int, http.Header, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	res, err := c.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	return res.StatusCode, res.Header, nil
}

// BuildTree は、worker に「木を作る」を頼む。応答がなければ、1 度だけ送り直す。
// 記録がなければ ports.ErrNotFound、上限までに終わらなければ ports.ErrSnapshotPending、頼めなければ ports.ErrUnavailable。
func (c *Client) BuildTree(ctx context.Context, repo, sha string) error {
	var status int
	var header http.Header
	var err error
	for try := 0; try < 2; try++ {
		status, header, err = c.post(ctx, worker.PathBuildTree, worker.BuildTreeRequest{Repository: repo, Revision: sha})
		if err == nil && header.Get(worker.HeaderSnapshotPending) != "" {
			return ports.ErrSnapshotPending
		}
		if err == nil && status != http.StatusServiceUnavailable {
			break
		}
	}
	switch {
	case err != nil:
		return fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
	case status == http.StatusNotFound:
		return ports.ErrNotFound
	case status != http.StatusNoContent && status != http.StatusOK:
		return fmt.Errorf("%w: worker が %d を返した", ports.ErrUnavailable, status)
	}
	return nil
}

// Wake は、worker を起こす。失敗は無視する。worker は、起こされなくても間隔ごとに巡回するからである。
func (c *Client) Wake(ctx context.Context, repo string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _, _ = c.post(ctx, worker.PathWake, worker.WakeRequest{Repository: repo})
}
