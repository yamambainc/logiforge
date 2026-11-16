// logiforge は、Logiforge の 1 つのバイナリである。サブコマンドで、役割を選ぶ。
//
//	logiforge serve    バックエンドの API（design/api/README.md）
//	logiforge worker   git を読んでスナップショットを作り続ける worker（design/backend/worker.md）
//	logiforge worker-health  worker のハートビートを確かめる。古ければ 0 以外で終わる（design/deploy.md の「ヘルスチェック」）
//	logiforge <名詞> <動詞>  opener と reviewer の CLI（design/cli/README.md）。logiforge help で一覧を見る
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/forgejo"
	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/pullreq"
	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/snapstore"
	"github.com/yamambainc/hypothesis-verification-harness/internal/api"
	"github.com/yamambainc/hypothesis-verification-harness/internal/backend"
	"github.com/yamambainc/hypothesis-verification-harness/internal/cli"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/workcopy"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
	"github.com/yamambainc/hypothesis-verification-harness/webui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch {
	case len(os.Args) >= 2 && os.Args[1] == "serve":
		err = runServe(ctx)
	case len(os.Args) >= 2 && os.Args[1] == "worker":
		err = runWorker(ctx)
	case len(os.Args) >= 2 && os.Args[1] == "worker-health":
		if err := workerHealth(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	default:
		app := &cli.App{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
		code := app.Run(ctx, os.Args[1:])
		stop()
		os.Exit(code)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("止まった", "err", err)
		os.Exit(1)
	}
}

// env は、必須の環境変数を読む。name の代わりに name_FILE があれば、そのパスのファイルの中身を値にする
// （末尾の改行は除く）。秘密を、マウントしたファイルで渡すためである（design/deploy.md）。
func env(name string) (string, error) {
	v, file := os.Getenv(name), os.Getenv(name+"_FILE")
	switch {
	case v != "" && file != "":
		return "", fmt.Errorf("環境変数 %s と %s_FILE の両方がある。どちらか一方にする", name, name)
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("%s_FILE: %w", name, err)
		}
		v = strings.TrimRight(string(b), "\r\n")
		if v == "" {
			return "", fmt.Errorf("%s_FILE のファイル %s が空である", name, file)
		}
		return v, nil
	case v == "":
		return "", fmt.Errorf("環境変数 %s がない（%s_FILE でファイルから渡してもよい）", name, name)
	}
	return v, nil
}

// runWorker は、worker を動かす。設定は、環境変数で受け取る（design/adapters/README.md の「設定と秘密情報」）。
// worker は、どのポートでも待ち受けない。見回りの間隔ごとに git を見て、置き場に書く（design/backend/worker.md）。
//
//	LOGIFORGE_WORKER_DIR       clone とハートビートを置く、永続するディレクトリ
//	LOGIFORGE_STORE_DIR        置き場（手元のディレクトリ）
//	LOGIFORGE_WORKER_INTERVAL  見回りの間隔（既定は 1s）
//	LOGIFORGE_GIT_TIMEOUT      git の操作 1 回の時間の上限（既定は 10m）
//	LOGIFORGE_WORKER_LIST_INTERVAL  repository の一覧を読み直す間隔（既定は 10s）
//	LOGIFORGE_GIT_ALLOW_PROTOCOL  git で使える転送の方式（既定は https:file。https、http、file の部分集合）
//	LOGIFORGE_FORGEJO_URL、LOGIFORGE_FORGEJO_ORG
//	LOGIFORGE_WORKER_LOGIN、LOGIFORGE_WORKER_TOKEN   読むだけのユーザー（read:repository と read:organization）
func runWorker(ctx context.Context) error {
	var cfg struct{ dir, storeDir, url, org, login, token string }
	for _, e := range []struct {
		name string
		dst  *string
	}{
		{"LOGIFORGE_WORKER_DIR", &cfg.dir}, {"LOGIFORGE_STORE_DIR", &cfg.storeDir},
		{"LOGIFORGE_FORGEJO_URL", &cfg.url}, {"LOGIFORGE_FORGEJO_ORG", &cfg.org},
		{"LOGIFORGE_WORKER_LOGIN", &cfg.login}, {"LOGIFORGE_WORKER_TOKEN", &cfg.token},
	} {
		name, dst := e.name, e.dst
		v, err := env(name)
		if err != nil {
			return err
		}
		*dst = v
	}
	interval, err := duration("LOGIFORGE_WORKER_INTERVAL", worker.DefaultInterval)
	if err != nil {
		return err
	}
	gitTimeout, err := duration("LOGIFORGE_GIT_TIMEOUT", worker.DefaultGitTimeout)
	if err != nil {
		return err
	}
	listInterval, err := duration("LOGIFORGE_WORKER_LIST_INTERVAL", worker.DefaultListInterval)
	if err != nil {
		return err
	}
	// worker は 1 つだけ動かす。ほかの worker が同じディレクトリで動いていれば、起動しない。
	// 置き場が手元のディレクトリなので、置き場の根にもロックを取る。
	release, err := worker.LockAll(cfg.dir, cfg.storeDir)
	if err != nil {
		return err
	}
	defer release()
	src := forgejo.NewWorkerSource(forgejo.Config{URL: cfg.url, Org: cfg.org}, cfg.login, cfg.token)
	gitEnv := src.GitEnv()
	if gitEnv, err = allowProtocol(gitEnv); err != nil {
		return err
	}
	w := worker.New(worker.Config{Dir: cfg.dir, GitEnv: gitEnv, Interval: interval, GitTimeout: gitTimeout, ListInterval: listInterval}, src, &store.Dir{Root: cfg.storeDir})
	slog.Info("worker を始めた", "interval", interval, "git_timeout", gitTimeout)
	return w.Run(ctx)
}

