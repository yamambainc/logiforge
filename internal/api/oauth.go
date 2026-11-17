package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/backend"
)

// OAuth は、WebUI のサインインの中継の設定である（design/webui/oauth-relay.md）。
// BE は、Forgejo の OAuth2 の認可コードを、client secret を足してアクセストークンに交換する。状態は持たない。
type OAuth struct {
	ForgejoURL string // BE から Forgejo への URL。トークンのエンドポイントは、ここから作る
	// PublicURL は、ブラウザから Forgejo への URL。authorize の URL は、ここから作る。空なら ForgejoURL を使う。
	PublicURL    string
	ClientID     string
	ClientSecret string
	RedirectURI  string // Forgejo に登録した redirect_uri。完全に一致させる
	// Client は、Forgejo を呼ぶ HTTP のクライアントである。nil なら、10 秒で切るクライアントを使う。
	Client *http.Client
}

// OAuthScope は、WebUI が求めるスコープである（design/webui/auth.md の「スコープ」）。BE は、主体を確かめるほか、
// 利用者のトークンで repository を作り、GitForge を読み、回答を書く。Logiforge は、書くための自分のアカウントを持たないからである。
const OAuthScope = "read:user read:organization write:organization write:repository write:issue"

// oauthBodyLimit は、交換の要求の本文の上限である（開いたままの代理にしないため）。
const oauthBodyLimit = 4 << 10

func (o *OAuth) base() string { return strings.TrimRight(o.ForgejoURL, "/") }

func (o *OAuth) public() string {
	if o.PublicURL != "" {
		return strings.TrimRight(o.PublicURL, "/")
	}
	return o.base()
}

// config は、GET /v1/oauth/config である。authorize の URL など、WebUI がサインインを始めるのに要るものを返す。
func (o *OAuth) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"authorize_url": o.public() + "/login/oauth/authorize",
		"client_id":     o.ClientID,
		"redirect_uri":  o.RedirectURI,
		"scope":         OAuthScope,
	})
}

// token は、POST /v1/oauth/token である。code と code_verifier だけを受け取り、ほかは設定から付けて Forgejo で交換する。
// 返すのは、アクセストークンと寿命だけである。リフレッシュトークンは返さない（design/webui/auth.md の「トークンの持ち方」）。
func (o *OAuth) token(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeProblem(w, &backend.Error{Code: backend.CodeValidationFailed, Detail: "Content-Type は application/json"})
		return
	}
	var in struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, oauthBodyLimit+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.Code == "" || in.CodeVerifier == "" {
		writeProblem(w, &backend.Error{Code: backend.CodeValidationFailed, Detail: "code と code_verifier を JSON で送る（4 KiB まで）"})
		return
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {o.ClientID},
		"client_secret": {o.ClientSecret},
		"code":          {in.Code},
		"code_verifier": {in.CodeVerifier},
		"redirect_uri":  {o.RedirectURI},
	}
	out, err := o.exchange(r.Context(), form)
	if err != nil {
		// code やトークンは、ログにもエラーにも書かない。
		if errors.Is(err, errExchange) {
			writeProblem(w, &backend.Error{Code: backend.CodeUnauthenticated, Detail: "サインインのコードを交換できなかった"})
			return
		}
		// Forgejo につながらない、または 5xx。少し待って送り直す
		writeProblem(w, &backend.Error{Code: backend.CodeUnavailable, Detail: "Forgejo につながらない"})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type oauthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

var (
	errExchange    = errors.New("交換を拒まれた")
	errUnavailable = errors.New("forgejo が 5xx を返した")
)

func (o *OAuth) exchange(ctx context.Context, form url.Values) (oauthToken, error) {
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.base()+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return oauthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return oauthToken{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 500 {
		return oauthToken{}, errUnavailable
	}
	if res.StatusCode != http.StatusOK {
		return oauthToken{}, errExchange
	}
	var t oauthToken
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&t); err != nil || t.AccessToken == "" {
		return oauthToken{}, errExchange
	}
	return t, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
