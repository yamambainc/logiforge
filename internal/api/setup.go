package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yamambainc/logiforge/internal/backend"
)

// SetupState は、セットアップの状態である（openapi.yaml の SetupState）。
type SetupState struct {
	Done bool `json:"done"`
	// Started は、だれかがセットアップを始めたかである。トークンを持たないブラウザは、続けられないことを示す。
	Started   bool            `json:"started"`
	Step      *string         `json:"step"`
	GitForge  *string         `json:"gitforge"`
	GitHubApp *SetupGitHubApp `json:"github_app"`
	// SetupToken は、セットアップを始めた応答にだけ入る、セットアップのトークンである。
	SetupToken string `json:"setup_token,omitempty"`
}

// SetupGitHubApp は、セットアップで作った GitHub App である。
type SetupGitHubApp struct {
	Slug       string `json:"slug"`
	InstallURL string `json:"install_url"`
}

// SetupManifestInput は、createSetupGitHubManifest の本文である。
type SetupManifestInput struct {
	Owner     string `json:"owner"`
	OwnerKind string `json:"owner_kind"`
	WebUIURL  string `json:"webui_url"`
	Public    bool   `json:"public"`
}

// SetupManifest は、createSetupGitHubManifest の応答である。
type SetupManifest struct {
	Manifest map[string]any `json:"manifest"`
	PostURL  string         `json:"post_url"`
}

// SetupRepository は、setSetupRepository の本文である（RegisteredRepository）。
type SetupRepository struct {
	Name        string `json:"name"`
	GitForgeRef string `json:"gitforge_ref"`
	Directory   string `json:"directory,omitempty"`
}

