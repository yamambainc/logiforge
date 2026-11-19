package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memSessions は、セッションの置き場の代わりである。
type memSessions struct {
	mu sync.Mutex
	m  map[string]Session
}

func (s *memSessions) PutSession(_ context.Context, x Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[x.IDHash] = x
	return nil
}

func (s *memSessions) SessionOf(_ context.Context, h string) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.m[h]
	return x, ok, nil
}

func (s *memSessions) DeleteSession(_ context.Context, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, h)
	return nil
}

// fakeRefresh は、GitForge のトークンのエンドポイントの代わりである。
// refresh token は 1 回しか使えない（GitHub と Forgejo と同じ）。使うたびに、新しいアクセストークンと refresh token を返す。
type fakeRefresh struct {
	mu       sync.Mutex
	valid    map[string]bool
	n        int
	refreshs atomic.Int32
	expires  int
}

func newFakeRefresh(t *testing.T) (*fakeRefresh, string) {
	f := &fakeRefresh{valid: map[string]bool{}, expires: 3600}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.PostForm.Get("client_secret") != "sec" {
			w.WriteHeader(400)
			return
		}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
		case "refresh_token":
			f.refreshs.Add(1)
			rt := r.PostForm.Get("refresh_token")
			if !f.valid[rt] {
				// GitHub は 200 で error を返す
				_, _ = io.WriteString(w, `{"error":"bad_refresh_token"}`)
				return
			}
			delete(f.valid, rt)
		default:
			w.WriteHeader(400)
			return
		}
		f.n++
		rt := fmt.Sprintf("rt-%d", f.n)
		f.valid[rt] = true
		_, _ = fmt.Fprintf(w, `{"access_token":"at-%d","token_type":"bearer","expires_in":%d,"refresh_token":%q}`, f.n, f.expires, rt)
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

const testOrigin = "https://lf.example"

func sessionServer(t *testing.T) (*fakeRefresh, *memSessions, http.Handler) {
	f, u := newFakeRefresh(t)
	st := &memSessions{m: map[string]Session{}}
	h := serverWith(&OAuth{ForgejoURL: u, ClientID: "cid", ClientSecret: "sec", RedirectURI: testOrigin + "/oauth/callback", Sessions: st}, nil)
	return f, st, h
}

func cookieOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	return nil
}

func postSession(h http.Handler, path, cookie string, hdr ...string) *httptest.ResponseRecorder {
	all := append([]string{"Origin", testOrigin}, hdr...)
	if cookie != "" {
		all = append(all, "Cookie", SessionCookie+"="+cookie)
	}
	return do(h, "POST", path, "application/json", "{}", all...)
}

func accessOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var v struct {
		AccessToken string `json:"access_token"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "rt-") {
		t.Fatalf("refresh token を返してはいけない：%s", rec.Body)
	}
	return v.AccessToken
}

// 交換でセッションを作り、cookie（HttpOnly、Secure、SameSite=Strict）を付ける。refresh token は、DB にだけ置き、ID はハッシュで置く。
func TestSessionStartAndReuse(t *testing.T) {
	f, st, h := sessionServer(t)
	rec := do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`)
	if accessOf(t, rec) != "at-1" {
		t.Fatal(rec.Body)
	}
	c := cookieOf(t, rec)
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Domain != "" || c.MaxAge <= 0 {
		t.Fatalf("cookie：%+v", c)
	}
	if len(st.m) != 1 {
		t.Fatalf("セッション：%v", st.m)
	}
	for k, s := range st.m {
		if k == c.Value || s.RefreshToken != "rt-1" || s.ClientID != "cid" {
			t.Fatalf("置いたセッション：%q %+v", k, s)
		}
	}
	// 残りが長ければ、refresh せずに、同じアクセストークンを返す（タブがいくつあっても 1 つ）
	for range 3 {
		if got := accessOf(t, postSession(h, "/v1/oauth/session", c.Value)); got != "at-1" {
			t.Fatalf("置いたものを返さない：%s", got)
		}
	}
	if f.refreshs.Load() != 0 {
		t.Fatalf("refresh した：%d", f.refreshs.Load())
	}
}

