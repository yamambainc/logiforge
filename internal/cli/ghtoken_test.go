package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGh は、gh の代わりのスクリプトを作る。呼ばれたら引数を log に書き、script の残りを実行する。
func fakeGh(t *testing.T, script string) (gh, log string) {
	t.Helper()
	dir := t.TempDir()
	gh, log = filepath.Join(dir, "gh"), filepath.Join(dir, "calls")
	body := "#!/bin/sh\necho \"$@\" >> '" + log + "'\n" + script + "\n"
	if err := os.WriteFile(gh, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return gh, log
}

func ghCalls(log string) string {
	b, _ := os.ReadFile(log)
	return strings.TrimSpace(string(b))
}

// ghBackend は、GET /v1/oauth/config に config の応答を返し、ほかの API には受け取ったトークンを書き留めるバックエンドである。
type ghBackend struct {
	mu          sync.Mutex
	configCalls int
	bearer      []string
}

func newGhBackend(t *testing.T, status int, config string) (*ghBackend, string) {
	b := &ghBackend{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if r.URL.Path == "/v1/oauth/config" {
			b.configCalls++
			if r.Header.Get("Authorization") != "" {
				t.Errorf("oauth/config にトークンを送った")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(config))
			return
		}
		b.bearer = append(b.bearer, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"next_cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	return b, srv.URL
}

func runGh(t *testing.T, gh string, e env, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	a := &App{Stdout: &out, Stderr: &errb, Stdin: strings.NewReader(stdin), Getenv: e.get, PendingWait: time.Millisecond, Gh: gh}
	code := a.Run(context.Background(), args)
	return code, out.String(), errb.String()
}

const githubConfig = `{"gitforge":"github","authorize_url":"https://github.example.com/login/oauth/authorize","client_id":"c","redirect_uri":"https://lf.example.com/oauth/callback","scope":"offline_access"}`

// GitHub の構成で、トークンがなければ、gh auth token --hostname <GitHub のホスト> を使う。API にも git にも。
func TestGhTokenOnGitHub(t *testing.T) {
	gh, log := fakeGh(t, `echo gho_fromgh`)
	b, host := newGhBackend(t, 200, githubConfig)
	e := env{"HOME": t.TempDir(), "LOGIFORGE_HOST": host}

	if code, _, errs := runGh(t, gh, e, "", "repo", "list"); code != 0 {
		t.Fatalf("repo list: %d %s", code, errs)
	}
	if len(b.bearer) != 1 || b.bearer[0] != "Bearer gho_fromgh" {
		t.Fatalf("API に gh のトークンを送らない：%v", b.bearer)
	}
	if got := ghCalls(log); got != "auth token --hostname github.example.com" {
		t.Fatalf("gh の呼び方：%q", got)
	}

	code, out, errs := runGh(t, gh, e, "protocol=https\nhost=github.example.com\n\n", "auth", "git-credential", "get")
	if code != 0 || out != "username=logiforge\npassword=gho_fromgh\n" {
		t.Fatalf("credential: %d %q %s", code, out, errs)
	}
	// ほかのホストには、gh のトークンを渡さない。
	code, out, errs = runGh(t, gh, e, "protocol=https\nhost=other.example.com\n\n", "auth", "git-credential", "get")
	if code == 0 || strings.Contains(out, "gho_fromgh") || !strings.Contains(errs, "other.example.com") {
		t.Fatalf("ほかのホスト：%d %q %s", code, out, errs)
	}
}

// LOGIFORGE_TOKEN と設定ファイルのトークンは、GitHub の構成でも gh より先。gh もバックエンドの設定も見ない。
func TestGhTokenNotUsedWhenTokenGiven(t *testing.T) {
	gh, log := fakeGh(t, `echo gho_fromgh`)
	b, host := newGhBackend(t, 200, githubConfig)
	home := t.TempDir()
	e := env{"HOME": home, "LOGIFORGE_HOST": host, "LOGIFORGE_TOKEN": "env"}
	if _, out, _ := runGh(t, gh, e, "", "auth", "git-credential", "get"); !strings.Contains(out, "password=env") {
		t.Fatalf("env: %q", out)
	}
	delete(e, "LOGIFORGE_TOKEN")
	if code, _, errs := runGh(t, gh, e, "", "auth", "login", "--token", "file"); code != 0 {
		t.Fatal(errs)
	}
	if _, out, _ := runGh(t, gh, e, "", "auth", "git-credential", "get"); !strings.Contains(out, "password=file") {
		t.Fatalf("file: %q", out)
	}
	if ghCalls(log) != "" || b.configCalls != 0 {
		t.Fatalf("gh を呼んだ：%q %d", ghCalls(log), b.configCalls)
	}
}

// Forgejo の構成と、GitHub の構成だと確かめられないときは、gh を呼ばず、今までと同じ誤りにする。
func TestGhTokenNeverOutsideGitHub(t *testing.T) {
	cases := []struct {
		name, config string
		status       int
		noHost       bool
	}{
		{name: "Forgejo", status: 200, config: `{"gitforge":"forgejo","authorize_url":"https://forgejo.example.com/login/oauth/authorize","client_id":"c","redirect_uri":"r","scope":"s"}`},
		{name: "セットアップ前", status: 503, config: `{"code":"setup_required","status":503}`},
		{name: "oauth/config がない", status: 404, config: `{}`},
		{name: "JSON でない", status: 200, config: `<html>`},
		{name: "gitforge がない", status: 200, config: `{"authorize_url":"https://github.com/login/oauth/authorize"}`},
		{name: "知らない gitforge", status: 200, config: `{"gitforge":"gitlab","authorize_url":"https://gitlab.com/login/oauth/authorize"}`},
		{name: "https でない", status: 200, config: `{"gitforge":"github","authorize_url":"http://github.example.com/login/oauth/authorize"}`},
		{name: "authorize_url がない", status: 200, config: `{"gitforge":"github"}`},
		{name: "バックエンドの URL がない", noHost: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gh, log := fakeGh(t, `echo gho_fromgh`)
			e := env{"HOME": t.TempDir()}
			if !c.noHost {
				_, host := newGhBackend(t, c.status, c.config)
				e["LOGIFORGE_HOST"] = host
			}
			code, out, errs := runGh(t, gh, e, "protocol=https\nhost=forgejo.example.com\n\n", "auth", "git-credential", "get")
			if code != ExitError || out != "" || !strings.Contains(errs, errNoToken.Error()) {
				t.Fatalf("今までと同じ誤りにならない：%d %q %s", code, out, errs)
			}
			if got := ghCalls(log); got != "" {
				t.Fatalf("gh を呼んだ：%q", got)
			}
		})
	}
	// バックエンドにつながらないときも、gh を呼ばない。
	gh, log := fakeGh(t, `echo gho_fromgh`)
	e := env{"HOME": t.TempDir(), "LOGIFORGE_HOST": "http://127.0.0.1:1"}
	if code, _, errs := runGh(t, gh, e, "", "auth", "git-credential", "get"); code != ExitError || !strings.Contains(errs, errNoToken.Error()) || ghCalls(log) != "" {
		t.Fatalf("つながらない：%d %s %q", code, errs, ghCalls(log))
	}
}

// gh がない、またはログインしていなければ、何をすればよいかを示す。
func TestGhTokenErrors(t *testing.T) {
	_, host := newGhBackend(t, 200, githubConfig)
	e := env{"HOME": t.TempDir(), "LOGIFORGE_HOST": host}

	code, _, errs := runGh(t, "logiforge-test-no-such-gh", e, "", "auth", "git-credential", "get")
	if code != ExitError || !strings.Contains(errs, "gh がない") || !strings.Contains(errs, "gh auth login --hostname github.example.com") || !strings.Contains(errs, "auth login --with-token") {
		t.Fatalf("gh がない：%d %s", code, errs)
	}

	gh, _ := fakeGh(t, `echo "You are not logged into any GitHub hosts. To log in, run: gh auth login" >&2; exit 1`)
	code, out, errs := runGh(t, gh, e, "", "auth", "git-credential", "get")
	if code != ExitError || out != "" || !strings.Contains(errs, "not logged into") || !strings.Contains(errs, "gh auth login --hostname github.example.com") || !strings.Contains(errs, "auth login --with-token") {
		t.Fatalf("ログインしていない：%d %q %s", code, out, errs)
	}

	gh, _ = fakeGh(t, `exit 0`)
	if code, _, errs := runGh(t, gh, e, "", "auth", "git-credential", "get"); code != ExitError || !strings.Contains(errs, "gh auth login") {
		t.Fatalf("空のトークン：%d %s", code, errs)
	}
}
