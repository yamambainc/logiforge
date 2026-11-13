// Package api は、バックエンドの HTTP の API である（design/api/README.md、design/api/openapi.yaml）。
// ルールは持たない。要求を読み、バックエンドを呼び、応答を書くだけである。
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/backend"
)

// statuses は、エラーの種類と HTTP の status の対応である（1 対 1）。
var statuses = map[backend.Code]int{
	backend.CodeValidationFailed:        http.StatusUnprocessableEntity,
	backend.CodeUnauthenticated:         http.StatusUnauthorized,
	backend.CodeForbidden:               http.StatusForbidden,
	backend.CodeSessionRequired:         http.StatusBadRequest,
	backend.CodeNotFound:                http.StatusNotFound,
	backend.CodeSameSession:             http.StatusForbidden,
	backend.CodeNotOpen:                 http.StatusConflict,
	backend.CodeVersionConflict:         http.StatusConflict,
	backend.CodeTooLarge:                http.StatusRequestEntityTooLarge,
	backend.CodeRepositoryExists:        http.StatusConflict,
	backend.CodeUnavailable:             http.StatusServiceUnavailable,
	backend.CodeSnapshotPending:         http.StatusServiceUnavailable,
	backend.CodeRepositoryNotRegistered: http.StatusUnprocessableEntity,
	backend.CodeInternal:                http.StatusInternalServerError,
}

// problem は、RFC 9457 の application/problem+json の応答である。
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code"`
}

type server struct {
	b   *backend.Backend
	log *slog.Logger
}

// NewHandler は、API の HTTP ハンドラーを返す。パスは /v1 から始まる。
func NewHandler(b *backend.Backend, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	s := &server{b: b, log: log}
	mux := http.NewServeMux()
	route := func(pattern string, h func(ctx context.Context, c backend.Caller, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, s.handle(h))
	}
	const repo = "/v1/repositories/{repository}"
	route("GET /v1/repositories", s.listRepositories)
	route("GET "+repo, s.getRepository)
	route("GET "+repo+"/tree", s.getTree)
	route("GET "+repo+"/tree/nodes", s.listNodes)
	route("GET "+repo+"/tree/completion", s.getCompletion)
	route("GET "+repo+"/tree/export", s.exportTree)
	route("GET "+repo+"/nodes/{nodeId}", s.getNode)
	route("GET "+repo+"/nodes/{nodeId}/history", s.getNodeHistory)
	route("GET "+repo+"/apply-requests", s.listApplyRequests)
	route("GET "+repo+"/apply-requests/{applyRequestId}", s.getApplyRequest)
	route("GET "+repo+"/apply-requests/{applyRequestId}/review-material", s.getReviewMaterial)
	route("GET "+repo+"/work", s.listWork)
	route("GET "+repo+"/artifacts/{sha256}", s.getArtifact)
	route("GET "+repo+"/verdicts/{verdictId}", s.getVerdict)
	route("GET "+repo+"/human-requests", s.listHumanRequests)
	route("GET "+repo+"/human-requests/{humanRequestId}", s.getHumanRequest)
	route("GET "+repo+"/human-requests/{humanRequestId}/attachments/{sha256}", s.getAnswerAttachment)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, &backend.Error{Code: backend.CodeNotFound, Detail: "そのパスの API はない"})
	})
	return mux
}

// raw は、JSON でない応答（バイト列や Markdown）である。
type raw struct {
	contentType string
	body        []byte
}

func (s *server) handle(h func(ctx context.Context, c backend.Caller, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := s.b.Authenticate(r.Context(), strings.TrimSpace(token))
		if err == nil {
			var out any
			out, err = h(r.Context(), c, r)
			if err == nil {
				if rw, ok := out.(raw); ok {
					w.Header().Set("Content-Type", rw.contentType)
					_, _ = w.Write(rw.body)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(out)
				return
			}
		}
		e := backend.AsError(err)
		if e == nil {
			s.log.Error("内部の誤り", "method", r.Method, "path", r.URL.Path, "err", err)
			e = &backend.Error{Code: backend.CodeInternal, Detail: "内部の誤り"}
		}
		writeProblem(w, e)
	}
}

func writeProblem(w http.ResponseWriter, e *backend.Error) {
	status := statuses[e.Code]
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: e.Detail, Code: string(e.Code)})
}

