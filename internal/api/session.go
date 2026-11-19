package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yamambainc/logiforge/internal/backend"
)

// WebUI のサインインのセッション（design/webui/session.md）。
// BE は、GitForge のリフレッシュトークンを設定の DB に持ち、ブラウザには、ランダムなセッション ID を入れた cookie だけを渡す。
// WebUI は、起動したときと期限の前に POST /v1/oauth/session を呼び、アクセストークンをもらう。

// Session は、セッションの置き場に置くものである。
type Session struct {
	IDHash        string
	ClientID      string
	AccessToken   string
	AccessExpires time.Time
	RefreshToken  string
	CreatedAt     time.Time
	UsedAt        time.Time
}

// SessionStore は、セッションの置き場である。cmd/logiforge が、設定の DB で実装する。
type SessionStore interface {
	PutSession(ctx context.Context, s Session) error
	SessionOf(ctx context.Context, idHash string) (Session, bool, error)
	DeleteSession(ctx context.Context, idHash string) error
}

const (
	// SessionCookie は、セッション ID を入れる cookie の名前である。__Host- を付け、Secure、Path=/、Domain なしを強いる。
	SessionCookie = "__Host-lf_session"
	// sessionCookieAge は、cookie の寿命である。リフレッシュトークンが切れれば、それより前でも使えなくなる。
	sessionCookieAge = 180 * 24 * time.Hour
	// sessionReuse は、置いてあるアクセストークンを、refresh せずに返す残りの時間の下限である。
	// なぜ：タブがいくつあっても、refresh を期限の近くの 1 回にまとめ、回転の競合を起こさないため。
	sessionReuse = 10 * time.Minute
)

// sessionLocks は、セッションごとのロックである。同じセッションの refresh を 1 つずつにする（BE は 1 台）。
// OAuth は設定の版ごとに作り直すので、ロックはパッケージに持つ。
var sessionLocks = struct {
	sync.Mutex
	m map[string]*sessionLock
}{m: map[string]*sessionLock{}}

type sessionLock struct {
	sync.Mutex
	n int
}

func lockSession(idHash string) func() {
	sessionLocks.Lock()
	l := sessionLocks.m[idHash]
	if l == nil {
		l = &sessionLock{}
		sessionLocks.m[idHash] = l
	}
	l.n++
	sessionLocks.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		sessionLocks.Lock()
		if l.n--; l.n == 0 {
			delete(sessionLocks.m, idHash)
		}
		sessionLocks.Unlock()
	}
}

func hashSessionID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func newSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: id, Path: "/", MaxAge: int(sessionCookieAge / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
}

func sessionIDFrom(r *http.Request) string {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// webOrigin は、WebUI のオリジンである。redirect_uri（WebUI の URL の /oauth/callback）から作る。
func (o *OAuth) webOrigin() string { return normOrigin(o.RedirectURI) }

// normOrigin は、URL のオリジンを、ブラウザの Origin と同じ形にする（小文字、既定のポートを除く）。
func normOrigin(v string) string {
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	scheme, host, port := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname()), u.Port()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

// sameOriginJSON は、cookie を使う要求の CSRF の守りである（design/webui/session.md の「CSRF」）。
// Content-Type が application/json で（フォームからは送れない）、Origin が WebUI のオリジンのものだけを受け付ける。
func (o *OAuth) sameOriginJSON(w http.ResponseWriter, r *http.Request) bool {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeProblem(w, &backend.Error{Code: backend.CodeValidationFailed, Detail: "Content-Type は application/json"})
		return false
	}
	if origin := o.webOrigin(); origin == "" || normOrigin(r.Header.Get("Origin")) != origin {
		writeProblem(w, &backend.Error{Code: backend.CodeForbidden, Detail: "ほかのオリジンからは呼べない"})
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, oauthBodyLimit))
	return true
}

// startSession は、交換で得たトークンでセッションを作り、cookie を付ける。リフレッシュトークンがなければ作らない。
// 前のセッションの cookie があれば、そのセッションは消す（サインインし直したので）。
func (o *OAuth) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, t oauthToken) {
	if o.Sessions == nil || t.RefreshToken == "" || t.ExpiresIn <= 0 {
		return
	}
	if old := sessionIDFrom(r); old != "" {
		if err := o.Sessions.DeleteSession(ctx, hashSessionID(old)); err != nil {
			slog.Warn("前のセッションを消せない", "err", err)
		}
	}
	id, now := newSessionID(), time.Now()
	s := Session{IDHash: hashSessionID(id), ClientID: o.ClientID, AccessToken: t.AccessToken, AccessExpires: now.Add(time.Duration(t.ExpiresIn) * time.Second),
		RefreshToken: t.RefreshToken, CreatedAt: now, UsedAt: now}
	if err := o.Sessions.PutSession(ctx, s); err != nil {
		// セッションを置けなくても、サインインは続ける。次に開いたときに、authorize に行くだけである。
		slog.Warn("セッションを置けない", "err", err)
		return
	}
	setSessionCookie(w, id)
}

