// Package api は、バックエンドの HTTP の API である（design/api/README.md、design/api/openapi.yaml）。
// ルールは持たない。要求を読み、バックエンドを呼び、応答を書くだけである。
package api

import (
	"context"
	"encoding/json"
	"errors"
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
	backend.CodeCommitConflict:          http.StatusConflict,
	backend.CodeOverlayConflict:         http.StatusConflict,
	backend.CodeOverlayUnsupported:      http.StatusUnprocessableEntity,
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
	route("POST /v1/repositories", s.createRepository)
	route("POST "+repo+"/apply-requests", s.createApplyRequest)
	route("PATCH "+repo+"/apply-requests/{applyRequestId}", s.updateApplyRequest)
	route("POST "+repo+"/apply-requests/{applyRequestId}/reviews", s.reviewApplyRequest)
	route("POST "+repo+"/apply-requests/{applyRequestId}/close", s.closeApplyRequest)
	route("POST "+repo+"/human-requests", s.createHumanRequest)
	route("POST "+repo+"/human-requests/{humanRequestId}/answer", s.answerHumanRequest)
	// ヘルスチェック。/v1 の外に置き、認証を求めない。GitForge や置き場には触れない（design/deploy.md）。
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, &backend.Error{Code: backend.CodeNotFound, Detail: "そのパスの API はない"})
	})
	return mux
}

// raw は、JSON でない応答（バイト列や Markdown）である。
type raw struct {
	contentType string
	body        []byte
	// attachment は、利用者やエージェントが置いた任意のファイルである。保存させるだけにし、同じオリジンの文書として動かさない
	// （design/webui/security.md の「ファイルの開き方」）。
	attachment bool
}

// created は、書き込みの応答である。status と、書いたものが読む側に出ていないときのコミットを持つ。
type created struct {
	status  int
	pending string
	body    any
}

// MaxBodySize は、1 回の要求の本文の上限である（design/api/README.md の「アップロードの大きさ」）。
const MaxBodySize = 32 << 20

