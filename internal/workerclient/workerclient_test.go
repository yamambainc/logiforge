package workerclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

func TestBuildTreeErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		pending bool
		want    error
		calls   int
	}{
		{"作り終えるのを待ちきれない：送り直さない", 503, true, ports.ErrSnapshotPending, 1},
		{"worker が応答しない：1 度だけ送り直す", 503, false, ports.ErrUnavailable, 2},
		{"記録がない", 404, false, ports.ErrNotFound, 1},
		{"作った", 204, false, nil, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer k" || r.URL.Path != worker.PathBuildTree {
					t.Error("鍵かパスが違う")
				}
				if c.pending {
					w.Header().Set(worker.HeaderSnapshotPending, "true")
				}
				w.WriteHeader(c.status)
			}))
			defer srv.Close()
			cl := &Client{URL: srv.URL, Key: "k"}
			if err := cl.BuildTree(context.Background(), "q", "a"); !errors.Is(err, c.want) || (c.want == nil && err != nil) || calls != c.calls {
				t.Fatalf("got %v, calls %d", err, calls)
			}
		})
	}
}
