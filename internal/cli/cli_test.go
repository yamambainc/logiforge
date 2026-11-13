package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

const (
	H0 = "h-0000000000"
	S1 = "s-1111111111"
	H1 = "h-3333333333"
)

// env は、テストの環境変数である。
type env map[string]string

func (e env) get(k string) string { return e[k] }

func run(t *testing.T, dir string, e env, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	a := &App{Stdout: &out, Stderr: &errb, Stdin: strings.NewReader(stdin), Getenv: e.get, Dir: dir}
	code := a.Run(context.Background(), args)
	return code, out.String(), errb.String()
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// clone は、木の入ったリモートと、その clone を作る。
func clone(t *testing.T) string {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	src := t.TempDir()
	git(t, src, "init", "--quiet", "--initial-branch=main")
	write := func(p string, b []byte) {
		full := filepath.Join(src, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, b, 0o644)
	}
	write("tree/"+H0+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H0, Statement: "根"}))
	write("tree/"+H0+"/"+S1+"/story.md", tree.MarshalStory(tree.StoryContent{ID: S1, Title: "t", Summary: "s"}))
	write("tree/"+H0+"/"+S1+"/"+H1+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: H1, Statement: "子"}))
	git(t, src, "add", "-A")
	git(t, src, "commit", "--quiet", "-m", "用意")
	dir := filepath.Join(t.TempDir(), "q")
	git(t, filepath.Dir(dir), "clone", "--quiet", src, dir)
	git(t, dir, "config", "logiforge.repository", "q")
	return dir
}

func TestScaffoldAndCheck(t *testing.T) {
	dir := clone(t)
	e := env{"LOGIFORGE_SESSION_ID": "s1"}
	git(t, dir, "checkout", "--quiet", "-b", "work")

	code, out, errs := run(t, dir, e, "", "story", "new", "--parent", H1, "--title", "新しいストーリー", "--summary", "概要", "--hypothesis", "孫1", "--hypothesis", "孫2", "--json")
	var s scaffold
	if code != 0 || json.Unmarshal([]byte(out), &s) != nil || len(s.Files) != 3 {
		t.Fatalf("story new: %d %s %s", code, out, errs)
	}
	hids := s.IDs["hypothesis_ids"].([]any)
	report := filepath.Join(t.TempDir(), "r.md")
	_ = os.WriteFile(report, []byte("# レポート\n"), 0o644)
	data := filepath.Join(t.TempDir(), "data.csv")
	_ = os.WriteFile(data, []byte("a,b\n"), 0o644)
	if code, out, errs := run(t, dir, e, "", "verdict", "new", "--hypothesis", hids[0].(string), "--value", "true", "--report", report, "--artifact", data, "--link", "論文=https://example.com/p"); code != 0 {
		t.Fatalf("verdict new: %s %s", out, errs)
	}
	bg := filepath.Join(t.TempDir(), "bg.md")
	_ = os.WriteFile(bg, []byte("背景\n"), 0o644)
	if code, out, errs := run(t, dir, e, "", "node", "edit", hids[1].(string), "--append-background", bg); code != 0 {
		t.Fatalf("node edit: %s %s", out, errs)
	}

	// trailer のないコミットは、警告する。
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "trailer なし")
	code, out, _ = run(t, dir, e, "", "check", "--json")
	var r checkResult
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || !r.OK || len(r.Operations) != 2 || len(r.Warnings) != 1 {
		t.Fatalf("check: %d %s", code, out)
	}
	// logiforge commit は、セッションの trailer を付ける。
	if code, out, errs := run(t, dir, e, "", "commit", "--amend", "--no-edit"); code != 0 {
		t.Fatalf("commit: %s %s", out, errs)
	}
	if got := git(t, dir, "log", "-1", "--format=%(trailers:key=Logiforge-Session-Id,valueonly)"); got != "s1" {
		t.Fatalf("trailer: %q", got)
	}
	code, out, _ = run(t, dir, e, "", "check", "--json")
	if json.Unmarshal([]byte(out), &r) != nil || code != 0 || len(r.Warnings) != 0 {
		t.Fatalf("check 2: %d %s", code, out)
	}

	// 読めない変化（すでにある仮説の書き換え）は、誤りにする。終了コードは 1。
	_ = os.WriteFile(filepath.Join(dir, "tree", H0, "hypothesis.md"), tree.MarshalHypothesis(tree.HypothesisContent{ID: H0, Statement: "書き換え"}), 0o644)
	code, out, _ = run(t, dir, e, "", "check")
	if code != ExitError || !strings.Contains(out, "change_not_allowed") {
		t.Fatalf("check 3: %d %s", code, out)
	}
	if code, _, _ := run(t, dir, e, "", "story", "new"); code != ExitUsage {
		t.Fatalf("引数なし：%d", code)
	}
}

// stub は、API の要求を記録し、決めた応答を返す。
type stub struct {
	reqs []*http.Request
	body []string
}

