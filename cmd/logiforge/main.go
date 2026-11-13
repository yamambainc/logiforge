// logiforge は、Logiforge の 1 つのバイナリである。サブコマンドで、役割を選ぶ。
//
//	logiforge worker   git を読んでスナップショットを作り続ける worker（design/backend/worker.md）
//
// バックエンド（serve）と CLI のコマンドは、あとの PR で足す。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/adapters/forgejo"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/worker"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "使い方：logiforge worker")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "worker":
		err = runWorker(ctx)
	default:
		err = fmt.Errorf("知らないサブコマンド：%s", os.Args[1])
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
	release, err := worker.Lock(cfg.dir)
	if err != nil {
		return err
	}
	defer release()
	src := forgejo.NewWorkerSource(forgejo.Config{URL: cfg.url, Org: cfg.org}, cfg.login, cfg.token)
	w := worker.New(worker.Config{Dir: cfg.dir, GitEnv: src.GitEnv(), Interval: interval}, src, &store.Dir{Root: cfg.storeDir})

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
