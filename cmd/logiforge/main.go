// logiforge は、Logiforge の 1 つのバイナリである。サブコマンドで、役割を選ぶ。
//
//	logiforge serve    バックエンドの API（design/api/README.md）
//	logiforge worker   git を読んでスナップショットを作り続ける worker（design/backend/worker.md）
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
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
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

// env は、必須の環境変数を読む。
func env(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("環境変数 %s がない", name)
	}
	return v, nil
}

// runWorker は、worker を動かす。設定は、環境変数で受け取る（design/adapters/README.md の「設定と秘密情報」）。
//
//	LOGIFORGE_WORKER_DIR       clone を置く、永続するディレクトリ
//	LOGIFORGE_STORE_DIR        置き場（手元のディレクトリ）
//	LOGIFORGE_INTERNAL_KEY     内部 API の鍵（バックエンドと同じもの）
//	LOGIFORGE_WORKER_ADDR      内部 API を待ち受けるアドレス（既定は 127.0.0.1:8081）
//	LOGIFORGE_WORKER_INTERVAL  巡回の間隔（既定は 1m）
//	LOGIFORGE_GIT_ALLOW_PROTOCOL  git で使える転送の方式（既定は https:file）
//	LOGIFORGE_FORGEJO_URL、LOGIFORGE_FORGEJO_ORG
//	LOGIFORGE_WORKER_LOGIN、LOGIFORGE_WORKER_TOKEN   読むだけのユーザー（read:repository と read:organization）
func runWorker(ctx context.Context) error {
	var cfg struct{ dir, storeDir, key, url, org, login, token string }
	for _, e := range []struct {
		name string
		dst  *string
	}{
		{"LOGIFORGE_WORKER_DIR", &cfg.dir}, {"LOGIFORGE_STORE_DIR", &cfg.storeDir}, {"LOGIFORGE_INTERNAL_KEY", &cfg.key},
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
	addr := os.Getenv("LOGIFORGE_WORKER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	interval := time.Minute
	if v := os.Getenv("LOGIFORGE_WORKER_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("LOGIFORGE_WORKER_INTERVAL: %w", err)
		}
		interval = d
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
	// git で使える転送の方式。既定は https と file だけ。テストの GitForge が http で動くときなどに、足す。
	if v := os.Getenv("LOGIFORGE_GIT_ALLOW_PROTOCOL"); v != "" {
		gitEnv = append(gitEnv, "GIT_ALLOW_PROTOCOL="+v)
	}
	w := worker.New(worker.Config{Dir: cfg.dir, GitEnv: gitEnv, Interval: interval}, src, &store.Dir{Root: cfg.storeDir})

	srv := &http.Server{Addr: addr, Handler: w.Handler(cfg.key), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 2)
	go func() { errc <- srv.ListenAndServe() }()
	go func() { errc <- w.Run(ctx) }()
	slog.Info("worker を始めた", "addr", addr)
	select {
	case err := <-errc:
		_ = srv.Close()
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		return ctx.Err()
	}
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
//	LOGIFORGE_WORKER_URL、LOGIFORGE_INTERNAL_KEY   worker の内部 API
//	LOGIFORGE_FORGEJO_URL、LOGIFORGE_FORGEJO_ORG、LOGIFORGE_USER_TEAM
//	LOGIFORGE_WRITER、LOGIFORGE_WRITER_TOKEN       書き込み主体
//	LOGIFORGE_WORKER_LOGIN     worker のユーザー（API の主体として受け付けない）
//	LOGIFORGE_RECORD_KEYS      記録の MAC の鍵（鍵の ID=鍵,…。最初のものを書くときに使う）
//	LOGIFORGE_WAIT_TIMEOUT     書いたものが読む側に出るまで待つ時間の上限（既定は 10s）
func runServe(ctx context.Context) error {
	var storeDir, workerURL, key, url, org, team, writer, writerToken, workerLogin, keys string
	if err := envs(map[string]*string{
		"LOGIFORGE_STORE_DIR": &storeDir, "LOGIFORGE_WORKER_URL": &workerURL, "LOGIFORGE_INTERNAL_KEY": &key,
		"LOGIFORGE_FORGEJO_URL": &url, "LOGIFORGE_FORGEJO_ORG": &org, "LOGIFORGE_USER_TEAM": &team,
		"LOGIFORGE_WRITER": &writer, "LOGIFORGE_WRITER_TOKEN": &writerToken, "LOGIFORGE_WORKER_LOGIN": &workerLogin,
		"LOGIFORGE_RECORD_KEYS": &keys,
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
	b := &backend.Backend{Forge: forge, Auth: auth, Snap: &snapstore.Adapter{Store: &store.Dir{Root: storeDir}, WorkerURL: workerURL, Key: key}}
	if v := os.Getenv("LOGIFORGE_WAIT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("LOGIFORGE_WAIT_TIMEOUT: %w", err)
		}
		b.WaitTimeout = d
	}
	srv := &http.Server{Addr: addr, Handler: api.NewHandler(b, slog.Default()), ReadHeaderTimeout: 10 * time.Second}
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