// workerHealth は、worker のハートビートを確かめる（`logiforge worker-health`）。worker と同じ環境変数を読む。
//
//	LOGIFORGE_WORKER_DIR       clone とハートビートを置く、永続するディレクトリ
//	LOGIFORGE_WORKER_INTERVAL  見回りの間隔（既定は 1s）
func workerHealth() error {
	dir := os.Getenv("LOGIFORGE_WORKER_DIR")
	if dir == "" {
		return fmt.Errorf("環境変数 LOGIFORGE_WORKER_DIR がない")
	}
	interval, err := duration("LOGIFORGE_WORKER_INTERVAL", worker.DefaultInterval)
	if err != nil {
		return err
	}
	return worker.CheckHeartbeat(dir, interval, time.Now())
}

// duration は、時間の長さの環境変数を読む。なければ def を返す。
func duration(name string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: 正の長さにする", name)
	}
	return d, nil
}

// allowProtocol は、LOGIFORGE_GIT_ALLOW_PROTOCOL（git で使える転送の方式）を読み、git の環境変数に足す。
// 既定は https と file だけ。テストの GitForge が http で動くときなどに、足す。
func allowProtocol(gitEnv []string) ([]string, error) {
	v := os.Getenv("LOGIFORGE_GIT_ALLOW_PROTOCOL")
	if v == "" {
		return gitEnv, nil
	}
	for _, p := range strings.Split(v, ":") {
		switch p {
		case "https", "file":
		case "http":
			slog.Warn("git で http を許している。読むだけのトークンが平文で流れる。テストのときだけ使う")
		default:
			return nil, fmt.Errorf("LOGIFORGE_GIT_ALLOW_PROTOCOL には、https、http、file だけを、: でつないで書く：%q", p)
		}
	}
	return append(gitEnv, "GIT_ALLOW_PROTOCOL="+v), nil
}

// envs は、必須の環境変数をまとめて読む。
func envs(dsts map[string]*string) error {
	names := make([]string, 0, len(dsts))
	for n := range dsts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v, err := env(n)
		if err != nil {
			return err
		}
		*dsts[n] = v
	}
	return nil
}

// oauthConfig は、WebUI のサインインの中継の設定を読む（design/webui/oauth-relay.md）。
// 3 つともなければ nil を返す（サインインの中継を動かさない）。一部だけなら、誤りにする。
func oauthConfig(forgejoURL string) (*api.OAuth, error) {
	names := []string{"LOGIFORGE_OAUTH_CLIENT_ID", "LOGIFORGE_OAUTH_CLIENT_SECRET", "LOGIFORGE_OAUTH_REDIRECT_URI"}
	set := 0
	for _, n := range names {
		if os.Getenv(n) != "" || os.Getenv(n+"_FILE") != "" {
			set++
		}
	}
	if set == 0 {
		return nil, nil
	}
	var id, secret, redirect string
	if err := envs(map[string]*string{names[0]: &id, names[1]: &secret, names[2]: &redirect}); err != nil {
		return nil, fmt.Errorf("WebUI のサインインには、%s の 3 つが要る：%w", strings.Join(names, "、"), err)
	}
	return &api.OAuth{ForgejoURL: forgejoURL, ClientID: id, ClientSecret: secret, RedirectURI: redirect}, nil
}

