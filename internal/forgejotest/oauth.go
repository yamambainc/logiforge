// Package forgejotest は、動く Forgejo を使うテスト（結合テストと E2E）のための手伝いである。テストからだけ使う。
package forgejotest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

// App は、OAuth2 のアプリケーションである。
type App struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// CreateApp は、admin のトークンで、confidential client の OAuth2 のアプリケーションを作る。
func CreateApp(base, adminToken, name, redirect string) (App, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "redirect_uris": []string{redirect}, "confidential_client": true})
	req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/api/v1/user/applications/oauth2", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "token "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return App{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		return App{}, fmt.Errorf("OAuth2 のアプリケーションを作れない：%d %s", res.StatusCode, b)
	}
	var a App
	return a, json.NewDecoder(res.Body).Decode(&a)
}

var hiddenField = regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`)

// Token は、利用者がブラウザでサインインするのと同じ流れ（ログイン、authorize、同意、認可コードと PKCE の交換）で、
// アクセストークンを得る。WebUI のサインインで得るトークンと同じものである。
func Token(base, login, password string, app App, redirect string) (string, error) {
	base = strings.TrimRight(base, "/")
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if res, err := c.Get(base + "/user/login"); err == nil {
		res.Body.Close()
	} else {
		return "", err
	}
	res, err := c.PostForm(base+"/user/login", url.Values{"user_name": {login}, "password": {password}})
	if err != nil {
		return "", err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusFound {
		return "", fmt.Errorf("forgejo にログインできない：%d", res.StatusCode)
	}
	verifier := randomString()
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {app.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	res, err = c.Get(base + "/login/oauth/authorize?" + q.Encode())
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode == http.StatusOK {
		// 初めてなら、同意の画面が出る。
		f := url.Values{}
		for _, m := range hiddenField.FindAllStringSubmatch(string(body), -1) {
			f.Set(m[1], m[2])
		}
		f.Set("granted", "true")
		if res, err = c.PostForm(base+"/login/oauth/grant", f); err != nil {
			return "", err
		}
		res.Body.Close()
	}
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil || loc.Query().Get("code") == "" {
		return "", fmt.Errorf("authorize が code を返さない：%d %s", res.StatusCode, res.Header.Get("Location"))
	}
	res, err = http.PostForm(base+"/login/oauth/access_token", url.Values{"grant_type": {"authorization_code"}, "client_id": {app.ClientID},
		"client_secret": {app.ClientSecret}, "code": {loc.Query().Get("code")}, "redirect_uri": {redirect}, "code_verifier": {verifier}})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("トークンを交換できない：%d %v", res.StatusCode, err)
	}
	return out.AccessToken, nil
}

func randomString() string {
	b := make([]byte, 48)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
