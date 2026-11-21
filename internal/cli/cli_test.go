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
	"time"

	"github.com/yamambainc/logiforge/internal/tree"
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
	a := &App{Stdout: &out, Stderr: &errb, Stdin: strings.NewReader(stdin), Getenv: e.get, Dir: dir, PendingWait: time.Millisecond}
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
	git(t, src, "config", "receive.denyCurrentBranch", "ignore") // テストで main に push できるようにする
	dir := filepath.Join(t.TempDir(), "q")
	git(t, filepath.Dir(dir), "clone", "--quiet", src, dir)
	git(t, dir, "config", "logiforge.repository", "q")
	return dir
}

func TestScaffoldAndCheck(t *testing.T) {
	dir := clone(t)
	e := env{}
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

	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "背景を足す")
	code, out, _ = run(t, dir, e, "", "check", "--json")
	var r checkResult
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || !r.OK || len(r.Operations) != 2 || len(r.Warnings) != 0 {
		t.Fatalf("check: %d %s", code, out)
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
	fail503, failN := false, 0
	host := st.server(t, func(r *http.Request) (int, string, string) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/apply-requests") && (fail503 || failN > 0):
			fail503 = false
			if failN > 0 {
				failN--
			}
			return 503, "application/problem+json", `{"type":"about:blank","title":"Service Unavailable","status":503,"code":"unavailable"}`
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/apply-requests/apr_1"):
			return 200, "application/json", `{"id":"apr_1","state":"open","head_commit":"` + head + `","head_commits":[{"commit":"` + head + `"}]}`
		case r.Method == "POST" && r.URL.Path == "/v1/repositories/q/apply-requests/apr_2/merge":
			return 409, "application/problem+json", `{"type":"about:blank","title":"Conflict","status":409,"code":"stale_target","detail":"ブランチを今の main に合わせる"}`
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/merge"):
			return 200, "application/json", `{"id":"apr_1","state":"merged","branch":"work","head_commit":"` + head + `"}`
		case r.URL.Path == "/v1/repositories/q/apply-requests/apr_9":
			return 404, "application/problem+json", `{"type":"about:blank","title":"Not Found","status":404,"code":"not_found","detail":"ない"}`
		}
		return 201, "application/json", `{"id":"apr_1","state":"open","branch":"work","head_commit":"` + head + `"}`
	})
	e := env{"LOGIFORGE_HOST": host, "LOGIFORGE_TOKEN": "tok"}
	posts := func() []*http.Request {
		var out []*http.Request
		for _, r := range st.reqs {
			if r.Method == "POST" {
				out = append(out, r)
			}
		}
		return out
	}

	// push していないブランチでは、API を呼ばない。
	if code, _, errs := run(t, dir, e, "", "ar", "create", "--branch", "work"); code != ExitError || !strings.Contains(errs, "push") || len(st.reqs) != 0 {
		t.Fatalf("push 前：%d %s", code, errs)
	}
	git(t, dir, "push", "--quiet", "origin", "work")
	code, out, _ := run(t, dir, e, "", "ar", "create", "--branch", "work", "--json")
	req := posts()[0]
	var created map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &created) != nil || created["id"] != "apr_1" ||
		req.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(st.body[len(st.body)-1], `"commit":"`+head+`"`) {
		t.Fatalf("ar create: %d %s %v", code, out, req.Header)
	}
	// 呼び出しの中の送り直し（503）は、同じ要求を送る。送り直しは、バックエンドがブランチで見分ける。
	fail503 = true
	if code, _, errs := run(t, dir, e, "", "ar", "create", "--branch", "work"); code != 0 {
		t.Fatalf("送り直し：%s", errs)
	}
	if p := posts(); len(p) != 3 {
		t.Fatalf("送り直しの数：%d", len(p))
	}

	// レビューしたコミットは、--commit の SHA で送る。
	if code, _, errs := run(t, dir, e, "", "ar", "review", "apr_1", "--approve", "--commit", head, "--prompt-revision", "p"); code != 0 {
		t.Fatalf("ar review: %s", errs)
	}
	if n := len(st.body); !strings.Contains(st.body[n-1], `"commit":"`+head+`"`) || !strings.Contains(st.body[n-1], `"kind":"approve"`) {
		t.Fatalf("review の本文：%s", st.body[n-1])
	}
	if code, _, _ := run(t, dir, e, "", "ar", "review", "apr_1", "--approve", "--request-changes", "--commit", head); code != ExitUsage {
		t.Fatalf("種類が 2 つ：%d", code)
	}

	// merge は、approve とは別の操作である。
	if code, out, errs := run(t, dir, e, "", "ar", "merge", "apr_1"); code != 0 || !strings.Contains(out, "merged") ||
		st.reqs[len(st.reqs)-1].URL.Path != "/v1/repositories/q/apply-requests/apr_1/merge" {
		t.Fatalf("ar merge: %d %s %s", code, out, errs)
	}
	// 確かめに通らなければ、理由と説明を書き、終了コードを 1 にする。
	if code, _, errs := run(t, dir, e, "", "ar", "merge", "apr_2"); code != ExitError || !strings.Contains(errs, "stale_target") || !strings.Contains(errs, "今の main に合わせる") {
		t.Fatalf("ar merge の失敗: %d %s", code, errs)
	}

	// API のエラーは、--json では Problem をそのまま書き、終了コードを 1 にする。
	code, out, _ = run(t, dir, e, "", "ar", "view", "apr_9", "--json")
	if code != ExitError || !strings.Contains(out, `"code":"not_found"`) {
		t.Fatalf("problem: %d %s", code, out)
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

// origin/main が進んだあとの check：base は分かれた点。同じファイルを origin/main でも変えていれば、警告する。
// origin/main の木とぶつかる識別子は、誤りにする。
func TestCheckAfterMainAdvanced(t *testing.T) {
	dir := clone(t)
	e := env{}
	origin := git(t, dir, "remote", "get-url", "origin")
	git(t, dir, "checkout", "--quiet", "-b", "work")
	bg := filepath.Join(dir, "tree", H0, S1, H1, "background.md")
	_ = os.WriteFile(bg, []byte("手元\n"), 0o644)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "背景")

	// origin/main を進める：関係のない背景情報と、同じ背景情報。
	other := filepath.Join(t.TempDir(), "o")
	git(t, filepath.Dir(other), "clone", "--quiet", origin, other)
	_ = os.WriteFile(filepath.Join(other, "tree", H0, "background.md"), []byte("根の背景\n"), 0o644)
	git(t, other, "add", "-A")
	git(t, other, "commit", "--quiet", "-m", "main 1")
	git(t, other, "push", "--quiet", "origin", "main")
	git(t, dir, "fetch", "--quiet", "origin")
	code, out, _ := run(t, dir, e, "", "check", "--json")
	var r checkResult
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || !r.OK || len(r.Warnings) != 0 || r.Base == git(t, dir, "rev-parse", "origin/main") {
		t.Fatalf("関係のない変化：%d %s", code, out)
	}

	_ = os.WriteFile(filepath.Join(other, "tree", H0, S1, H1, "background.md"), []byte("main でも\n"), 0o644)
	git(t, other, "add", "-A")
	git(t, other, "commit", "--quiet", "-m", "main 2")
	git(t, other, "push", "--quiet", "origin", "main")
	git(t, dir, "fetch", "--quiet", "origin")
	code, out, _ = run(t, dir, e, "", "check", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &r) != nil || len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "conflict") {
		t.Fatalf("同じファイル：%d %s", code, out)
	}

	// origin/main にできたストーリーと同じ識別子を、手元でも使うと、誤り。
	sid := "s-9999999999"
	for _, d := range []string{other, dir} {
		p := filepath.Join(d, "tree", H0, S1, H1, sid)
		_ = os.MkdirAll(filepath.Join(p, "h-8888888888"), 0o755)
		_ = os.WriteFile(filepath.Join(p, "story.md"), tree.MarshalStory(tree.StoryContent{ID: sid, Title: "t", Summary: "s"}), 0o644)
		_ = os.WriteFile(filepath.Join(p, "h-8888888888", "hypothesis.md"), tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-8888888888", Statement: "x"}), 0o644)
	}
	git(t, other, "add", "-A")
	git(t, other, "commit", "--quiet", "-m", "main 3")
	git(t, other, "push", "--quiet", "origin", "main")
	git(t, dir, "fetch", "--quiet", "origin")
	code, out, _ = run(t, dir, e, "", "check", "--json")
	if code != ExitError || !strings.Contains(out, "id_collision") || !strings.Contains(out, "コミットしていない変更がある") {
		t.Fatalf("識別子の衝突：%d %s", code, out)
	}
}