// session は、POST /v1/oauth/session である。cookie のセッションのアクセストークンを返す。
// 置いてあるものの残りが sessionReuse より短ければ、GitForge で refresh して、新しいトークンを置いてから返す。
func (o *OAuth) session(w http.ResponseWriter, r *http.Request) {
	if !o.sameOriginJSON(w, r) {
		return
	}
	id := sessionIDFrom(r)
	if o.Sessions == nil || id == "" {
		writeProblem(w, &backend.Error{Code: backend.CodeUnauthenticated, Detail: "セッションがない"})
		return
	}
	// refresh から置くまでを、切断で止めない。なぜ：GitForge で回転したリフレッシュトークンを置き損ねると、そのセッションは使えなくなるから。
	ctx, h := context.WithoutCancel(r.Context()), hashSessionID(id)
	defer lockSession(h)()
	s, ok, err := o.Sessions.SessionOf(ctx, h)
	if err != nil {
		slog.Error("セッションを読めない", "err", err)
		writeProblem(w, &backend.Error{Code: backend.CodeUnavailable, Detail: "セッションを読めない"})
		return
	}
	if !ok || s.ClientID != o.ClientID {
		o.dropSession(ctx, w, h, ok)
		return
	}
	now := time.Now()
	if s.AccessExpires.Sub(now) < sessionReuse {
		t, err := o.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {o.ClientID}, "client_secret": {o.ClientSecret},
			"refresh_token": {s.RefreshToken}})
		if err != nil {
			if err == errExchange {
				// GitForge が断った（取り消された、期限が切れた、すでに使われた）。このセッションは、もう使えない。
				o.dropSession(ctx, w, h, true)
				return
			}
			writeProblem(w, &backend.Error{Code: backend.CodeUnavailable, Detail: "GitForge につながらない"})
			return
		}
		s.AccessToken, s.AccessExpires = t.AccessToken, now.Add(time.Duration(t.ExpiresIn)*time.Second)
		if t.ExpiresIn <= 0 {
			s.AccessExpires = now.Add(sessionReuse)
		}
		if t.RefreshToken != "" {
			s.RefreshToken = t.RefreshToken
		}
	}
	s.UsedAt = now
	if err := o.Sessions.PutSession(ctx, s); err != nil {
		slog.Error("セッションを置けない", "err", err)
		writeProblem(w, &backend.Error{Code: backend.CodeUnavailable, Detail: "セッションを置けない"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": s.AccessToken, "token_type": "bearer", "expires_in": int(time.Until(s.AccessExpires) / time.Second)})
}

// dropSession は、セッションを消し、cookie を消して、unauthenticated を返す。WebUI は authorize に行く。
func (o *OAuth) dropSession(ctx context.Context, w http.ResponseWriter, idHash string, stored bool) {
	if stored {
		if err := o.Sessions.DeleteSession(ctx, idHash); err != nil {
			slog.Warn("セッションを消せない", "err", err)
		}
	}
	clearSessionCookie(w)
	writeProblem(w, &backend.Error{Code: backend.CodeUnauthenticated, Detail: "セッションが切れた。サインインし直す"})
}

// signOut は、POST /v1/oauth/signout である。cookie のセッションを消し、cookie も消す。
func (o *OAuth) signOut(w http.ResponseWriter, r *http.Request) {
	if !o.sameOriginJSON(w, r) {
		return
	}
	if id := sessionIDFrom(r); id != "" && o.Sessions != nil {
		h := hashSessionID(id)
		unlock := lockSession(h)
		err := o.Sessions.DeleteSession(r.Context(), h)
		unlock()
		if err != nil {
			slog.Error("セッションを消せない", "err", err)
			writeProblem(w, &backend.Error{Code: backend.CodeUnavailable, Detail: "セッションを消せない"})
			return
		}
	}
	clearSessionCookie(w)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
