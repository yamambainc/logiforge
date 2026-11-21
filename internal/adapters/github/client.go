// Package github は、GitHub を GitForge として使うアダプタである（design/adapters/github.md）。
// Logiforge は、GitHub に書くための自分のアカウントを持たない。REST は、要求を送った主体のトークン（ports.WithToken）で呼ぶ。
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// DefaultAPIURL と DefaultWebURL は、github.com の REST の URL と、画面と git の URL である。
const (
	DefaultAPIURL = "https://api.github.com"
	DefaultWebURL = "https://github.com"
)

// apiError は、GitHub の API の誤りの応答である。
type apiError struct {
	Status  int
	Message string
	// Limited は、制限（二次の上限を含む）に当たり、待つ上限を超えたことを表す。権限がないことではない。
	Limited bool
}

func (e *apiError) Error() string { return fmt.Sprintf("github: %d %s", e.Status, e.Message) }

// Unwrap は、誤りの応答を、ポートの誤りにする（design/adapters/github.md の「GitHub の誤りの応答」）。
// バックエンドが、内部の誤り（500）でなく、意味のある誤りで返せるようにするためである。どれにも当たらなければ nil。
func (e *apiError) Unwrap() error {
	switch {
	case e.Limited || e.Status == http.StatusTooManyRequests || e.Status >= 500:
		return ports.ErrUnavailable
	case e.Status == http.StatusForbidden:
		return ports.ErrForbidden
	case e.Status == http.StatusNotFound:
		return ports.ErrNotFound
	}
	return nil
}

// errMessage は、GitHub の誤りの応答の message を返す。
func errMessage(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Message
	}
	return ""
}

// limited は、制限に当たった誤りかを返す。
func limited(err error) bool {
	var e *apiError
	return errors.As(err, &e) && e.Limited
}