// keyring は、記録の MAC の鍵を読む。LOGIFORGE_RECORD_KEYS は「鍵の ID=鍵,…」で、最初のものを書くときに使う。
func keyring(v string) (pullreq.Keyring, error) {
	k := pullreq.Keyring{Keys: map[string][]byte{}}
	for i, part := range strings.Split(v, ",") {
		id, secret, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || id == "" || secret == "" {
			return k, fmt.Errorf("LOGIFORGE_RECORD_KEYS の形が正しくない（鍵の ID=鍵,…）")
		}
		if i == 0 {
			k.Current = id
		}
		k.Keys[id] = []byte(secret)
	}
	return k, nil
}

// runServe は、バックエンドの API を動かす。設定は、環境変数で受け取る（design/adapters/README.md の「設定と秘密情報」）。
//
//	LOGIFORGE_ADDR             待ち受けるアドレス（既定は 127.0.0.1:8080）
//	LOGIFORGE_STORE_DIR        置き場（手元のディレクトリ。読むだけ）
//	LOGIFORGE_FORGEJO_URL、LOGIFORGE_FORGEJO_ORG、LOGIFORGE_USER_TEAM
//	LOGIFORGE_WRITER、LOGIFORGE_WRITER_TOKEN       書き込み主体
//	LOGIFORGE_WORKER_LOGIN、LOGIFORGE_WORKER_TOKEN   worker のユーザー（API の主体として受け付けない）。
//	                           apply request の作成で、git を読むのにも使う（読むだけのトークン）
//	LOGIFORGE_GIT_CACHE_DIR    作業用の git の複製を置く、一時的なディレクトリ（既定は OS の一時ディレクトリ）
//	LOGIFORGE_GIT_ALLOW_PROTOCOL  git で使える転送の方式（worker と同じ）
//	LOGIFORGE_RECORD_KEYS      記録の MAC の鍵（鍵の ID=鍵,…。最初のものを書くときに使う）
//	LOGIFORGE_WAIT_TIMEOUT     書いたものが読む側に出るまで待つ時間の上限（既定は 10s）
//	LOGIFORGE_OAUTH_CLIENT_ID、LOGIFORGE_OAUTH_CLIENT_SECRET、LOGIFORGE_OAUTH_REDIRECT_URI
//	                           WebUI のサインイン（Forgejo の OAuth2）。3 つともなければ、サインインの中継を動かさない
func runServe(ctx context.Context) error {
	var storeDir, url, org, team, writer, writerToken, workerLogin, workerToken, keys string
	if err := envs(map[string]*string{
		"LOGIFORGE_STORE_DIR":   &storeDir,
		"LOGIFORGE_FORGEJO_URL": &url, "LOGIFORGE_FORGEJO_ORG": &org, "LOGIFORGE_USER_TEAM": &team,
		"LOGIFORGE_WRITER": &writer, "LOGIFORGE_WRITER_TOKEN": &writerToken, "LOGIFORGE_WORKER_LOGIN": &workerLogin,
		"LOGIFORGE_WORKER_TOKEN": &workerToken, "LOGIFORGE_RECORD_KEYS": &keys,
	}); err != nil {
		return err
	}
	ring, err := keyring(keys)
	if err != nil {
		return err
	}
	addr := os.Getenv("LOGIFORGE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	forge, auth := forgejo.New(forgejo.Config{URL: url, Org: org, UserTeam: team, Writer: writer, WriterToken: writerToken, WorkerLogin: workerLogin}, ring)
	// apply request の作成で git を読む、作業用の複製。worker と同じ、読むだけのユーザーで読む。
	reader := forgejo.NewWorkerSource(forgejo.Config{URL: url, Org: org}, workerLogin, workerToken)
	gitEnv, err := allowProtocol(reader.GitEnv())
	if err != nil {
		return err
	}
	cacheDir := os.Getenv("LOGIFORGE_GIT_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "logiforge-git")
	}
	git, err := workcopy.NewCache(cacheDir, gitEnv, reader.GitURL)
	if err != nil {
		return fmt.Errorf("作業用の git の複製の置き場を作れない：%w", err)
	}
	// 作業用の複製は、使い捨てのキャッシュである。止まるときに消す。
	defer os.RemoveAll(git.Dir)
	b := &backend.Backend{Forge: forge, Auth: auth, Snap: &snapstore.Adapter{Store: &store.Dir{Root: storeDir}}, Git: git}
	if v := os.Getenv("LOGIFORGE_WAIT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("LOGIFORGE_WAIT_TIMEOUT: %w", err)
		}
		b.WaitTimeout = d
	}
	oauth, err := oauthConfig(url)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: api.NewServer(b, slog.Default(), api.Options{OAuth: oauth, WebUI: webui.Dist()}), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("バックエンドを始めた", "addr", addr)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		return ctx.Err()
	}
}
