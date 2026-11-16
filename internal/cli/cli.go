// Package cli は、opener と reviewer が使う CLI（logiforge）である（design/cli/README.md）。
// git とセットで使う。木を変えるときは、git で clone し、ひな形でファイルを作り、コミットして push し、ar create で apply request を作る。
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// 終了コード。
const (
	ExitOK    = 0
	ExitError = 1 // API のエラー、検査に通らない、git の失敗など
	ExitUsage = 2 // コマンドや引数の誤り
)

// App は、CLI の 1 回の実行である。
type App struct {
	Stdout, Stderr io.Writer
	Stdin          io.Reader
	// Getenv は、環境変数を読む。nil なら os.Getenv。
	Getenv func(string) string
	// Dir は、作業ディレクトリ。空ならプロセスの作業ディレクトリ。
	Dir  string
	HTTP *http.Client
	// PendingWait は、snapshot_pending が返ったときに、送り直すまで待つ時間である。0 なら 1 秒。
	PendingWait time.Duration

	json bool
	// errorExtra は、エラーの --json の出力（Problem）に足す項目である（例：ar create の idempotency_key）。
	errorExtra map[string]any
}

// usageError は、コマンドや引数の誤りである。
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

// problem は、API のエラーの応答（application/problem+json）である。
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code"`
	raw    []byte
}

func (p *problem) Error() string { return fmt.Sprintf("%s（%d）：%s", p.Code, p.Status, p.Detail) }

// Run は、引数のコマンドを実行し、終了コードを返す。
func (a *App) Run(ctx context.Context, args []string) int {
	if a.Getenv == nil {
		a.Getenv = os.Getenv
	}
	if a.HTTP == nil {
		a.HTTP = http.DefaultClient
	}
	// --json は、どこに書いてもよい。ただし、-- のあと（git に渡す引数など）では取り除かない。
	var rest []string
	for i, arg := range args {
		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if arg == "--json" {
			a.json = true
			continue
		}
		rest = append(rest, arg)
	}
	err := a.dispatch(ctx, rest)
	if err == nil {
		return ExitOK
	}
	var u usageError
	var p *problem
	var quiet *silent
	switch {
	case errors.As(err, &quiet):
		if !a.json {
			fmt.Fprintln(a.Stderr, "logiforge:", quiet.Error())
		}
		return ExitError
	case errors.As(err, &u):
		fmt.Fprintln(a.Stderr, "logiforge:", u.msg)
		fmt.Fprintln(a.Stderr, "使い方は logiforge help")
		return ExitUsage
	case errors.As(err, &p):
		if a.json {
			a.writeProblem(p.raw, p)
		} else {
			fmt.Fprintln(a.Stderr, "logiforge:", p.Error())
		}
		return ExitError
	default:
		if a.json {
			a.writeProblem(nil, problem{Type: "about:blank", Title: "CLI error", Code: "cli_error", Detail: err.Error()})
		} else {
			fmt.Fprintln(a.Stderr, "logiforge:", err)
		}
		return ExitError
	}
}

type command struct {
	name, args, help string
	run              func(a *App, ctx context.Context, args []string) error
}