func TestAuthInputs(t *testing.T) {
	home := t.TempDir()
	e := env{"HOME": home}
	if code, _, errs := run(t, "", e, "tok-from-stdin\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal(errs)
	}
	if _, out, _ := run(t, "", e, "", "auth", "git-credential", "get"); !strings.Contains(out, "password=tok-from-stdin") {
		t.Fatalf("with-token: %q", out)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	if code, _, errs := run(t, "", e, "", "auth", "setup-git", "--url", "http://forge.example.com"); code != ExitError || !strings.Contains(errs, "https") {
		t.Fatalf("http を断らない：%d %s", code, errs)
	}
	if code, _, errs := run(t, "", e, "", "auth", "setup-git", "--url", "http://forge.example.com", "--insecure"); code != 0 {
		t.Fatalf("--insecure: %s", errs)
	}
}

func TestJSONAfterDoubleDash(t *testing.T) {
	var out, errb bytes.Buffer
	a := &App{Stdout: &out, Stderr: &errb}
	// -- のあとの --json は、取り除かない。
	code := a.Run(context.Background(), []string{"nope", "--", "--json"})
	if code != ExitUsage || a.json {
		t.Fatalf("code %d json %v", code, a.json)
	}
}

// 表示：検証結果の「反映：」に、反映した main のコミット（submitted_commit）を出す。
// apply request の表示に、push された先頭のコミットの一覧を、worker が見つけた時刻と一緒に出す。
func TestViewShowsCommits(t *testing.T) {
	dir := t.TempDir()
	sha, base := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var st stub
	host := st.server(t, func(r *http.Request) (int, string, string) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/verdicts/v-0000000001"):
			return 200, "application/json", `{"id":"v-0000000001","hypothesis_id":"h-0000000001","value":true,"report_body":"本文","artifacts":[],"citations":[],"submitted_commit":"` + sha + `","superseded_commit":null}`
		case strings.HasSuffix(r.URL.Path, "/apply-requests/apr_1"):
			return 200, "application/json", `{"id":"apr_1","state":"open","branch":"work","head_commit":"` + sha + `","head_commits":[{"commit":"` + base + `","base_commit":"","seen_at":"2026-11-14T01:02:03Z","author_session_id":null},{"commit":"` + sha + `","base_commit":"","seen_at":null,"author_session_id":null}],"operations":[],"reviews":[],"comments":[],"awaiting":"review","warnings":[]}`
		}
		return 404, "application/problem+json", `{"type":"about:blank","title":"Not Found","status":404,"code":"not_found"}`
	})
	e := env{"LOGIFORGE_HOST": host, "LOGIFORGE_TOKEN": "tok", "LOGIFORGE_REPO": "q"}
	code, out, errs := run(t, dir, e, "", "verdict", "view", "v-0000000001")
	if code != 0 || !strings.Contains(out, "反映："+sha) {
		t.Fatalf("verdict view：%d %q %s", code, out, errs)
	}
	code, out, errs = run(t, dir, e, "", "ar", "view", "apr_1")
	if code != 0 || !strings.Contains(out, "先頭のコミット "+sha) || !strings.Contains(out, base+" 2026-11-14T01:02:03Z") || !strings.Contains(out, sha+" （worker がまだ取り込んでいない）") {
		t.Fatalf("ar view：%d %q %s", code, out, errs)
	}
}

