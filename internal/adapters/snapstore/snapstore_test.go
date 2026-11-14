package snapstore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

func TestReadBranchErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		pending bool
		want    error
		calls   int
	}{
		{"取り込みを待ちきれない：送り直さない", 503, true, ports.ErrSnapshotPending, 1},
		{"worker が応答しない：1 度だけ送り直す", 503, false, ports.ErrUnavailable, 2},
		{"ブランチがない", 404, false, ports.ErrNotFound, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer k" {
					t.Error("鍵を付けていない")
				}
				if c.pending {
					w.Header().Set(worker.HeaderSnapshotPending, "true")
				}
				w.WriteHeader(c.status)
			}))
			defer srv.Close()
			a := &Adapter{WorkerURL: srv.URL, Key: "k"}
			if _, err := a.ReadBranch(context.Background(), "q", "b"); !errors.Is(err, c.want) || calls != c.calls {
				t.Fatalf("got %v, calls %d", err, calls)
			}
		})
	}
}
