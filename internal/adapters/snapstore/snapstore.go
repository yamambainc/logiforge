// Package snapstore は、スナップショットの置き場のポートのアダプタである（design/adapters/snapshot-store.md）。
// 置き場を読む部分と、worker の内部 API のクライアントからなる。置き場には書かない。
package snapstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

// Adapter は、ports.SnapshotStore を実装する。
type Adapter struct {
	// Store は、置き場を読む。バックエンドは、読む鍵だけを持つ。
	Store store.Reader
	// WorkerURL と Key は、worker の内部 API の宛先と鍵。
	WorkerURL string
	Key       string
	HTTP      *http.Client
}

var _ ports.SnapshotStore = (*Adapter)(nil)

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return ports.ErrNotFound
	}
	return err
}

func (a *Adapter) Repositories(ctx context.Context) ([]string, error) {
	return store.GetRepositories(ctx, a.Store)
}

func (a *Adapter) Latest(ctx context.Context, repo string) (string, error) {
	sha, err := store.GetLatest(ctx, a.Store, repo)
	if err != nil {
		return "", err
	}
	if sha == "" {
		return "", ports.ErrNotFound
	}
	return sha, nil
}

func (a *Adapter) Commit(ctx context.Context, repo, sha string) (snapshot.Commit, error) {
	c, err := store.GetCommit(ctx, a.Store, repo, sha)
	return c, notFound(err)
}

func (a *Adapter) Main(ctx context.Context, repo, sha string) (*snapshot.Main, error) {
	m, err := store.GetMain(ctx, a.Store, repo, sha)
	return m, notFound(err)
}

func (a *Adapter) Branch(ctx context.Context, repo, sha string) (*snapshot.Branch, error) {
	b, err := store.GetBranch(ctx, a.Store, repo, sha)
	return b, notFound(err)
}

func (a *Adapter) BranchHistory(ctx context.Context, repo, branch string) (store.BranchHistory, error) {
	h, _, err := store.GetBranchHistory(ctx, a.Store, repo, branch)
	return h, err
}

func (a *Adapter) Artifact(ctx context.Context, repo, sha256 string) ([]byte, error) {
	b, err := a.Store.Get(ctx, store.ArtifactKey(repo, sha256))
	return b, notFound(err)
}

func (a *Adapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (a *Adapter) post(ctx context.Context, path string, in, out any) (int, error) {
	status, _, err := a.postHeader(ctx, path, in, out)
	return status, err
}

func (a *Adapter) postHeader(ctx context.Context, path string, in, out any) (int, http.Header, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.WorkerURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.Key)
	res, err := a.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	if out != nil && res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return res.StatusCode, res.Header, err
		}
	}
	return res.StatusCode, res.Header, nil
}

// ReadBranch は、worker に「ブランチを読む」を頼む。応答がなければ、1 度だけ送り直す（design/backend/worker-api.md）。
func (a *Adapter) ReadBranch(ctx context.Context, repo, branch string) (ports.BranchRead, error) {
	var res worker.BranchResult
	var status int
	var header http.Header
	var err error
	for try := 0; try < 2; try++ {
		status, header, err = a.postHeader(ctx, worker.PathReadBranch, worker.ReadBranchRequest{Repository: repo, Branch: branch}, &res)
		if err == nil && header.Get(worker.HeaderSnapshotPending) != "" {
			return ports.BranchRead{}, ports.ErrSnapshotPending
		}
		if err == nil && status != http.StatusServiceUnavailable {
			break
		}
	}
	switch {
	case err != nil:
		return ports.BranchRead{}, fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
	case status == http.StatusNotFound:
		return ports.BranchRead{}, ports.ErrNotFound
	case status != http.StatusOK:
		return ports.BranchRead{}, fmt.Errorf("%w: worker が %d を返した", ports.ErrUnavailable, status)
	}
	return ports.BranchRead{Head: res.Head, Base: res.Base}, nil
}

// BuildTree は、worker に「木を作る」を頼む。応答がなければ、1 度だけ送り直す。
func (a *Adapter) BuildTree(ctx context.Context, repo, sha string) error {
	var status int
	var header http.Header
	var err error
	for try := 0; try < 2; try++ {
		status, header, err = a.postHeader(ctx, worker.PathBuildTree, worker.BuildTreeRequest{Repository: repo, Revision: sha}, nil)
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

// Wake は、worker を起こす。失敗は無視する。
func (a *Adapter) Wake(ctx context.Context, repo string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = a.post(ctx, worker.PathWake, worker.WakeRequest{Repository: repo}, nil)
}
