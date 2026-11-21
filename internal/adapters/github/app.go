package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// App は、worker とバックエンドの作業用の複製が読むための、読むだけの GitHub App である（design/adapters/github.md の「読むだけの GitHub App」）。
// App は書かない（design/backend/invalid-main.md の「main はいつも正しい」）。App は、読むトークンを作るためだけにある。
//
// App の ID と秘密鍵から JWT を作り、installation token を作る。installation token は、owner（organization か個人）ごとに 1 つで、
// 登録した repository だけと、読む権限（Contents、Issues、Pull requests、Metadata の Read）だけに絞って作る。
// 有効な間は使い回し、切れる前（残りが RefreshBefore より短くなったら）に作り直す。
type App struct {
	id    string
	key   *rsa.PrivateKey
	c     *client
	repos map[string][]string // owner → GitHub のリポジトリの名前（owner を除く。重ならない）
	now   func() time.Time

	mu     sync.Mutex
	tokens map[string]appToken // owner → 作った installation token
}

type appToken struct {
	token   string
	expires time.Time
}

// RefreshBefore は、installation token を作り直す、切れるまでの残りの時間である。
// installation token は 1 時間で切れる。git の操作 1 回の時間の上限（既定 10 分）より長く残しておく。
// そのため、App を使う worker は、git の操作の時間の上限をこれより短くする（cmd/logiforge が起動のときに確かめる）。
const RefreshBefore = 20 * time.Minute

// appPermissions は、installation token に求める権限である。App にこれより狭い権限しかなければ、GitHub が断る（422）。
var appPermissions = map[string]string{"contents": "read", "issues": "read", "pull_requests": "read", "metadata": "read"}

// NewApp は、App の ID と、PEM の秘密鍵（GitHub が渡す PKCS#1 か、PKCS#8）から、App を作る。
func NewApp(cfg Config, id string, privateKey []byte) (*App, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("github: App の ID が空である")
	}
	key, err := parseRSAKey(privateKey)
	if err != nil {
		return nil, err
	}
	if err := cfg.check(); err != nil {
		return nil, err
	}
	repos := map[string][]string{}
	seen := map[string]bool{}
	for _, r := range cfg.Repositories {
		rp, _ := ports.SplitRef(r.GitForgeRef)
		if seen[rp] {
			continue // ディレクトリの repository は、同じ GitHub のリポジトリを分けて使う
		}
		seen[rp] = true
		owner, name, _ := strings.Cut(rp, "/")
		repos[owner] = append(repos[owner], name)
	}
	for _, ns := range repos {
		sort.Strings(ns)
	}
	return &App{id: id, key: key, c: newClient(cfg.APIURL, cfg.HTTP), repos: repos, now: time.Now, tokens: map[string]appToken{}}, nil
}

func parseRSAKey(b []byte) (*rsa.PrivateKey, error) {
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, errors.New("github: App の秘密鍵が PEM でない")
	}
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github: App の秘密鍵を読めない：%w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github: App の秘密鍵が RSA でない")
	}
	return rk, nil
}

// Token は、gitForgeRef の owner の installation token を返す。有効なものがあれば使い回す。
func (a *App) Token(ctx context.Context, gitForgeRef string) (string, error) {
	rp, _ := ports.SplitRef(gitForgeRef)
	owner, _, _ := strings.Cut(rp, "/")
	names, ok := a.repos[owner]
	if !ok {
		return "", fmt.Errorf("%w: %s", ports.ErrNotRegistered, gitForgeRef)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if t, ok := a.tokens[owner]; ok && t.expires.Sub(a.now()) > RefreshBefore {
		return t.token, nil
	}
	t, err := a.mint(ctx, owner, names)
	if err != nil {
		return "", err
	}
	a.tokens[owner] = t
	return t.token, nil
}

// Owners は、登録した GitHub リポジトリの owner と、それぞれのリポジトリの名前（owner を除く）を返す。
func (a *App) Owners() map[string][]string {
	out := make(map[string][]string, len(a.repos))
	for o, ns := range a.repos {
		out[o] = append([]string(nil), ns...)
	}
	return out
}

// Mint は、owner の、登録したリポジトリだけと Read だけに絞った installation token を、新しく作る。
// バックエンドが、worker のために作るときに使う（design/adapters/github-app.md の「worker のトークン」）。
func (a *App) Mint(ctx context.Context, owner string) (token string, expires time.Time, err error) {
	names, ok := a.repos[owner]
	if !ok {
		return "", time.Time{}, fmt.Errorf("%w: owner %s", ports.ErrNotRegistered, owner)
	}
	t, err := a.mint(ctx, owner, names)
	return t.token, t.expires, err
}

// mint は、owner の installation を引き、names だけに絞った、読むだけの installation token を作る。
func (a *App) mint(ctx context.Context, owner string, names []string) (appToken, error) {
	jwt, err := a.jwt()
	if err != nil {
		return appToken{}, err
	}
	// installation は、owner の登録したリポジトリのどれか 1 つから引く。organization でも個人でも同じに引ける。
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := a.c.do(ctx, jwt, "GET", "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(names[0])+"/installation", nil, &inst); err != nil {
		return appToken{}, fmt.Errorf("github: App が %s/%s に入っていない（App を owner %s に install し、repository を選ぶ）：%w", owner, names[0], owner, err)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	in := map[string]any{"repositories": names, "permissions": appPermissions}
	if err := a.c.do(ctx, jwt, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", inst.ID), in, &out); err != nil {
		return appToken{}, fmt.Errorf("github: owner %s の installation token を作れない（App の権限と、install した repository を確かめる）：%w", owner, err)
	}
	if out.Token == "" {
		return appToken{}, errors.New("github: installation token が空である")
	}
	return appToken{token: out.Token, expires: out.ExpiresAt}, nil
}

// jwt は、App として API を呼ぶための JWT（RS256、10 分より短い）を作る。時計のずれに備えて、iat を 60 秒前にする。
func (a *App) jwt() (string, error) {
	now := a.now()
	enc := base64.RawURLEncoding
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	p, _ := json.Marshal(map[string]any{"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": a.id})
	s := enc.EncodeToString(h) + "." + enc.EncodeToString(p)
	sum := sha256.Sum256([]byte(s))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return s + "." + enc.EncodeToString(sig), nil
}