func statusOf(err error) int {
	var e *apiError
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// defaultHTTP は、既定の HTTP のクライアントである。GitHub が応答しないときに止まったままにならないよう、時間の上限を付ける。
var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

// client は、REST の呼び方である。token が空なら、認証なしで呼ぶ。
type client struct {
	apiURL string
	http   *http.Client
	// maxWait は、制限に当たったときに待つ時間の上限の合計。0 なら 1 分。
	maxWait time.Duration
	// sleep は、テストで差し替える。
	sleep func(ctx context.Context, d time.Duration) error
}

func newClient(apiURL string, h *http.Client) *client {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	if h == nil {
		h = defaultHTTP
	}
	return &client{apiURL: strings.TrimRight(apiURL, "/"), http: h, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// graphQLURL は、GraphQL の URL である。github.com では https://api.github.com/graphql、GitHub Enterprise Server では
// REST の /api/v3 のかわりに /api/graphql である。
func (c *client) graphQLURL() string {
	if strings.HasSuffix(c.apiURL, "/api/v3") {
		return strings.TrimSuffix(c.apiURL, "/v3") + "/graphql"
	}
	return c.apiURL + "/graphql"
}

// graphQL は、token で GraphQL を 1 回呼び、応答の JSON を out に読む。1 件の pull request の review を読むときだけ使う
// （design/adapters/pull-requests.md の「数える review」）。
func (c *client) graphQL(ctx context.Context, token, query string, vars map[string]any, out any) error {
	return c.do(ctx, token, http.MethodPost, c.graphQLURL(), map[string]any{"query": query, "variables": vars}, out)
}

// do は、token で API を呼び、応答の JSON を out に読む。path は、/ で始まるパスか、完全な URL（ページの次の URL）。
func (c *client) do(ctx context.Context, token, method, path string, in, out any) error {
	_, err := c.doHeader(ctx, token, method, path, in, out)
	return err
}

// basicPrefix は、token がこれで始まれば、Authorization の値としてそのまま渡す印である（OAuth App の client_id と client secret の Basic 認証）。
const basicPrefix = "Basic "

// doHeader は、do と同じく呼び、応答のヘッダーも返す。
// 制限に当たったとき（403 か 429 で、Retry-After か、残りが 0）は、Retry-After を守って待ち、待つ間隔を延ばしながらやり直す
// （design/adapters/github-main.md の「GitHub の制約への対処」）。制限の応答では、要求は処理されていないので、書き込みもやり直してよい。
func (c *client) doHeader(ctx context.Context, token, method, path string, in, out any) (http.Header, error) {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = b
	}
	u := path
	if strings.HasPrefix(path, "/") {
		u = c.apiURL + path
	} else if !strings.HasPrefix(path, c.apiURL+"/") && path != c.graphQLURL() {
		return nil, fmt.Errorf("github: API の URL の下にない URL は呼ばない: %s", path)
	}
	maxWait := c.maxWait
	if maxWait == 0 {
		maxWait = time.Minute
	}
	var waited time.Duration
	backoff := time.Second
	for {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		switch {
		case strings.HasPrefix(token, basicPrefix):
			req.Header.Set("Authorization", token)
		case token != "":
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		if wait, limited := rateLimited(res); limited {
			if wait < backoff {
				wait = backoff
			}
			if waited+wait > maxWait {
				return res.Header, &apiError{Status: res.StatusCode, Message: "制限に当たった（待つ上限を超えた）", Limited: true}
			}
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
			waited += wait
			backoff *= 2
			continue
		}
		if res.StatusCode >= 300 {
			var m struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(data, &m)
			return res.Header, &apiError{Status: res.StatusCode, Message: m.Message}
		}
		if out != nil && len(data) > 0 {
			return res.Header, json.Unmarshal(data, out)
		}
		return res.Header, nil
	}
}

// rateLimited は、応答が制限によるものかと、待つ時間を返す。
func rateLimited(res *http.Response) (time.Duration, bool) {
	if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	if s := res.Header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return time.Duration(n) * time.Second, true
		}
	}
	if res.Header.Get("X-RateLimit-Remaining") == "0" {
		if n, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return time.Until(time.Unix(n, 0)) + time.Second, true
		}
		return time.Minute, true
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return time.Minute, true
	}
	return 0, false
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// list は、Link の next を辿って、すべてのページを読む。
func list[T any](ctx context.Context, c *client, token, path string) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	next := path + sep + "per_page=100"
	var all []T
	for next != "" {
		var items []T
		h, err := c.doHeader(ctx, token, http.MethodGet, next, nil, &items)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		next = ""
		if m := nextLink.FindStringSubmatch(h.Get("Link")); m != nil {
			next = m[1]
		}
	}
	return all, nil
}

// repoPath は、gitforge_ref（owner/name）から、API のパスを作る。
func repoPath(ref string) (string, error) {
	// ディレクトリの repository の参照（owner/name:パス）では、git リポジトリの参照だけを使う。
	ref, _ = ports.SplitRef(ref)
	owner, name, ok := strings.Cut(ref, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("github: gitforge_ref の形が正しくない: %q", ref)
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}

// gitRef は、gitforge_ref の、git リポジトリの参照（ディレクトリのパスを除いたもの）である。
func gitRef(ref string) string { g, _ := ports.SplitRef(ref); return g }

// listIn は、{"<key>": [...]} の形で返す一覧（/user/installations など）を、すべてのページについて読む。
func listIn[T any](ctx context.Context, c *client, token, path, key string) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	next := path + sep + "per_page=100"
	var all []T
	for next != "" {
		var page map[string]json.RawMessage
		h, err := c.doHeader(ctx, token, http.MethodGet, next, nil, &page)
		if err != nil {
			return nil, err
		}
		var items []T
		if raw, ok := page[key]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
		}
		all = append(all, items...)
		next = ""
		if m := nextLink.FindStringSubmatch(h.Get("Link")); m != nil {
			next = m[1]
		}
	}
	return all, nil
}
