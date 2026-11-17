package api

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeForgejo は、Forgejo のトークンのエンドポイントの代わりである。受け取ったフォームを覚える。
func fakeForgejo(t *testing.T, status int) (*httptest.Server, *url.Values) {
	t.Helper()
	got := &url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/access_token" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		*got = r.PostForm
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"access_token":"at-1","token_type":"bearer","expires_in":3600,"refresh_token":"rt-secret"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func serverWith(o *OAuth, ui fstest.MapFS) http.Handler {
	opt := Options{OAuth: o}
	if ui != nil {
		opt.WebUI = ui
	}
	return NewServer(nil, nil, opt)
}

func do(h http.Handler, method, target, ct, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOAuthConfig(t *testing.T) {
	h := serverWith(&OAuth{ForgejoURL: "https://forge.example/", ClientID: "cid", ClientSecret: "sec", RedirectURI: "https://lf.example/oauth/callback"}, nil)
	rec := do(h, "GET", "/v1/oauth/config", "", "")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["authorize_url"] != "https://forge.example/login/oauth/authorize" || got["client_id"] != "cid" || got["scope"] != OAuthScope {
		t.Fatalf("config: %v", got)
	}
	if strings.Contains(rec.Body.String(), "sec") {
		t.Fatal("client secret を返してはいけない")
	}
}

func TestOAuthNotConfigured(t *testing.T) {
	rec := do(serverWith(nil, nil), "GET", "/v1/oauth/config", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("設定がなければ not_found：%d", rec.Code)
	}
}

func TestOAuthTokenRelay(t *testing.T) {
	fj, got := fakeForgejo(t, 200)
	h := serverWith(&OAuth{ForgejoURL: fj.URL, ClientID: "cid", ClientSecret: "sec", RedirectURI: "https://lf.example/oauth/callback"}, nil)
	rec := do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c1","code_verifier":"v1"}`)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	// grant_type、client_id、client secret、redirect_uri は、BE の設定から付ける
	for k, want := range map[string]string{"grant_type": "authorization_code", "client_id": "cid", "client_secret": "sec", "code": "c1", "code_verifier": "v1", "redirect_uri": "https://lf.example/oauth/callback"} {
		if got.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, got.Get(k), want)
		}
	}
	// リフレッシュトークンは返さない
	if strings.Contains(rec.Body.String(), "rt-secret") || strings.Contains(rec.Body.String(), "refresh") {
		t.Fatalf("refresh_token を返してはいけない：%s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"access_token":"at-1"`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("応答：%s %v", rec.Body, rec.Header())
	}
}

func TestOAuthPublicURL(t *testing.T) {
	h := serverWith(&OAuth{ForgejoURL: "http://forgejo:3000", PublicURL: "https://forge.example", ClientID: "c", ClientSecret: "s", RedirectURI: "r"}, nil)
	rec := do(h, "GET", "/v1/oauth/config", "", "")
	if !strings.Contains(rec.Body.String(), `"authorize_url":"https://forge.example/login/oauth/authorize"`) {
		t.Fatalf("authorize の URL はブラウザ向けの URL から作る：%s", rec.Body)
	}
}

func TestOAuthTokenUnavailable(t *testing.T) {
	fj, _ := fakeForgejo(t, 502)
	h := serverWith(&OAuth{ForgejoURL: fj.URL, ClientID: "c", ClientSecret: "s", RedirectURI: "r"}, nil)
	if rec := do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`); rec.Code != 503 {
		t.Fatalf("Forgejo の 5xx は 503：%d", rec.Code)
	}
	fj.Close()
	if rec := do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`); rec.Code != 503 {
		t.Fatalf("Forgejo につながらなければ 503：%d", rec.Code)
	}
}

func TestOAuthTokenRejects(t *testing.T) {
	fj, got := fakeForgejo(t, 400)
	h := serverWith(&OAuth{ForgejoURL: fj.URL, ClientID: "cid", ClientSecret: "sec", RedirectURI: "r"}, nil)
	cases := []struct {
		name, ct, body string
		status         int
	}{
		{"Forgejo が拒んだ", "application/json", `{"code":"c","code_verifier":"v"}`, 401},
		{"JSON でない", "application/x-www-form-urlencoded", `code=c&code_verifier=v`, 422},
		{"code がない", "application/json", `{"code_verifier":"v"}`, 422},
		{"知らない項目（送り先や grant_type を受け取らない）", "application/json", `{"code":"c","code_verifier":"v","grant_type":"refresh_token"}`, 422},
		{"4 KiB を超える", "application/json", `{"code":"` + strings.Repeat("x", 5000) + `","code_verifier":"v"}`, 422},
	}
	for _, c := range cases {
		*got = url.Values{}
		rec := do(h, "POST", "/v1/oauth/token", c.ct, c.body)
		if rec.Code != c.status {
			t.Errorf("%s：status %d, want %d", c.name, rec.Code, c.status)
		}
		if c.status == 422 && len(*got) != 0 {
			t.Errorf("%s：Forgejo を呼んではいけない", c.name)
		}
	}
}

