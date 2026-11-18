// Package forgejo は、Forgejo を GitForge として使うアダプタである（design/adapters/forgejo.md）。
// Forgejo の HTTP の API だけを使う。Forgejo のコードはリンクしない。
package forgejo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// Config は、Forgejo アダプタの設定である（design/settings/README.md）。
type Config struct {
	// URL は、Forgejo の URL（例：https://forgejo.example.com）。
	URL string
	// Org は、repository を置く organization。
	Org string
	// UserTeam は、主体の種類を user とみなす team の名前（例：logiforge-users）。
	UserTeam string
	// WorkerLogin は、worker の読むだけのユーザーのログイン名。API の主体としては受け付けない。
	WorkerLogin string
	// HTTP は、使う HTTP のクライアント。nil なら、時間の上限（30 秒）を付けたクライアント。
	HTTP *http.Client
	// MergeWait は、merge できるかを Forgejo が確かめ終わるまで待つ時間の上限。0 なら 15 秒。
	MergeWait time.Duration
	// Directories は、ディレクトリの repository の登録（名前と owner/name:パス）である（design/directory/README.md の「登録」）。
	// ディレクトリを置く git リポジトリは、git リポジトリ全体の repository としては数えない。
	Directories []ports.Repository
}

// apiError は、Forgejo の API の誤りの応答である。
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("forgejo: %d %s", e.Status, e.Message) }

// errMessage は、Forgejo の誤りの応答の message を返す。
func errMessage(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Message
	}
	return ""
}

func statusOf(err error) int {
	var e *apiError
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

type client struct {
	cfg Config
}

func (c *client) http() *http.Client {
	if c.cfg.HTTP != nil {
		return c.cfg.HTTP
	}
	return defaultHTTP
}

// transferHTTP は、添付の送り受けに使う HTTP のクライアントである。時間の上限は、要求の ctx に任せる。
var transferHTTP = &http.Client{}

// transferKey は、要求が添付の送り受けであることを、ctx で示す。
type transferKey struct{}

// defaultHTTP は、既定の HTTP のクライアントである。Forgejo が応答しないときに、呼ぶ側が止まったままにならないよう、時間の上限を付ける。
var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

// do は、token で API を呼び、応答の JSON を out に読む。
func (c *client) do(ctx context.Context, token, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.URL, "/")+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, token, out)
}

func (c *client) send(req *http.Request, token string, out any) error {
	_, err := c.sendHeader(req, token, out)
	return err
}

// sendHeader は、send と同じく呼び、応答のヘッダーも返す。
func (c *client) sendHeader(req *http.Request, token string, out any) (http.Header, error) {
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	h := c.http()
	// 添付の送り受けは、大きく（上限 32 MiB）、遅い回線では時間がかかる。クライアントの時間の上限ではなく、要求の ctx の上限に任せる。
	if req.Context().Value(transferKey{}) != nil && c.cfg.HTTP == nil {
		h = transferHTTP
	}
	res, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &m)
		return res.Header, &apiError{Status: res.StatusCode, Message: m.Message}
	}
	if out != nil && len(data) > 0 {
		if b, ok := out.(*[]byte); ok {
			*b = data
			return res.Header, nil
		}
		return res.Header, json.Unmarshal(data, out)
	}
	return res.Header, nil
}

// list は、ページを順に読み、すべてを返す。X-Total-Count の数まで読んだか、空のページで終わる。
// ページを受け付けない API（すべてを 1 度に返す）でも、X-Total-Count で終わる。
func list[T any](ctx context.Context, c *client, token, path string) ([]T, error) {
	const limit = 50
	var all []T
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for page := 1; ; page++ {
		var items []T
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1%s%spage=%d&limit=%d", strings.TrimRight(c.cfg.URL, "/"), path, sep, page, limit), nil)
		if err != nil {
			return nil, err
		}
		h, err := c.sendHeader(req, token, &items)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) == 0 {
			return all, nil
		}
		if total, err := strconv.Atoi(h.Get("X-Total-Count")); err == nil && len(all) >= total {
			return all, nil
		}
	}
}

// upload は、multipart でファイルを送る。
func (c *client) upload(ctx context.Context, token, path, field, name string, content []byte, out any) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, name)
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	u := strings.TrimRight(c.cfg.URL, "/") + "/api/v1" + path + "?name=" + url.QueryEscape(name)
	req, err := http.NewRequestWithContext(context.WithValue(ctx, transferKey{}, true), http.MethodPost, u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.send(req, token, out)
}

// download は、添付のファイルを読む。
func (c *client) download(ctx context.Context, token, rawURL string) ([]byte, error) {
	if !strings.HasPrefix(rawURL, strings.TrimRight(c.cfg.URL, "/")+"/") {
		return nil, fmt.Errorf("forgejo: 添付の URL が、Forgejo の URL の下にない: %s", rawURL)
	}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, transferKey{}, true), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	var data []byte
	if err := c.send(req, token, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// repoPath は、gitforge_ref（owner/name）から、API のパスを作る。
func repoPath(ref string) (string, error) {
	// ディレクトリの repository の参照（owner/name:パス）では、git リポジトリの参照だけを使う。
	ref, _ = ports.SplitRef(ref)
	owner, name, ok := strings.Cut(ref, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("forgejo: gitforge_ref の形が正しくない: %q", ref)
	}
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}

// gitRef は、gitforge_ref の、git リポジトリの参照（ディレクトリのパスを除いたもの）である。
func gitRef(ref string) string { g, _ := ports.SplitRef(ref); return g }