func commands() []command {
	return []command{
		{"auth login", "--with-token | --token <トークン> [--host <URL>]", "トークン（--with-token なら標準入力から読む）とバックエンドの URL を、設定ファイルに書く", (*App).authLogin},
		{"auth setup-git", "[--url <GitForge の URL>] [--insecure]", "git が、このトークンで GitForge を読み書きするようにする", (*App).authSetupGit},
		{"auth git-credential", "get", "git の credential helper（auth setup-git が設定する）", (*App).authGitCredential},
		{"repo list", "", "repository の一覧", (*App).repoList},
		{"repo view", "<repository>", "repository", (*App).repoView},
		{"repo clone", "<repository> [<ディレクトリ>]", "repository を git で clone する", (*App).repoClone},
		{"commit", "[git commit の引数]", "セッションの trailer を付けて git commit する", (*App).commit},
		{"story new", "--parent <仮説> --title <タイトル> --summary <概要> --hypothesis <主文>...", "ストーリーと子仮説のひな形を作る", (*App).storyNew},
		{"story inactivate", "<ストーリー> --reason <理由>", "inactivated.md を作る", (*App).storyInactivate},
		{"verdict new", "--hypothesis <仮説> --value true|false --report <ファイル> [--artifact <ファイル>]... [--link <名前>=<URL>]... [--cite subtree:<ノード>@<SHA>|artifact:<sha256>|answer:<人への依頼>]... [--supersedes <検証結果>]...", "検証結果のひな形を作る", (*App).verdictNew},
		{"verdict view", "<検証結果>", "検証結果とレポート", (*App).verdictView},
		{"node edit", "<仮説> --append-background <ファイル>", "背景情報の末尾に加える", (*App).nodeEdit},
		{"node view", "<ノード> [--commit <SHA>]", "ノードの詳細", (*App).nodeView},
		{"node history", "<ノード>", "ノードの履歴", (*App).nodeHistory},
		{"check", "", "手元の作業ツリーを、origin/main と比べて検査する", (*App).check},
		{"tree view", "[--commit <SHA>]", "木の概要", (*App).treeView},
		{"tree nodes", "[--root <ノード>] [--depth <n>] [--commit <SHA>]", "木のノード", (*App).treeNodes},
		{"tree completion", "", "作業の終わりの判定", (*App).treeCompletion},
		{"tree export", "--format json|markdown [--commit <SHA>]", "木を書き出す", (*App).treeExport},
		{"artifact get", "<sha256> [--commit <SHA>] [-o <ファイル>]", "artifact の中身を読む", (*App).artifactGet},
		{"ar create", "--branch <ブランチ> [--description <説明>] [--idempotency-key <キー>]", "push したブランチから apply request を作る", (*App).arCreate},
		{"ar edit", "<apply request> --description <説明>", "説明欄を書き換える", (*App).arEdit},
		{"ar view", "<apply request>", "apply request の詳細", (*App).arView},
		{"ar list", "[--state open|merged|closed]", "apply request の一覧", (*App).arList},
		{"ar material", "<apply request>", "レビューの材料", (*App).arMaterial},
		{"ar review", "<apply request> --approve|--request-changes|--comment --commit <SHA> [--body <本文>] [--prompt-revision <リビジョン>]", "レビューを付ける（approve なら反映する）", (*App).arReview},
		{"ar close", "<apply request> --reason <理由>", "apply request を close する", (*App).arClose},
		{"ar cleanup", "", "反映か close された apply request の、手元のブランチを消す", (*App).arCleanup},
		{"work list", "[--subtree <ノード>] [--kind <種類>,...] [--include-claimed]", "着手できる作業", (*App).workList},
		{"human-request create", "--node <ノード> --kind pvr|decision --body <ファイル>", "人への依頼を起票する", (*App).hrCreate},
		{"human-request list", "[--kind] [--state] [--node]", "人への依頼の一覧", (*App).hrList},
		{"human-request view", "<人への依頼>", "人への依頼と回答", (*App).hrView},
	}
}

func (a *App) dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		a.help()
		return nil
	}
	for _, c := range commands() {
		words := strings.Fields(c.name)
		if len(args) >= len(words) && strings.Join(args[:len(words)], " ") == c.name {
			return c.run(a, ctx, args[len(words):])
		}
	}
	return usagef("知らないコマンド：%s", strings.Join(args, " "))
}

func (a *App) help() {
	fmt.Fprintln(a.Stdout, "使い方：logiforge <コマンド> [引数] [--json]")
	fmt.Fprintln(a.Stdout, "")
	for _, c := range commands() {
		fmt.Fprintf(a.Stdout, "  %s %s\n      %s\n", c.name, c.args, c.help)
	}
	fmt.Fprintln(a.Stdout, "")
	fmt.Fprintln(a.Stdout, "環境変数：LOGIFORGE_TOKEN、LOGIFORGE_HOST、LOGIFORGE_SESSION_ID、LOGIFORGE_REPO")
}

// ---- フラグ ----

// flags は、コマンドのフラグを読む。位置の引数は、フラグの前でも後でもよい。
type flags struct {
	*flag.FlagSet
	positional []string
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func newFlags(name string) *flags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &flags{FlagSet: fs}
}