func (s *server) handle(h func(ctx context.Context, c backend.Caller, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := s.b.Authenticate(r.Context(), strings.TrimSpace(token))
		if err == nil {
			var out any
			out, err = h(r.Context(), c, r)
			if err == nil {
				if rw, ok := out.(raw); ok {
					s.writeRaw(w, rw)
					return
				}
				if cr, ok := out.(created); ok {
					if cr.pending != "" {
						w.Header().Set("Logiforge-Pending-Commit", cr.pending)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(cr.status)
					_ = json.NewEncoder(w).Encode(cr.body)
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

func (s *server) writeRaw(w http.ResponseWriter, rw raw) {
	w.Header().Set("Content-Type", rw.contentType)
	if rw.attachment {
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	_, _ = w.Write(rw.body)
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
	return s.b.GetTree(ctx, c, r.PathValue("repository"), at(r))
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
	return s.b.ListNodes(ctx, c, r.PathValue("repository"), at(r), q.Get("root"), depth)
}

func (s *server) getCompletion(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetCompletion(ctx, c, r.PathValue("repository"))
}

func (s *server) exportTree(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	format := r.URL.Query().Get("format")
	if format != "json" && format != "markdown" {
		return nil, &backend.Error{Code: backend.CodeValidationFailed, Detail: "format は json か markdown"}
	}
	e, err := s.b.ExportTree(ctx, c, r.PathValue("repository"), r.URL.Query().Get("commit"))
	if err != nil {
		return nil, err
	}
	if format == "markdown" {
		return raw{contentType: "text/markdown; charset=utf-8", body: []byte(e.Markdown())}, nil
	}
	return e, nil
}

func (s *server) getNode(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	return s.b.GetNode(ctx, c, r.PathValue("repository"), r.PathValue("nodeId"), at(r))
}

// at は、木を読む時点（commit か apply_request）を、クエリから読む。
func at(r *http.Request) backend.At {
	q := r.URL.Query()
	return backend.At{Commit: q.Get("commit"), ApplyRequest: q.Get("apply_request")}
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
	data, err := s.b.GetArtifact(ctx, c, r.PathValue("repository"), r.PathValue("sha256"), r.URL.Query().Get("commit"))
	if err != nil {
		return nil, err
	}
	return raw{contentType: "application/octet-stream", body: data, attachment: true}, nil
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
	return raw{contentType: "application/octet-stream", body: data, attachment: true}, nil
}

// decodeBody は、JSON の本文を読む。32 MiB を超えれば too_large、形が正しくなければ validation_failed にする。
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodySize))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &backend.Error{Code: backend.CodeTooLarge, Detail: "要求の本文が 32 MiB を超える"}
		}
		return &backend.Error{Code: backend.CodeValidationFailed, Detail: "本文の JSON が正しくない：" + err.Error()}
	}
	return nil
}

func session(r *http.Request) string { return r.Header.Get("Logiforge-Session-Id") }

func (s *server) createRepository(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	var in struct {
		Name       string `json:"name"`
		Statement  string `json:"statement"`
		Background string `json:"background"`
	}
	if err := decodeBody(nil, r, &in); err != nil {
		return nil, err
	}
	out, err := s.b.CreateRepository(ctx, c, in.Name, in.Statement, in.Background)
	if err != nil {
		return nil, err
	}
	return created{status: http.StatusCreated, pending: out.Pending, body: out.Value}, nil
}

func (s *server) createApplyRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	var in struct {
		Branch      string `json:"branch"`
		Commit      string `json:"commit"`
		Description string `json:"description"`
	}
	if err := decodeBody(nil, r, &in); err != nil {
		return nil, err
	}
	out, err := s.b.CreateApplyRequest(ctx, c, r.PathValue("repository"), backend.NewApplyRequest{Branch: in.Branch, Commit: in.Commit,
		Description: in.Description, Session: session(r), IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		return nil, err
	}
	return created{status: http.StatusCreated, pending: out.Pending, body: out.Value}, nil
}

func (s *server) updateApplyRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	var in struct {
		Description *string `json:"description"`
	}
	if err := decodeBody(nil, r, &in); err != nil {
		return nil, err
	}
	if in.Description == nil {
		return nil, &backend.Error{Code: backend.CodeValidationFailed, Detail: "description がない"}
	}
	return s.b.UpdateApplyRequest(ctx, c, r.PathValue("repository"), r.PathValue("applyRequestId"), session(r), *in.Description)
}

func (s *server) reviewApplyRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	var in struct {
		Kind           string `json:"kind"`
		Commit         string `json:"commit"`
		Body           string `json:"body"`
		PromptRevision string `json:"prompt_revision"`
	}
	if err := decodeBody(nil, r, &in); err != nil {
		return nil, err
	}
	out, err := s.b.ReviewApplyRequest(ctx, c, r.PathValue("repository"), r.PathValue("applyRequestId"), backend.NewReview{Kind: in.Kind,
		Commit: in.Commit, Body: in.Body, PromptRevision: in.PromptRevision, Session: session(r)})
	if err != nil {
		return nil, err
	}
	return created{status: http.StatusCreated, pending: out.Pending, body: out.Value}, nil
}

func (s *server) closeApplyRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decodeBody(nil, r, &in); err != nil {
		return nil, err
	}
	return s.b.CloseApplyRequest(ctx, c, r.PathValue("repository"), r.PathValue("applyRequestId"), session(r), in.Reason)
}

func (s *server) createHumanRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	var in struct {
		NodeID string `json:"node_id"`
		Kind   string `json:"kind"`
		Body   string `json:"body"`
	}
	if err := decodeBody(nil, r, &in); err != nil {
		return nil, err
	}
	hr, err := s.b.CreateHumanRequest(ctx, c, r.PathValue("repository"), backend.NewHumanRequest{NodeID: in.NodeID, Kind: in.Kind, Body: in.Body,
		Session: session(r), IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		return nil, err
	}
	return created{status: http.StatusCreated, body: hr}, nil
}

func (s *server) answerHumanRequest(ctx context.Context, c backend.Caller, r *http.Request) (any, error) {
	var in struct {
		Body        string `json:"body"`
		Attachments []struct {
			Kind          string `json:"kind"`
			Name          string `json:"name"`
			MediaType     string `json:"media_type"`
			ContentBase64 string `json:"content_base64"`
			URI           string `json:"uri"`
			SHA256        string `json:"sha256"`
		} `json:"attachments"`
	}
	if err := decodeBody(nil, r, &in); err != nil {
		return nil, err
	}
	var files []backend.AnswerFile
	for _, a := range in.Attachments {
		files = append(files, backend.AnswerFile{Kind: a.Kind, Name: a.Name, ContentBase64: a.ContentBase64, URI: a.URI})
	}
	hr, err := s.b.AnswerHumanRequest(ctx, c, r.PathValue("repository"), r.PathValue("humanRequestId"), in.Body, files)
	if err != nil {
		return nil, err
	}
	return created{status: http.StatusCreated, body: hr}, nil
}

// healthz は、プロセスが要求を受けられることだけを返す。
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}