func (s *stub) server(t *testing.T, handle func(r *http.Request) (int, string, string)) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.reqs, s.body = append(s.reqs, r), append(s.body, string(b))
		status, ctype, body := handle(r)
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestApplyRequestCommands(t *testing.T) {
	dir := clone(t)
	git(t, dir, "checkout", "--quiet", "-b", "work")
	_ = os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "x")
	head := git(t, dir, "rev-parse", "HEAD")
	var st stub
	closedCount := 0
	host := st.server(t, func(r *http.Request) (int, string, string) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/repositories/q/apply-requests":
			items := ""
			for i := 0; i < closedCount; i++ {
				if i > 0 {
					items += ","
				}
				items += `{"id":"apr_old","branch":"work","state":"closed"}`
			}
			return 200, "application/json", `{"items":[` + items + `],"next_cursor":null}`
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/apply-requests/apr_1"):
			return 200, "application/json", `{"id":"apr_1","state":"open","versions":[{"version":1,"revision":"` + head + `"}]}`
		case r.URL.Path == "/v1/repositories/q/apply-requests/apr_9":
			return 404, "application/problem+json", `{"type":"about:blank","title":"Not Found","status":404,"code":"not_found","detail":"ない"}`
		}
		return 201, "application/json", `{"id":"apr_1","state":"open","branch":"work","version":1}`
	})
	e := env{"LOGIFORGE_HOST": host, "LOGIFORGE_TOKEN": "tok", "LOGIFORGE_SESSION_ID": "s1"}

	// push していないブランチでは、API を呼ばない。
	if code, _, errs := run(t, dir, e, "", "ar", "create", "--branch", "work"); code != ExitError || !strings.Contains(errs, "push") || len(st.reqs) != 0 {
		t.Fatalf("push 前：%d %s", code, errs)
	}
	git(t, dir, "push", "--quiet", "origin", "work")
	code, out, _ := run(t, dir, e, "", "ar", "create", "--branch", "work", "--json")
	var req *http.Request
	for _, r := range st.reqs {
		if r.Method == "POST" {
			req = r
		}
	}
	if code != 0 || req.Header.Get("Logiforge-Session-Id") != "s1" || req.Header.Get("Authorization") != "Bearer tok" || req.Header.Get("Idempotency-Key") == "" ||
		!strings.Contains(st.body[len(st.body)-1], `"revision":"`+head+`"`) || !strings.HasPrefix(out, `{"id":"apr_1"`) {
		t.Fatalf("ar create: %d %s %v %s", code, out, req.Header, st.body[0])
	}
	key := req.Header.Get("Idempotency-Key")
	posts := func() []*http.Request {
		var out []*http.Request
		for _, r := range st.reqs {
			if r.Method == "POST" {
				out = append(out, r)
			}
		}
		return out
	}
	run(t, dir, e, "", "ar", "create", "--branch", "work")
	if p := posts(); p[len(p)-1].Header.Get("Idempotency-Key") != key {
		t.Fatal("送り直しで、Idempotency-Key が変わった")
	}
	// close したあと、同じブランチの同じ先頭から作り直すと、キーが変わる（新しい apply request になる）。
	closedCount = 1
	run(t, dir, e, "", "ar", "create", "--branch", "work")
	if p := posts(); p[len(p)-1].Header.Get("Idempotency-Key") == key {
		t.Fatal("作り直しで、Idempotency-Key が変わらない")
	}

	// --version は、SHA に引き直して送る。
	if code, _, errs := run(t, dir, e, "", "ar", "review", "apr_1", "--approve", "--version", "1", "--prompt-revision", "p"); code != 0 {
		t.Fatalf("ar review: %s", errs)
	}
	if n := len(st.body); !strings.Contains(st.body[n-1], `"revision":"`+head+`"`) || !strings.Contains(st.body[n-1], `"kind":"approve"`) {
		t.Fatalf("review の本文：%s", st.body[n-1])
	}
	if code, _, _ := run(t, dir, e, "", "ar", "review", "apr_1", "--approve", "--request-changes", "--version", "1"); code != ExitUsage {
		t.Fatalf("種類が 2 つ：%d", code)
	}

	// API のエラーは、--json では Problem をそのまま書き、終了コードを 1 にする。
	code, out, _ = run(t, dir, e, "", "ar", "view", "apr_9", "--json")
	if code != ExitError || !strings.Contains(out, `"code":"not_found"`) {
		t.Fatalf("problem: %d %s", code, out)
	}
	// セッションがなければ、書き込みは失敗する。
	delete(e, "LOGIFORGE_SESSION_ID")
	if code, _, errs := run(t, dir, e, "", "ar", "close", "apr_1", "--reason", "r"); code != ExitError || !strings.Contains(errs, "セッション") {
		t.Fatalf("セッションなし：%d %s", code, errs)
	}
}

func TestAuth(t *testing.T) {
	home := t.TempDir()
	e := env{"HOME": home}
	if code, _, errs := run(t, "", e, "", "auth", "login", "--token", "tok", "--host", "https://lf.example.com/"); code != 0 {
		t.Fatal(errs)
	}
	p := filepath.Join(home, ".config", "logiforge", "config.yaml")
	info, err := os.Stat(p)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("設定ファイル：%v %v", info, err)
	}
	code, out, _ := run(t, "", e, "protocol=https\nhost=forge.example.com\n\n", "auth", "git-credential", "get")
	if code != 0 || out != "username=logiforge\npassword=tok\n" {
		t.Fatalf("credential: %q", out)
	}
	// 環境変数のトークンが、設定ファイルより先。
	e["LOGIFORGE_TOKEN"] = "env"
	if _, out, _ := run(t, "", e, "", "auth", "git-credential", "get"); !strings.Contains(out, "password=env") {
		t.Fatalf("env: %q", out)
	}
	if _, out, _ := run(t, "", e, "", "auth", "git-credential", "store"); out != "" {
		t.Fatalf("store: %q", out)
	}
}
