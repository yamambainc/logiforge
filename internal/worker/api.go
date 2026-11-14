package worker

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
)

// 内部 API のパス（design/backend/worker-api.md）。
const (
	PathWake      = "/internal/v1/wake"
	PathBuildTree = "/internal/v1/build-tree"
	PathHealthz   = "/healthz"
)

// HeaderSnapshotPending は、「木を作る」が、作り終えるのを待ちきれなかったことを表す応答のヘッダーである。
const HeaderSnapshotPending = "Logiforge-Snapshot-Pending"

// BuildTreeRequest は、「木を作る」の要求である。
type BuildTreeRequest struct {
	Repository string `json:"repository"`
	Revision   string `json:"revision"`
}

// WakeRequest は、「起こす」の要求である。
type WakeRequest struct {
	Repository string `json:"repository"`
}

// Handler は、worker の内部 API の HTTP ハンドラーを返す。key は、バックエンドと共有する鍵で、Bearer トークンとして確かめる。
func (w *Worker) Handler(key string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathBuildTree, func(rw http.ResponseWriter, r *http.Request) {
		var req BuildTreeRequest
		if !decode(rw, r, &req) {
			return
		}
		err := w.BuildTree(r.Context(), req.Repository, req.Revision)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			http.Error(rw, "not found", http.StatusNotFound)
		case errors.Is(err, ErrSnapshotPending):
			rw.Header().Set(HeaderSnapshotPending, "true")
			http.Error(rw, "snapshot pending", http.StatusServiceUnavailable)
		case err != nil:
			w.cfg.Log.Error("木を作れない", "repository", req.Repository, "revision", req.Revision, "err", err)
			http.Error(rw, "unavailable", http.StatusServiceUnavailable)
		default:
			rw.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("POST "+PathWake, func(rw http.ResponseWriter, r *http.Request) {
		var req WakeRequest
		if !decode(rw, r, &req) {
			return
		}
		w.Wake(req.Repository)
		rw.WriteHeader(http.StatusAccepted)
	})
	// ヘルスチェックだけは、鍵を求めない。中身は返さない（design/deploy.md）。
	outer := http.NewServeMux()
	// 巡回が止まっていれば（Healthy）、503 を返し、コンテナを作り直させる。
	outer.HandleFunc("GET "+PathHealthz, func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !w.Healthy() {
			rw.WriteHeader(http.StatusServiceUnavailable)
			_, _ = rw.Write([]byte("stalled\n"))
			return
		}
		_, _ = rw.Write([]byte("ok\n"))
	})
	outer.Handle("/", authorize(key, mux))
	return outer
}

func authorize(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" || !ok || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(rw, r)
	})
}

func decode(rw http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}
