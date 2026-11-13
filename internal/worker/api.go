package worker

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
)

// 内部 API のパス（design/backend/worker-api.md）。
const (
	PathReadBranch = "/internal/v1/read-branch"
	PathWake       = "/internal/v1/wake"
)

// ReadBranchRequest は、「ブランチを読む」の要求である。
type ReadBranchRequest struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
}

// WakeRequest は、「起こす」の要求である。
type WakeRequest struct {
	Repository string `json:"repository"`
}

// Handler は、worker の内部 API の HTTP ハンドラーを返す。key は、バックエンドと共有する鍵で、Bearer トークンとして確かめる。
func (w *Worker) Handler(key string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathReadBranch, func(rw http.ResponseWriter, r *http.Request) {
		var req ReadBranchRequest
		if !decode(rw, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		res, err := w.ReadBranch(ctx, req.Repository, req.Branch)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			http.Error(rw, "not found", http.StatusNotFound)
		case err != nil:
			w.cfg.Log.Error("ブランチを読めない", "repository", req.Repository, "branch", req.Branch, "err", err)
			http.Error(rw, "unavailable", http.StatusServiceUnavailable)
		default:
			rw.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(rw).Encode(res)
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
	return authorize(key, mux)
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
