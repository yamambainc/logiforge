package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/yamambainc/logiforge/internal/ports"
)

// ManifestInput は、GitHub App の manifest を作るための入力である（design/settings/setup.md の「GitHub を使う構成」）。
type ManifestInput struct {
	Owner string
	// Organization は、Owner が organization か（false ならアカウント）。
	Organization bool
	// WebUIURL は、WebUI の URL である。redirect_url と callback_urls の元になる。
	WebUIURL string
	// Public は、ほかの owner にも install できる App にするか。
	Public bool
	// WebURL は、GitHub の画面の URL である。空なら DefaultWebURL。
	WebURL string
}

// Manifest は、GitHub App の manifest と、manifest をフォームで送る先の URL を返す。
// 権限は design/adapters/github-app.md の「GitHub App」の表のとおりで、Webhook は使わない。
func Manifest(in ManifestInput) (manifest map[string]any, postURL string, err error) {
	if in.Owner == "" || strings.ContainsAny(in.Owner, "/?#") {
		return nil, "", fmt.Errorf("github: App を作る owner の名前が正しくない：%q", in.Owner)
	}
	u, err := url.Parse(in.WebUIURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, "", fmt.Errorf("github: WebUI の URL は http か https の URL にする：%q", in.WebUIURL)
	}
	webui := strings.TrimRight(in.WebUIURL, "/")
	web := strings.TrimRight(in.WebURL, "/")
	if web == "" {
		web = DefaultWebURL
	}
	manifest = map[string]any{
		"name":            "Logiforge " + u.Host,
		"url":             webui,
		"redirect_url":    webui + "/setup/github/callback",
		"callback_urls":   []string{webui + "/oauth/callback"},
		"public":          in.Public,
		"hook_attributes": map[string]any{"url": webui + "/", "active": false},
		"default_events":  []string{},
		"default_permissions": map[string]string{
			"contents":      "write",
			"issues":        "write",
			"pull_requests": "write",
			"metadata":      "read",
		},
	}
	postURL = web + "/settings/apps/new"
	if in.Organization {
		postURL = web + "/organizations/" + url.PathEscape(in.Owner) + "/settings/apps/new"
	}
	return manifest, postURL, nil
}

// ManifestApp は、manifest flow で作った GitHub App である。
type ManifestApp struct {
	ID            string
	Slug          string
	ClientID      string
	ClientSecret  string
	PrivateKeyPEM string
	// HTMLURL は、App の画面の URL である。
	HTMLURL string
}

// ErrManifestCode は、manifest flow の code が、切れているか正しくないことである。
var ErrManifestCode = errors.New("github: manifest flow の code が切れているか、正しくない。App の作成からやり直す")

// ConvertManifest は、manifest flow の code を、App の ID と秘密に換える（POST /app-manifests/{code}/conversions）。
// この呼び出しは認証を求めない。code は 1 時間で切れ、1 回だけ使える。
func ConvertManifest(ctx context.Context, apiURL string, h *http.Client, code string) (ManifestApp, error) {
	if code == "" || strings.ContainsAny(code, "/?#") {
		return ManifestApp{}, ErrManifestCode
	}
	var r struct {
		ID           int64  `json:"id"`
		Slug         string `json:"slug"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		PEM          string `json:"pem"`
		HTMLURL      string `json:"html_url"`
	}
	err := newClient(apiURL, h).do(ctx, "", http.MethodPost, "/app-manifests/"+url.PathEscape(code)+"/conversions", nil, &r)
	if s := statusOf(err); s == http.StatusNotFound || s == http.StatusUnprocessableEntity {
		return ManifestApp{}, ErrManifestCode
	}
	if err != nil {
		return ManifestApp{}, err
	}
	if r.ID == 0 || r.Slug == "" || r.ClientID == "" || r.ClientSecret == "" || r.PEM == "" {
		return ManifestApp{}, errors.New("github: manifest flow の応答に、App の ID、slug、client_id、client secret、秘密鍵がそろっていない")
	}
	return ManifestApp{ID: strconv.FormatInt(r.ID, 10), Slug: r.Slug, ClientID: r.ClientID, ClientSecret: r.ClientSecret, PrivateKeyPEM: r.PEM, HTMLURL: r.HTMLURL}, nil
}

// InstallURL は、App を owner に install する GitHub の画面の URL である。
func InstallURL(webURL, slug string) string {
	web := strings.TrimRight(webURL, "/")
	if web == "" {
		web = DefaultWebURL
	}
	return web + "/apps/" + url.PathEscape(slug) + "/installations/new"
}

// CanReadRepository は、App の installation token で、gitForgeRef の GitHub リポジトリを読めるかを確かめる。
// App が owner に install されていないか、そのリポジトリを選んでいなければ、誤りを返す。
func (a *App) CanReadRepository(ctx context.Context, gitForgeRef string) error {
	tok, err := a.Token(ctx, gitForgeRef)
	if err != nil {
		return err
	}
	rp, _ := ports.SplitRef(gitForgeRef)
	return a.c.do(ctx, tok, http.MethodGet, "/repos/"+rp, nil, nil)
}