func TestWebUIServing(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html><title>x</title>")},
		"assets/app-1.js": {Data: []byte(strings.Repeat("console.log(1);", 100))},
		"favicon.svg":     {Data: []byte("<svg/>")},
		"mermaid.html":    {Data: []byte("<!doctype html><title>m</title>")},
		".gitkeep":        {Data: nil},
	}
	h := serverWith(nil, ui)
	// 画面のパスは index.html
	rec := do(h, "GET", "/r/q/tree?node=h-1", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>x</title>") || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index: %d %v", rec.Code, rec.Header())
	}
	if rec.Header().Get("Content-Security-Policy") != WebUICSP || rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("WebUI のヘッダー：%v", rec.Header())
	}
	// ハッシュ付きの assets は immutable
	rec = do(h, "GET", "/assets/app-1.js", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("assets: %d %v", rec.Code, rec.Header())
	}
	// sandbox の iframe（オパークなオリジン）から読めるように、assets は CORS を許す
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("assets の CORS: %v", rec.Header())
	}
	// Mermaid を描く文書だけ、インラインの style を許す CSP。ほかの画面は、今までの CSP
	rec = do(h, "GET", "/mermaid.html", "", "")
	if rec.Code != 200 || rec.Header().Get("Content-Security-Policy") != MermaidCSP || !strings.Contains(MermaidCSP, "'unsafe-inline'") {
		t.Fatalf("mermaid.html: %d %v", rec.Code, rec.Header())
	}
	// 直接開かれても、オパークなオリジンで動く（allow-same-origin を付けない sandbox）
	if !strings.HasSuffix(MermaidCSP, "; sandbox allow-scripts") || strings.Contains(MermaidCSP, "allow-same-origin") {
		t.Fatalf("mermaid.html の sandbox：%s", MermaidCSP)
	}
	if strings.Contains(WebUICSP, "unsafe-inline") {
		t.Fatal("アプリの CSP に unsafe-inline がある")
	}
	// assets にないファイルは 404（index.html に置き換えない）
	if rec := do(h, "GET", "/assets/old-0.js", "", ""); rec.Code != 404 {
		t.Fatalf("古い assets: %d", rec.Code)
	}
	// ドットで始まるファイルは配らない
	if rec := do(h, "GET", "/.gitkeep", "", ""); !strings.Contains(rec.Body.String(), "<title>x</title>") {
		t.Fatal(".gitkeep を配ってはいけない")
	}
	// API の 404 は、WebUI に置き換えない
	if rec := do(h, "GET", "/v1/nothing", "", ""); rec.Code != 404 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("API の 404: %d %v", rec.Code, rec.Header())
	}
}

func TestGzip(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>" + strings.Repeat("<p>x</p>", 200))}}
	h := serverWith(nil, ui)
	rec := do(h, "GET", "/", "", "", "Accept-Encoding", "gzip, br")
	if rec.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("gzip で返していない：%v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(zr)
	if !strings.Contains(string(b), "<p>x</p>") {
		t.Fatal("中身が違う")
	}
	// gzip を受け付けない要求には、そのまま返す
	if rec := do(h, "GET", "/", "", ""); rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzip を受け付けない要求に gzip で返した")
	}
	// API の JSON（ここでは 404 の problem+json）も gzip にする
	if rec := do(h, "GET", "/v1/nothing", "", "", "Accept-Encoding", "gzip"); rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("API の JSON を gzip にしていない：%v", rec.Header())
	}
}

func TestGzipSkipsNoBody(t *testing.T) {
	h := withGzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotModified)
	}))
	rec := do(h, "GET", "/", "", "", "Accept-Encoding", "gzip")
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.Len() != 0 {
		t.Fatalf("304 に gzip を付けた：%v %d", rec.Header(), rec.Body.Len())
	}
}

func TestAttachmentHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	(&server{}).writeRaw(rec, raw{contentType: "application/octet-stream", body: []byte("<svg onload=alert(1)>"), attachment: true})
	if rec.Header().Get("Content-Disposition") != "attachment" || rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("artifact と添付のヘッダー：%v", rec.Header())
	}
}