// snapshot_pending が返ったら、短く待って、数回まで送り直す。
func TestRetryOnSnapshotPending(t *testing.T) {
	var st stub
	pending := 2
	host := st.server(t, func(r *http.Request) (int, string, string) {
		if pending > 0 {
			pending--
			return 503, "application/problem+json", `{"type":"about:blank","title":"Service Unavailable","status":503,"code":"snapshot_pending"}`
		}
		return 200, "application/json", `{"root_hypothesis_id":"h-0000000000","root_value":null,"commit":"c","warnings":[]}`
	})
	e := env{"LOGIFORGE_HOST": host, "LOGIFORGE_TOKEN": "tok"}
	if code, out, errOut := run(t, t.TempDir(), e, "", "tree", "view", "--repo", "q", "--json"); code != 0 || len(st.reqs) != 3 {
		t.Fatalf("送り直し：%d %s %s（要求 %d 回）", code, out, errOut, len(st.reqs))
	}
	// 送り直しても snapshot_pending のままなら、あきらめて返す（最初の 1 回と、送り直し 3 回）。
	st.reqs, pending = nil, 100
	if code, _, _ := run(t, t.TempDir(), e, "", "tree", "view", "--repo", "q", "--json"); code == 0 || len(st.reqs) != 1+pendingRetries {
		t.Fatalf("あきらめない：%d（要求 %d 回）", code, len(st.reqs))
	}
}

// 最新のコミットが valid でなければ、木を読むコマンドは、結果を書いたうえで、標準エラーに警告を書く。終了コードは変えない
// （design/backend/invalid-main.md の「CLI」）。
func TestLatestInvalidWarns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Logiforge-Latest-Invalid", "c1")
		_, _ = io.WriteString(w, `{"root_hypothesis_id":"h-0000000000","root_value":null,"commit":"c1","warnings":[],"valid_commit":"c1","invalid_since":"c2"}`)
	}))
	t.Cleanup(srv.Close)
	e := env{"LOGIFORGE_HOST": srv.URL, "LOGIFORGE_TOKEN": "tok"}
	code, out, errOut := run(t, t.TempDir(), e, "", "tree", "view", "--repo", "q", "--json")
	if code != 0 || !strings.Contains(out, `"commit":"c1"`) || !strings.Contains(errOut, "valid でない") || !strings.Contains(errOut, "c1") {
		t.Fatalf("警告：%d %s %s", code, out, errOut)
	}
}

// logiforge version と --version は、版を示す。--json なら JSON。
func TestVersion(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"version"}, {"--version"}} {
		code, out, _ := run(t, dir, env{}, "", args...)
		if code != ExitOK || !strings.HasPrefix(out, "logiforge dev（コミット ") {
			t.Fatalf("%v：%d %q", args, code, out)
		}
	}
	code, out, _ := run(t, dir, env{}, "", "version", "--json")
	var v struct{ Version, Go string }
	if code != ExitOK || json.Unmarshal([]byte(out), &v) != nil || v.Version != "dev" || v.Go == "" {
		t.Fatalf("--json：%d %q", code, out)
	}
	if code, _, _ := run(t, dir, env{}, "", "version", "x"); code != ExitUsage {
		t.Fatalf("引数：%d", code)
	}
}