func queryInt(r *http.Request, name string, def int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, &backend.Error{Code: backend.CodeValidationFailed, Detail: name + " は整数"}
	}
	return n, nil
}

func (s *server) listRepositories(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		return nil, err
	}
	return s.b.ListRepositories(ctx, c, r.URL.Query().Get("cursor"), limit)
}

func (s *server) getRepository(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetRepository(ctx, c, r.PathValue("repository"))
}

func (s *server) getTree(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetTree(ctx, c, r.PathValue("repository"), r.URL.Query().Get("revision"))
}

func (s *server) listNodes(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	depth, err := queryInt(r, "depth", -1)
	if err != nil {
		return nil, err
	}
	if r.URL.Query().Has("depth") && depth < 0 {
		return nil, &backend.Error{Code: backend.CodeValidationFailed, Detail: "depth は 0 以上"}
	}
	q := r.URL.Query()
	return s.b.ListNodes(ctx, c, r.PathValue("repository"), q.Get("revision"), q.Get("root"), depth)
}

func (s *server) getCompletion(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetCompletion(ctx, c, r.PathValue("repository"))
}

func (s *server) exportTree(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	format := r.URL.Query().Get("format")
	if format != "json" && format != "markdown" {
		return nil, &backend.Error{Code: backend.CodeValidationFailed, Detail: "format は json か markdown"}
	}
	e, err := s.b.ExportTree(ctx, c, r.PathValue("repository"), r.URL.Query().Get("revision"))
	if err != nil {
		return nil, err
	}
	if format == "markdown" {
		return raw{contentType: "text/markdown; charset=utf-8", body: []byte(e.Markdown())}, nil
	}
	return e, nil
}

func (s *server) getNode(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetNode(ctx, c, r.PathValue("repository"), r.PathValue("nodeId"), r.URL.Query().Get("revision"))
}

func (s *server) getNodeHistory(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		return nil, err
	}
	return s.b.GetNodeHistory(ctx, c, r.PathValue("repository"), r.PathValue("nodeId"), r.URL.Query().Get("cursor"), limit)
}

func (s *server) listApplyRequests(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	return s.b.ListApplyRequests(ctx, c, r.PathValue("repository"), q.Get("state"), q.Get("cursor"), limit)
}

func (s *server) getApplyRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetApplyRequest(ctx, c, r.PathValue("repository"), r.PathValue("applyRequestId"))
}

func (s *server) getReviewMaterial(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetReviewMaterial(ctx, c, r.PathValue("repository"), r.PathValue("applyRequestId"))
}

func (s *server) listWork(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	wq := backend.WorkQuery{Subtree: q.Get("subtree"), Cursor: q.Get("cursor"), Limit: limit}
	if k := q.Get("kind"); k != "" {
		wq.Kinds = strings.Split(k, ",")
	}
	if v := q.Get("include_claimed"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, &backend.Error{Code: backend.CodeValidationFailed, Detail: "include_claimed は真偽値"}
		}
		wq.IncludeClaimed = b
	}
	return s.b.ListWork(ctx, c, r.PathValue("repository"), wq)
}

func (s *server) getArtifact(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	data, err := s.b.GetArtifact(ctx, c, r.PathValue("repository"), r.PathValue("sha256"), r.URL.Query().Get("revision"))
	if err != nil {
		return nil, err
	}
	return raw{contentType: "application/octet-stream", body: data}, nil
}

func (s *server) getVerdict(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetVerdict(ctx, c, r.PathValue("repository"), r.PathValue("verdictId"))
}

func (s *server) listHumanRequests(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	limit, err := queryInt(r, "limit", 0)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	return s.b.ListHumanRequests(ctx, c, r.PathValue("repository"), q.Get("kind"), q.Get("state"), q.Get("node_id"), q.Get("cursor"), limit)
}

func (s *server) getHumanRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetHumanRequest(ctx, c, r.PathValue("repository"), r.PathValue("humanRequestId"))
}

func (s *server) getAnswerAttachment(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	data, err := s.b.GetAnswerAttachment(ctx, c, r.PathValue("repository"), r.PathValue("humanRequestId"), r.PathValue("sha256"))
	if err != nil {
		return nil, err
	}
	return raw{contentType: "application/octet-stream", body: data}, nil
}