// SetupForgejo は、setSetupForgejo の本文である（ForgejoSettingsInput）。
type SetupForgejo struct {
	URL          string `json:"url"`
	PublicURL    string `json:"public_url,omitempty"`
	Organization string `json:"organization"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	WorkerUser   string `json:"worker_user"`
	WorkerToken  string `json:"worker_token"`
}

// Setup は、初回のセットアップの中身である（design/settings/setup.md）。cmd/logiforge が、設定の DB とアダプタで実装する。
// 誤りは *backend.Error で返す。
type Setup interface {
	State(ctx context.Context) (SetupState, error)
	// Authorize は、token が、セットアップを始めたブラウザのトークンかを確かめる。終わっていれば setup_done、
	// まだ始まっていなければ validation_failed、違えば setup_in_progress。
	Authorize(ctx context.Context, token string) error
	// Start は、構成（kind）と WebUI の URL を確かめてから、セットアップを始めてそれらを置き、トークンを入れた状態を返す。
	// 確かめに通らなければ、始めない。もう始まっていれば setup_in_progress、終わっていれば setup_done。
	Start(ctx context.Context, kind, webuiURL string) (SetupState, error)
	// SetGitForge は、構成を決める。webuiURL（WebUI の URL。空なら変えない）も置く。サインインの redirect_uri の元になる。
	SetGitForge(ctx context.Context, kind, webuiURL string) (SetupState, error)
	GitHubManifest(ctx context.Context, in SetupManifestInput) (SetupManifest, error)
	GitHubConversion(ctx context.Context, code string) (SetupGitHubApp, error)
	SetRepository(ctx context.Context, r SetupRepository) (SetupState, error)
	SetForgejo(ctx context.Context, in SetupForgejo) (SetupState, error)
	// Finish は、token（WebUI のサインインで得たトークン）の主体を最初の利用者にし、セットアップを終える。
	Finish(ctx context.Context, token string) (SetupState, error)
}

// SetupHandler は、/v1/setup の下の API のハンドラーである（design/api/endpoints/setup.md）。
// だれでも、最初の段から始められる。始めたブラウザが受け取ったトークン（Logiforge-Setup-Token）を持つ要求だけが、続けられる。
// 最後の段でサインインした人が、最初の利用者になる。セットアップが終わったら、getSetup のほかは setup_done を返す。
func SetupHandler(s Setup, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &setupHandler{s: s, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/setup", func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) { return s.State(ctx) })
	})
	// 構成を決める呼び出しが、セットアップを始める。トークンを持たずに呼べば、始めてトークンを返す。
	mux.HandleFunc("PUT /v1/setup/gitforge", func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) {
			r.Body = http.MaxBytesReader(w, r.Body, setupBodyLimit)
			var in struct {
				GitForge string `json:"gitforge"`
				WebUIURL string `json:"webui_url"`
			}
			if err := decodeJSON(r, &in); err != nil {
				return nil, err
			}
			tok := r.Header.Get(SetupTokenHeader)
			if tok == "" {
				return s.Start(ctx, in.GitForge, in.WebUIURL)
			}
			if err := s.Authorize(ctx, tok); err != nil {
				return nil, err
			}
			return s.SetGitForge(ctx, in.GitForge, in.WebUIURL)
		})
	})
	mux.HandleFunc("POST /v1/setup/github/manifest", h.open(func(ctx context.Context, r *http.Request) (any, error) {
		var in SetupManifestInput
		if err := decodeJSON(r, &in); err != nil {
			return nil, err
		}
		return s.GitHubManifest(ctx, in)
	}))
	mux.HandleFunc("POST /v1/setup/github/conversion", h.open(func(ctx context.Context, r *http.Request) (any, error) {
		var in struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(r, &in); err != nil {
			return nil, err
		}
		return s.GitHubConversion(ctx, in.Code)
	}))
	mux.HandleFunc("PUT /v1/setup/repository", h.open(func(ctx context.Context, r *http.Request) (any, error) {
		var in SetupRepository
		if err := decodeJSON(r, &in); err != nil {
			return nil, err
		}
		return s.SetRepository(ctx, in)
	}))
	mux.HandleFunc("PUT /v1/setup/forgejo", h.open(func(ctx context.Context, r *http.Request) (any, error) {
		var in SetupForgejo
		if err := decodeJSON(r, &in); err != nil {
			return nil, err
		}
		return s.SetForgejo(ctx, in)
	}))
	mux.HandleFunc("POST /v1/setup/finish", h.open(func(ctx context.Context, r *http.Request) (any, error) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			return nil, &backend.Error{Code: backend.CodeUnauthenticated, Detail: "WebUI のサインインで得たトークンを Authorization に付ける"}
		}
		return s.Finish(ctx, strings.TrimSpace(token))
	}))
	return mux
}

type setupHandler struct {
	s   Setup
	log *slog.Logger
}

// SetupTokenHeader は、セットアップのトークンを送るヘッダーである。
const SetupTokenHeader = "Logiforge-Setup-Token"

// open は、要求が、セットアップを始めたブラウザのものかを確かめてから f を呼ぶ。
func (h *setupHandler) open(f func(ctx context.Context, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.reply(w, r, func(ctx context.Context) (any, error) {
			if err := h.s.Authorize(ctx, r.Header.Get(SetupTokenHeader)); err != nil {
				return nil, err
			}
			r.Body = http.MaxBytesReader(w, r.Body, setupBodyLimit)
			return f(ctx, r)
		})
	}
}

// setupBodyLimit は、セットアップの要求の本文の上限である。
const setupBodyLimit = 64 << 10

func (h *setupHandler) reply(w http.ResponseWriter, r *http.Request, f func(ctx context.Context) (any, error)) {
	w.Header().Set("Cache-Control", "no-store")
	out, err := f(r.Context())
	if err != nil {
		e := backend.AsError(err)
		if e == nil {
			h.log.Error("セットアップの内部の誤り", "method", r.Method, "path", r.URL.Path, "err", err)
			e = &backend.Error{Code: backend.CodeInternal, Detail: "内部の誤り"}
		}
		writeProblem(w, e)
		return
	}
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if c, ok := out.(created); ok {
		writeCreated(w, c)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &backend.Error{Code: backend.CodeValidationFailed, Detail: "本文の JSON が正しくない：" + err.Error()}
	}
	return nil
}
