// Package e2e は、動く Forgejo と、logiforge の serve、worker、CLI をつないで、利用の流れを通すテストである。
// FORGEJO_URL と FORGEJO_ADMIN_TOKEN がなければ飛ばす。Forgejo は scripts/forgejo-test.sh で起動する。
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var bin string // ビルドした logiforge

func TestMain(m *testing.M) {
	if os.Getenv("FORGEJO_URL") == "" {
		os.Exit(m.Run()) // 各テストが飛ばす
	}
	dir, err := os.MkdirTemp("", "logiforge-e2e")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "logiforge")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/logiforge")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("go build: %v: %s", err, out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// world は、Forgejo の organization と主体、動いている serve と worker である。
type world struct {
	t        *testing.T
	forgejo  string
	admin    string
	org      string
	tokens   map[string]string
	api      string // serve の URL（/v1 の手前まで）
	storeDir string
	// workerEnv と stopWorker は、worker を止めて、また動かすために持つ。
	workerEnv  []string
	stopWorker func()
}

func (w *world) login(name string) string { return w.org + name }

func freePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// forgejoCall は、Forgejo の API を呼ぶ。want があれば、違うときに止める。
func (w *world) forgejoCall(token, method, path string, in, out any, want ...int) int {
	w.t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, w.forgejo+"/api/v1"+path, body)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		_ = json.NewDecoder(res.Body).Decode(out)
	}
	if len(want) > 0 && res.StatusCode != want[0] {
		w.t.Fatalf("Forgejo %s %s: %d", method, path, res.StatusCode)
	}
	return res.StatusCode
}

