// logiforge は、Logiforge の 1 つのバイナリである。サブコマンドで、役割を選ぶ。
//
//	logiforge serve    バックエンドの API（design/api/README.md）
//	logiforge worker   git を読んでスナップショットを作り続ける worker（design/backend/worker/README.md）
//	logiforge worker-health  worker のハートビートを確かめる。古ければ 0 以外で終わる（design/deploy/README.md の「ヘルスチェック」）
//	logiforge settings load|rekey  設定の DB を書く（design/settings/README.md）
//	logiforge <名詞> <動詞>  opener と reviewer の CLI（design/cli/README.md）。logiforge help で一覧を見る
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/yamambainc/logiforge/internal/cli"
	"github.com/yamambainc/logiforge/internal/ports"
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
	case len(os.Args) >= 2 && os.Args[1] == "settings":
		if err := runSettings(ctx, os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
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
// （末尾の改行は除く）。秘密を、マウントしたファイルで渡すためである（design/deploy/README.md）。
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

// lookup は、worker が、main のコミットを merge のコミットに持つ pull request を、worker のトークンで探す
// （design/backend/invalid-main.md の「どの apply request の反映か」）。
type lookup struct {
	forge ports.GitForge
	src   readSource
}

func (v lookup) MergedApplyRequest(ctx context.Context, r ports.Repository, sha string, committed time.Time) (string, string, error) {
	tok, err := v.src.ReadToken(ctx, r.GitForgeRef)
	if err != nil {
		return "", "", err
	}
	ar, err := v.forge.MergedApplyRequest(ports.WithToken(ctx, tok), r.GitForgeRef, sha, committed)
	if err != nil {
		return "", "", err
	}
	return ar.ID, ar.HeadRevision, nil
}

// Archived は、git リポジトリが archive されているかを、worker のトークンで読む（移した元の主張を確かめる。design/directory/migrate.md）。
func (v lookup) Archived(ctx context.Context, gitForgeRef string) (bool, error) {
	tok, err := v.src.ReadToken(ctx, gitForgeRef)
	if err != nil {
		return false, err
	}
	return v.forge.Archived(ports.WithToken(ctx, tok), gitForgeRef)
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
