package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yamambainc/logiforge/internal/backend"
)

// SecretState は、秘密を置いたかと、最後に変えた利用者と時刻である。値は返さない。
type SecretState struct {
	Set       bool    `json:"set"`
	UpdatedBy *string `json:"updated_by"`
	UpdatedAt *string `json:"updated_at"`
}

// GitHubSettings は、Settings の github である。
type GitHubSettings struct {
	AppID        int64       `json:"app_id,omitempty"`
	ClientID     string      `json:"client_id,omitempty"`
	Slug         string      `json:"slug,omitempty"`
	APIURL       string      `json:"api_url,omitempty"`
	WebURL       string      `json:"web_url,omitempty"`
	PrivateKey   SecretState `json:"private_key"`
	ClientSecret SecretState `json:"client_secret"`
}

// ForgejoSettings は、Settings の forgejo である。
type ForgejoSettings struct {
	URL          string      `json:"url,omitempty"`
	PublicURL    string      `json:"public_url,omitempty"`
	Organization string      `json:"organization,omitempty"`
	ClientID     string      `json:"client_id,omitempty"`
	ClientSecret SecretState `json:"client_secret"`
	WorkerUser   string      `json:"worker_user,omitempty"`
	WorkerToken  SecretState `json:"worker_token"`
}

// IntervalSettings は、時間の長さの設定（秒）である。0 は既定。
type IntervalSettings struct {
	PatrolSeconds     float64 `json:"patrol_seconds,omitempty"`
	ListSeconds       float64 `json:"list_seconds,omitempty"`
	GitTimeoutSeconds float64 `json:"git_timeout_seconds,omitempty"`
	WriteWaitSeconds  float64 `json:"write_wait_seconds,omitempty"`
}

// Settings は、getSettings の応答である（openapi.yaml の Settings）。
type Settings struct {
	GitForge  string           `json:"gitforge"`
	WebUIURL  string           `json:"webui_url"`
	GitHub    *GitHubSettings  `json:"github"`
	Forgejo   *ForgejoSettings `json:"forgejo"`
	Intervals IntervalSettings `json:"intervals"`
	UpdatedBy string           `json:"updated_by"`
	UpdatedAt string           `json:"updated_at"`
}

// GitHubSettingsInput は、updateGitForgeSettings の github である。送った項目だけを変える。
type GitHubSettingsInput struct {
	AppID        *int64  `json:"app_id"`
	ClientID     *string `json:"client_id"`
	Slug         *string `json:"slug"`
	PrivateKey   *string `json:"private_key"`
	ClientSecret *string `json:"client_secret"`
	APIURL       *string `json:"api_url"`
	WebURL       *string `json:"web_url"`
}

// ForgejoSettingsInput は、updateGitForgeSettings の forgejo である。送った項目だけを変える。
type ForgejoSettingsInput struct {
	URL          *string `json:"url"`
	PublicURL    *string `json:"public_url"`
	Organization *string `json:"organization"`
	ClientID     *string `json:"client_id"`
	ClientSecret *string `json:"client_secret"`
	WorkerUser   *string `json:"worker_user"`
	WorkerToken  *string `json:"worker_token"`
}

// IntervalSettingsInput は、updateGitForgeSettings の intervals である。送った項目だけを変える。
type IntervalSettingsInput struct {
	PatrolSeconds     *float64 `json:"patrol_seconds"`
	ListSeconds       *float64 `json:"list_seconds"`
	GitTimeoutSeconds *float64 `json:"git_timeout_seconds"`
	WriteWaitSeconds  *float64 `json:"write_wait_seconds"`
}

// GitForgeSettingsInput は、updateGitForgeSettings の本文である。
type GitForgeSettingsInput struct {
	WebUIURL  *string                `json:"webui_url"`
	GitHub    *GitHubSettingsInput   `json:"github"`
	Forgejo   *ForgejoSettingsInput  `json:"forgejo"`
	Intervals *IntervalSettingsInput `json:"intervals"`
}

// SettingsService は、設定の API の中身である（design/api/endpoints/settings.md）。cmd/logiforge が、設定の DB とアダプタで実装する。
// token は、要求の Authorization のトークンである。実装は、キャッシュを使わずに、WebUI のサインインの user かを確かめる。
type SettingsService interface {
	Get(ctx context.Context, token string) (Settings, error)
	UpdateGitForge(ctx context.Context, token string, in GitForgeSettingsInput) (Settings, error)
	ListRepositories(ctx context.Context, token string) ([]SetupRepository, error)
	RegisterRepository(ctx context.Context, token string, r SetupRepository) (SetupRepository, error)
	UnregisterRepository(ctx context.Context, token, name string) error
}

// SettingsHandler は、/v1/settings の下の API のハンドラーである。
func SettingsHandler(s SettingsService, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &setupHandler{log: log}
	tok := func(r *http.Request) string {
		t, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		return strings.TrimSpace(t)
	}
	body := func(w http.ResponseWriter, r *http.Request, v any) error {
		r.Body = http.MaxBytesReader(w, r.Body, setupBodyLimit)
		return decodeJSON(r, v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) { return s.Get(ctx, tok(r)) })
	})
	mux.HandleFunc("PATCH /v1/settings/gitforge", func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) {
			var in GitForgeSettingsInput
			if err := body(w, r, &in); err != nil {
				return nil, err
			}
			return s.UpdateGitForge(ctx, tok(r), in)
		})
	})
	mux.HandleFunc("GET /v1/settings/repositories", func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) {
			rs, err := s.ListRepositories(ctx, tok(r))
			if err != nil {
				return nil, err
			}
			if rs == nil {
				rs = []SetupRepository{}
			}
			return map[string]any{"items": rs}, nil
		})
	})
	mux.HandleFunc("PUT /v1/settings/repositories/{repository}", func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) {
			var in struct {
				GitForgeRef string `json:"gitforge_ref"`
				Directory   string `json:"directory"`
			}
			if err := body(w, r, &in); err != nil {
				return nil, err
			}
			return s.RegisterRepository(ctx, tok(r), SetupRepository{Name: r.PathValue("repository"), GitForgeRef: in.GitForgeRef, Directory: in.Directory})
		})
	})
	mux.HandleFunc("DELETE /v1/settings/repositories/{repository}", func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) {
			return nil, s.UnregisterRepository(ctx, tok(r), r.PathValue("repository"))
		})
	})
	mux.HandleFunc("/v1/settings/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, &backend.Error{Code: backend.CodeNotFound, Detail: "この設定の API はない"})
	})
	return mux
}

// writeCreated は、created を書く（reply から使う）。
func writeCreated(w http.ResponseWriter, c created) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(c.status)
	_ = json.NewEncoder(w).Encode(c.body)
}