func (f *flags) parse(args []string) error {
	for {
		if err := f.FlagSet.Parse(args); err != nil {
			return usagef("%s：%v", f.Name(), err)
		}
		args = f.Args()
		if len(args) == 0 {
			return nil
		}
		f.positional = append(f.positional, args[0])
		args = args[1:]
	}
}

func (f *flags) need(n int, what string) error {
	if len(f.positional) != n {
		return usagef("%s には %s が要る", f.Name(), what)
	}
	return nil
}

// ---- 設定 ----

// config は、設定ファイル（~/.config/logiforge/config.yaml）の中身である。
type config struct {
	Token string `yaml:"token,omitempty"`
	Host  string `yaml:"host,omitempty"`
}

func (a *App) configPath() (string, error) {
	if dir := a.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "logiforge", "config.yaml"), nil
	}
	home := a.Getenv("HOME")
	if home == "" {
		return "", errors.New("HOME がない")
	}
	return filepath.Join(home, ".config", "logiforge", "config.yaml"), nil
}

func (a *App) readConfig() (config, error) {
	var c config
	p, err := a.configPath()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, yaml.Unmarshal(b, &c)
}

// token は、トークンを、環境変数 LOGIFORGE_TOKEN、設定ファイルの順に探す。
func (a *App) token() (string, error) {
	if t := a.Getenv("LOGIFORGE_TOKEN"); t != "" {
		return t, nil
	}
	c, err := a.readConfig()
	if err != nil {
		return "", err
	}
	if c.Token == "" {
		return "", errors.New("トークンがない。logiforge auth login --token <トークン> か、LOGIFORGE_TOKEN で渡す")
	}
	return c.Token, nil
}

func (a *App) host() (string, error) {
	if h := a.Getenv("LOGIFORGE_HOST"); h != "" {
		return strings.TrimRight(h, "/"), nil
	}
	c, err := a.readConfig()
	if err != nil {
		return "", err
	}
	if c.Host == "" {
		return "", errors.New("バックエンドの URL がない。logiforge auth login --host <URL> か、LOGIFORGE_HOST で渡す")
	}
	return strings.TrimRight(c.Host, "/"), nil
}

// session は、セッション ID を、フラグ --session、環境変数 LOGIFORGE_SESSION_ID の順に探す。CLI は作らない。
func (a *App) session(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if s := a.Getenv("LOGIFORGE_SESSION_ID"); s != "" {
		return s, nil
	}
	return "", errors.New("セッション ID がない。--session か LOGIFORGE_SESSION_ID で渡す（CLI は作らない）")
}

// repo は、repository の名前を、フラグ --repo、環境変数 LOGIFORGE_REPO、clone の git の設定（logiforge.repository）の順に探す。
func (a *App) repo(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if r := a.Getenv("LOGIFORGE_REPO"); r != "" {
		return r, nil
	}
	if out, err := a.git("config", "--get", "logiforge.repository"); err == nil && out != "" {
		return out, nil
	}
	return "", errors.New("repository がわからない。clone の中で使うか、--repo か LOGIFORGE_REPO で渡す")
}

// ---- API ----

type request struct {
	method, path string
	query        url.Values
	body         any
	session      string
	idempotency  string
	// retry なら、通信の誤りか、502、503、504 のときに、同じ要求（同じ Idempotency-Key）を 1 度だけ送り直す。
	retry bool
}

type response struct {
	body    []byte
	pending string
	status  int
}

// pendingRetries は、snapshot_pending が返ったときに、送り直す回数である。
// worker は見回りの間隔（既定 1 秒）ごとに取り込むので、数秒待てば、多くは読む側に出ている（design/api/writes.md の「書いた直後の読み」）。
const pendingRetries = 3

func (a *App) call(ctx context.Context, r request) (response, error) {
	res, err := a.callOnce(ctx, r)
	// snapshot_pending は、何も書かずに返るので、どの要求でも、短く待って送り直してよい。
	for i := 0; i < pendingRetries && isPending(err); i++ {
		wait := a.PendingWait
		if wait == 0 {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return res, err
		case <-time.After(wait):
		}
		res, err = a.callOnce(ctx, r)
	}
	if r.retry && err != nil && !isPending(err) {
		var p *problem
		if !errors.As(err, &p) || p.Status == http.StatusBadGateway || p.Status == http.StatusServiceUnavailable || p.Status == http.StatusGatewayTimeout {
			return a.callOnce(ctx, r)
		}
	}
	return res, err
}