func (w *world) token(login string, scopes []string) string {
	b, _ := json.Marshal(map[string]any{"name": "t", "scopes": scopes})
	req, _ := http.NewRequest("POST", w.forgejo+"/api/v1/users/"+login+"/tokens", bytes.NewReader(b))
	req.SetBasicAuth(login, "pw-"+login+"-12345")
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		SHA1 string `json:"sha1"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.SHA1 == "" {
		w.t.Fatalf("token: %d", res.StatusCode)
	}
	return out.SHA1
}

// start は、logiforge のサブコマンドを動かす。テストの終わりに止める。ログは、失敗したときに出す。
// 戻り値は、それより前に止める関数である。
func (w *world) start(name string, env []string, args ...string) func() {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		w.t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	w.t.Cleanup(func() {
		stop()
		if w.t.Failed() {
			w.t.Logf("---- %s のログ ----\n%s", name, log.String())
		}
	})
	return stop
}

// restartWorker は、止めた worker を、同じディレクトリと設定で、また動かす。
func (w *world) restartWorker() {
	w.stopWorker = w.start("worker", w.workerEnv, "worker")
}

// setup は、Forgejo に organization と主体を作り、serve と worker を動かす。
func setup(t *testing.T) *world {
	fu, admin := os.Getenv("FORGEJO_URL"), os.Getenv("FORGEJO_ADMIN_TOKEN")
	if fu == "" || admin == "" {
		t.Skip("FORGEJO_URL と FORGEJO_ADMIN_TOKEN がない（scripts/forgejo-test.sh で Forgejo を起動する）")
	}
	w := &world{t: t, forgejo: fu, admin: admin, org: fmt.Sprintf("e2e%d", time.Now().UnixNano()%1e9), tokens: map[string]string{}}
	w.forgejoCall(admin, "POST", "/orgs", map[string]any{"username": w.org, "visibility": "private"}, nil, 201)
	team := func(name string, perm string, units map[string]string, createRepo bool) int64 {
		var out struct {
			ID int64 `json:"id"`
		}
		w.forgejoCall(admin, "POST", "/orgs/"+w.org+"/teams", map[string]any{"name": name, "permission": perm, "units_map": units,
			"includes_all_repositories": true, "can_create_org_repo": createRepo}, &out, 201)
		return out.ID
	}
	writers := team("writers", "write", map[string]string{"repo.code": "write", "repo.pulls": "write", "repo.issues": "write"}, true)
	users := team("logiforge-users", "read", map[string]string{"repo.code": "write", "repo.pulls": "read", "repo.issues": "read"}, false)
	agents := team("agents", "read", map[string]string{"repo.code": "write", "repo.pulls": "read", "repo.issues": "read"}, false)
	readers := team("readers", "read", map[string]string{"repo.code": "read"}, false)
	for name, tm := range map[string]int64{"writer": writers, "alice": users, "agent": agents, "reviewer": agents, "reader": readers} {
		login := w.login(name)
		w.forgejoCall(admin, "POST", "/admin/users", map[string]any{"username": login, "email": login + "@example.com", "password": "pw-" + login + "-12345", "must_change_password": false}, nil, 201)
		scopes := []string{"all"}
		if name == "reader" {
			scopes = []string{"read:repository", "read:organization"}
		}
		w.tokens[name] = w.token(login, scopes)
		w.forgejoCall(admin, "PUT", fmt.Sprintf("/teams/%d/members/%s", tm, login), nil, nil, 204)
	}

	w.storeDir = t.TempDir()
	workerAddr, serveAddr := freePort(t), freePort(t)
	common := []string{"LOGIFORGE_STORE_DIR=" + w.storeDir, "LOGIFORGE_INTERNAL_KEY=internal-key", "LOGIFORGE_FORGEJO_URL=" + fu, "LOGIFORGE_FORGEJO_ORG=" + w.org,
		"LOGIFORGE_WORKER_LOGIN=" + w.login("reader")}
	w.workerEnv = append(common, "LOGIFORGE_WORKER_DIR="+t.TempDir(), "LOGIFORGE_WORKER_ADDR="+workerAddr, "LOGIFORGE_WORKER_INTERVAL=1s",
		"LOGIFORGE_WORKER_TOKEN="+w.tokens["reader"], "LOGIFORGE_GIT_ALLOW_PROTOCOL=http:https:file")
	w.stopWorker = w.start("worker", w.workerEnv, "worker")
	w.start("serve", append(common, "LOGIFORGE_ADDR="+serveAddr, "LOGIFORGE_WORKER_URL=http://"+workerAddr, "LOGIFORGE_USER_TEAM=logiforge-users",
		"LOGIFORGE_WRITER="+w.login("writer"), "LOGIFORGE_WRITER_TOKEN="+w.tokens["writer"], "LOGIFORGE_RECORD_KEYS=k1=e2e-secret",
		"LOGIFORGE_WAIT_TIMEOUT=20s"), "serve")
	w.api = "http://" + serveAddr
	w.eventually("serve が起動する", 20*time.Second, func() bool {
		res, err := http.Get(w.api + "/v1/repositories")
		if err == nil {
			res.Body.Close()
		}
		return err == nil
	})
	return w
}

func (w *world) eventually(what string, limit time.Duration, ok func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(limit)
	for !ok() {
		if time.Now().After(deadline) {
			w.t.Fatalf("待っても、%s にならない", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// call は、Logiforge の API を、主体のトークンで呼ぶ。
func (w *world) call(who, method, path string, in any, headers ...string) (int, []byte) {
	w.t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, w.api+"/v1"+path, body)
	req.Header.Set("Authorization", "Bearer "+w.tokens[who])
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

// user は、CLI を使う主体（エージェント）の環境である。
type user struct {
	w       *world
	who     string
	home    string
	session string
	dir     string // clone のディレクトリ
}

func (w *world) user(who, session string) *user {
	u := &user{w: w, who: who, home: w.t.TempDir(), session: session}
	return u
}

func (u *user) env() []string {
	return append(os.Environ(),
		"HOME="+u.home, "XDG_CONFIG_HOME="+filepath.Join(u.home, ".config"),
		"GIT_CONFIG_GLOBAL="+filepath.Join(u.home, ".gitconfig"), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME="+u.who, "GIT_AUTHOR_EMAIL="+u.who+"@example.com", "GIT_COMMITTER_NAME="+u.who, "GIT_COMMITTER_EMAIL="+u.who+"@example.com",
		"LOGIFORGE_HOST="+u.w.api, "LOGIFORGE_SESSION_ID="+u.session)
}

// cli は、CLI を動かし、終了コードと標準出力を返す。
func (u *user) cli(stdin string, args ...string) (int, string) {
	u.w.t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = u.env()
	cmd.Dir = u.dir
	if cmd.Dir == "" {
		cmd.Dir = u.home
	}
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		u.w.t.Fatal(err)
	}
	if code != 0 {
		u.w.t.Logf("logiforge %s：終了コード %d：%s%s", strings.Join(args, " "), code, out.String(), errb.String())
	}
	return code, out.String()
}

// must は、CLI を動かし、成功しなければ止める。--json の出力を v に読む。
func (u *user) must(v any, args ...string) {
	u.w.t.Helper()
	code, out := u.cli("", append(args, "--json")...)
	if code != 0 {
		u.w.t.Fatalf("logiforge %s：%d", strings.Join(args, " "), code)
	}
	if v != nil {
		if err := json.Unmarshal([]byte(out), v); err != nil {
			u.w.t.Fatalf("logiforge %s の出力：%v：%s", strings.Join(args, " "), err, out)
		}
	}
}

func (u *user) git(args ...string) string {
	u.w.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env, cmd.Dir = u.env(), u.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		u.w.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (u *user) write(p, content string) {
	full := filepath.Join(u.dir, p)
	_ = os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		u.w.t.Fatal(err)
	}
}

// login は、CLI にトークンを渡し、git の資格情報を設定し、repository を clone する。
func (u *user) loginAndClone(repo string) {
	u.w.t.Helper()
	if code, _ := u.cli(u.w.tokens[u.who]+"\n", "auth", "login", "--with-token"); code != 0 {
		u.w.t.Fatal("auth login")
	}
	if code, _ := u.cli("", "auth", "setup-git", "--url", u.w.forgejo, "--insecure"); code != 0 {
		u.w.t.Fatal("auth setup-git")
	}
	dir := filepath.Join(u.home, repo)
	u.must(nil, "repo", "clone", repo, dir)
	u.dir = dir
}

var _ = context.Background
var _ = url.PathEscape