// 期限が近ければ refresh し、回転した refresh token を置く。同時に来ても、refresh は 1 回である。
func TestSessionRefreshRotates(t *testing.T) {
	f, st, h := sessionServer(t)
	c := cookieOf(t, do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`))
	for k, s := range st.m {
		s.AccessExpires = time.Now().Add(time.Minute)
		st.m[k] = s
	}
	var wg sync.WaitGroup
	got := make([]string, 5)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := postSession(h, "/v1/oauth/session", c.Value)
			if rec.Code == 200 {
				got[i] = accessOf(t, rec)
			}
		}()
	}
	wg.Wait()
	for _, g := range got {
		if g != "at-2" {
			t.Fatalf("同時の要求：%v", got)
		}
	}
	if f.refreshs.Load() != 1 {
		t.Fatalf("refresh の回数：%d", f.refreshs.Load())
	}
	for _, s := range st.m {
		if s.RefreshToken != "rt-2" {
			t.Fatalf("回転した refresh token を置いていない：%+v", s)
		}
	}
}

// GitForge が refresh を断ったら（取り消し、期限切れ、使い済み）、セッションと cookie を消して 401。
func TestSessionRefreshRejected(t *testing.T) {
	f, st, h := sessionServer(t)
	c := cookieOf(t, do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`))
	for k, s := range st.m {
		s.AccessExpires = time.Now()
		st.m[k] = s
	}
	f.mu.Lock()
	f.valid = map[string]bool{}
	f.mu.Unlock()
	rec := postSession(h, "/v1/oauth/session", c.Value)
	if rec.Code != 401 || len(st.m) != 0 {
		t.Fatalf("断られたら 401 でセッションを消す：%d %v", rec.Code, st.m)
	}
	if k := cookieOf(t, rec); k == nil || k.MaxAge >= 0 {
		t.Fatalf("cookie を消していない：%+v", k)
	}
	// 知らない cookie、cookie なし、client_id が変わったセッションも 401
	if rec := postSession(h, "/v1/oauth/session", "nothing"); rec.Code != 401 {
		t.Fatalf("知らない cookie：%d", rec.Code)
	}
	if rec := postSession(h, "/v1/oauth/session", ""); rec.Code != 401 {
		t.Fatalf("cookie なし：%d", rec.Code)
	}
	c2 := cookieOf(t, do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`))
	for k, s := range st.m {
		s.ClientID = "old"
		st.m[k] = s
	}
	if rec := postSession(h, "/v1/oauth/session", c2.Value); rec.Code != 401 || len(st.m) != 0 {
		t.Fatalf("client_id が変わったら使わない：%d %v", rec.Code, st.m)
	}
}

// CSRF：JSON でない要求と、ほかのオリジンからの要求は断る。
func TestSessionCSRF(t *testing.T) {
	_, st, h := sessionServer(t)
	c := cookieOf(t, do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`))
	ck := SessionCookie + "=" + c.Value
	for _, x := range []struct {
		path, ct string
		hdr      []string
	}{
		{"/v1/oauth/session", "application/x-www-form-urlencoded", []string{"Origin", testOrigin, "Cookie", ck}},
		{"/v1/oauth/session", "application/json", []string{"Origin", "https://evil.example", "Cookie", ck}},
		{"/v1/oauth/session", "application/json", []string{"Cookie", ck}},
		{"/v1/oauth/signout", "text/plain", []string{"Origin", testOrigin, "Cookie", ck}},
		{"/v1/oauth/signout", "application/json", []string{"Origin", "null", "Cookie", ck}},
	} {
		if rec := do(h, "POST", x.path, x.ct, "{}", x.hdr...); rec.Code < 400 {
			t.Errorf("%s %s %v：%d", x.path, x.ct, x.hdr, rec.Code)
		}
	}
	if len(st.m) != 1 {
		t.Fatal("断った要求で、セッションが消えた")
	}
	if rec := do(h, "GET", "/v1/oauth/session", "", "", "Cookie", ck); rec.Code < 400 {
		t.Fatalf("GET：%d", rec.Code)
	}
}

// サインアウトで、セッションと cookie を消す。そのあとの session は 401。
func TestSessionSignOut(t *testing.T) {
	_, st, h := sessionServer(t)
	c := cookieOf(t, do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`))
	rec := postSession(h, "/v1/oauth/signout", c.Value)
	if rec.Code != 204 || len(st.m) != 0 {
		t.Fatalf("サインアウト：%d %v", rec.Code, st.m)
	}
	if k := cookieOf(t, rec); k == nil || k.MaxAge >= 0 {
		t.Fatalf("cookie を消していない：%+v", k)
	}
	if rec := postSession(h, "/v1/oauth/session", c.Value); rec.Code != 401 {
		t.Fatalf("サインアウトのあと：%d", rec.Code)
	}
}

// サインインし直したら、前のセッションを消す。refresh token が来なければ（期限のないトークン）、セッションを作らない。
func TestSessionReplaceAndNoRefreshToken(t *testing.T) {
	f, st, h := sessionServer(t)
	c := cookieOf(t, do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`))
	do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`, "Cookie", SessionCookie+"="+c.Value)
	if len(st.m) != 1 {
		t.Fatalf("前のセッションを消していない：%v", st.m)
	}
	f.mu.Lock()
	f.expires = 0
	f.mu.Unlock()
	st.m = map[string]Session{}
	rec := do(h, "POST", "/v1/oauth/token", "application/json", `{"code":"c","code_verifier":"v"}`)
	if rec.Code != 200 || cookieOf(t, rec) != nil || len(st.m) != 0 {
		t.Fatalf("期限のないトークンで、セッションを作った：%d %v", rec.Code, st.m)
	}
}