// isPending は、snapshot_pending の応答かを返す。
func isPending(err error) bool {
	var p *problem
	return errors.As(err, &p) && p.Code == "snapshot_pending"
}

func (a *App) callOnce(ctx context.Context, r request) (response, error) {
	host, err := a.host()
	if err != nil {
		return response{}, err
	}
	tok, err := a.token()
	if err != nil {
		return response{}, err
	}
	u := host + "/v1" + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	var body io.Reader
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return response{}, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, body)
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.session != "" {
		req.Header.Set("Logiforge-Session-Id", r.session)
	}
	if r.idempotency != "" {
		req.Header.Set("Idempotency-Key", r.idempotency)
	}
	res, err := a.HTTP.Do(req)
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, err
	}
	if res.StatusCode >= 300 {
		p := &problem{Status: res.StatusCode, Code: "http_error", Detail: strings.TrimSpace(string(data))}
		if strings.HasPrefix(res.Header.Get("Content-Type"), "application/problem+json") {
			_ = json.Unmarshal(data, p)
			p.raw = data
		}
		return response{}, p
	}
	return response{body: data, pending: res.Header.Get("Logiforge-Pending-Commit"), status: res.StatusCode}, nil
}

// getJSON は、API を呼び、応答を v に読む。
func (a *App) getJSON(ctx context.Context, path string, query url.Values, v any) error {
	res, err := a.call(ctx, request{method: http.MethodGet, path: path, query: query})
	if err != nil {
		return err
	}
	return json.Unmarshal(res.body, v)
}

// show は、API の応答を書く。--json なら、そのまま書く。そうでなければ、text で書く。
func (a *App) show(res response, text func(v map[string]any)) error {
	if res.pending != "" {
		fmt.Fprintf(a.Stderr, "書いたものは、まだ読む側に出ていない（コミット %s）。少し待って読み直す\n", res.pending)
	}
	if a.json {
		_, err := a.Stdout.Write(res.body)
		return err
	}
	var v map[string]any
	if err := json.Unmarshal(res.body, &v); err != nil || text == nil {
		return a.indent(res.body)
	}
	text(v)
	return nil
}

func (a *App) indent(b []byte) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		_, err := a.Stdout.Write(b)
		return err
	}
	buf.WriteByte('\n')
	_, err := buf.WriteTo(a.Stdout)
	return err
}

// writeJSON は、CLI が作った結果（ひな形、check）を書く。
func (a *App) writeJSON(v any) error {
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func repoPath(repo string) string { return "/repositories/" + url.PathEscape(repo) }

// ---- git ----

func (a *App) gitCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = a.Dir
	return cmd
}

// git は、git を動かし、標準出力を返す。
func (a *App) git(args ...string) (string, error) {
	cmd := a.gitCmd(args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// gitPassthrough は、git の出力を、そのまま利用者に見せる。
func (a *App) gitPassthrough(args ...string) error {
	cmd := a.gitCmd(args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = a.Stderr, a.Stderr, a.Stdin
	if a.json {
		cmd.Stdout = io.Discard
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
	}
	return nil
}

func str(v map[string]any, k string) string {
	switch x := v[k].(type) {
	case string:
		return x
	case nil:
		return ""
	case bool:
		return fmt.Sprint(x)
	case float64:
		return fmt.Sprint(int64(x))
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func value(v any) string {
	switch x := v.(type) {
	case nil:
		return "未決"
	case bool:
		if x {
			return "真"
		}
		return "偽"
	}
	return fmt.Sprint(v)
}

func items(v map[string]any) []map[string]any {
	var out []map[string]any
	list, _ := v["items"].([]any)
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeProblem は、エラーの Problem を書く。raw があれば、それに errorExtra の項目を足す。
func (a *App) writeProblem(raw []byte, p any) {
	v := map[string]any{}
	if raw == nil || json.Unmarshal(raw, &v) != nil {
		b, _ := json.Marshal(p)
		_ = json.Unmarshal(b, &v)
	}
	for k, x := range a.errorExtra {
		v[k] = x
	}
	_ = json.NewEncoder(a.Stdout).Encode(v)
}
