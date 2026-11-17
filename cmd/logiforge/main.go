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

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/adapters/snapstore"
	"github.com/yamambainc/logiforge/internal/api"
	"github.com/yamambainc/logiforge/internal/backend"
	"github.com/yamambainc/logiforge/internal/cli"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
	"github.com/yamambainc/logiforge/internal/version"
	"github.com/yamambainc/logiforge/internal/workcopy"
	"github.com/yamambainc/logiforge/internal/worker"
	"github.com/yamambainc/logiforge/webui"
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
	case len(os.Args) >= 2 && os.Args[1] == "record-public-keys":
		// LOGIFORGE_RECORD_KEYS から、worker に渡す公開鍵（LOGIFORGE_RECORD_PUBLIC_KEYS の値）を作って書く。
		if err := recordPublicKeys(); err != nil {
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
//	LOGIFORGE_WORKER_RECORD_INTERVAL  記録を読んで控えに足す間隔（既定は見回りの間隔）
//	LOGIFORGE_GIT_ALLOW_PROTOCOL  git で使える転送の方式（既定は https:file。https、http、file の部分集合）
//	LOGIFORGE_GITFORGE         構成（forgejo か github。既定は forgejo）と、構成ごとの環境変数（setupForge）
//	                           Forgejo の読むだけのユーザーは、read:repository、read:organization、read:issue
//	LOGIFORGE_RECORD_PUBLIC_KEYS  記録の署名の公開鍵（鍵の ID=base64,…）。反映かの判定で記録を確かめる（design/backend/verify.md）。
//	                           logiforge record-public-keys が、LOGIFORGE_RECORD_KEYS から作る
func runWorker(ctx context.Context) error {
	var cfg struct{ dir, storeDir, publicKeys string }
	if err := envs(map[string]*string{"LOGIFORGE_WORKER_DIR": &cfg.dir, "LOGIFORGE_STORE_DIR": &cfg.storeDir, "LOGIFORGE_RECORD_PUBLIC_KEYS": &cfg.publicKeys}); err != nil {
		return err
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
	recordInterval, err := duration("LOGIFORGE_WORKER_RECORD_INTERVAL", 0)
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
	pub, err := pullreq.ParsePublicKeys(cfg.publicKeys)
	if err != nil {
		return fmt.Errorf("LOGIFORGE_RECORD_PUBLIC_KEYS: %w", err)
	}
	// 反映かの判定は、バックエンドと同じ規則で、読むだけのトークンで行う。記録は、公開鍵だけで確かめる。
	fs, err := setupForge(pub, false)
	if err != nil {
		return err
	}
	src := fs.reader
	gitEnv := src.GitEnv()
	if gitEnv, err = allowProtocol(gitEnv); err != nil {
		return err
	}
	st := &store.Dir{Root: cfg.storeDir}
	fs.forge.Log = snapstore.RecordLog{Store: st}
	v := verifier{b: &backend.Backend{Forge: fs.forge, Snap: &snapstore.Adapter{Store: st}}, token: fs.readToken, keys: pub, ids: src}
	records := snapstore.LogSyncer{Source: src, Keys: pub, Store: st}
	w := worker.New(worker.Config{Dir: cfg.dir, GitEnv: gitEnv, Interval: interval, GitTimeout: gitTimeout, ListInterval: listInterval,
		Verifier: v, Records: records, RecordInterval: recordInterval}, src, st)
	slog.Info("worker を始めた", "version", version.Get().Version, "interval", interval, "git_timeout", gitTimeout)
	return w.Run(ctx)
}

// verifier は、worker の反映かの判定を、バックエンドの規則で、worker のトークンで行う。
type verifier struct {
	b     *backend.Backend
	token string
	keys  pullreq.Keyring
	ids   interface {
		RepoID(ctx context.Context, gitForgeRef string) (string, error)
	}
}

// VerifyInit は、用意のコミットの署名を確かめる（design/backend/verify.md の「用意のコミットの署名」）。
func (v verifier) VerifyInit(ctx context.Context, r ports.Repository, kind, parent, migratedFrom, signature string, added map[string]string) (bool, error) {
	id, err := v.ids.RepoID(ctx, r.GitForgeRef)
	if err != nil {
		return false, err
	}
	files := make([]pullreq.InitFile, 0, len(added))
	for p, sum := range added {
		files = append(files, pullreq.InitFile{Path: p, SHA256: sum})
	}
	return v.keys.VerifyInit(pullreq.InitScope{Kind: kind, RepoID: id, Directory: r.Directory(), Parent: parent, MigratedFrom: migratedFrom}, files, signature) == nil, nil
}

func (v verifier) VerifyMerge(ctx context.Context, r ports.Repository, sha string, parents []string, prevVersion string, files tree.Files, broken *store.Broken) (ports.MergeVerdict, error) {
	return v.b.VerifyMerge(ports.WithToken(ctx, v.token), r.Name, r.GitForgeRef, sha, parents, prevVersion, files, broken)
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
func oauthConfig(forgejoURL, kind string) (*api.OAuth, error) {
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
	return &api.OAuth{ForgejoURL: forgejoURL, PublicURL: os.Getenv("LOGIFORGE_FORGEJO_PUBLIC_URL"), ClientID: id, ClientSecret: secret, RedirectURI: redirect, GitForge: kind}, nil
}

// keyring は、記録の署名の秘密鍵を読む。LOGIFORGE_RECORD_KEYS は「鍵の ID=秘密,…」で、最初のものを書くときに使う。
// 秘密の SHA-256 を、Ed25519 の秘密鍵の種にする（pullreq.KeyringFromSecrets）。
func keyring(v string) (pullreq.Keyring, error) {
	var ids, secrets []string
	for _, part := range strings.Split(v, ",") {
		id, secret, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || id == "" || secret == "" {
			return pullreq.Keyring{}, fmt.Errorf("LOGIFORGE_RECORD_KEYS の形が正しくない（鍵の ID=秘密,…）")
		}
		ids, secrets = append(ids, id), append(secrets, secret)
	}
	return pullreq.KeyringFromSecrets(ids, secrets)
}

// recordPublicKeys は、LOGIFORGE_RECORD_KEYS の秘密から、記録の署名の公開鍵を作り、標準出力に書く。
func recordPublicKeys() error {
	v, err := env("LOGIFORGE_RECORD_KEYS")
	if err != nil {
		return err
	}
	ring, err := keyring(v)
	if err != nil {
		return err
	}
	fmt.Println(ring.FormatPublicKeys())
	return nil
}

// runServe は、バックエンドの API を動かす。設定は、環境変数で受け取る（design/adapters/README.md の「設定と秘密情報」）。
//
//	LOGIFORGE_ADDR             待ち受けるアドレス（既定は 127.0.0.1:8080）
//	LOGIFORGE_STORE_DIR        置き場（手元のディレクトリ。読むだけ）
//	LOGIFORGE_GITFORGE         構成（forgejo か github。既定は forgejo）と、構成ごとの環境変数（setupForge）。
//	                           読むだけのトークンは、apply request の作成で git を読むのにも使う
//	LOGIFORGE_GIT_CACHE_DIR    作業用の git の複製を置く、一時的なディレクトリ（既定は OS の一時ディレクトリ）
//	LOGIFORGE_GIT_ALLOW_PROTOCOL  git で使える転送の方式（worker と同じ）
//	LOGIFORGE_RECORD_KEYS      記録の署名の秘密（鍵の ID=秘密,…。最初のものを書くときに使う。Ed25519 の鍵は、秘密の SHA-256 から作る）
//	LOGIFORGE_WAIT_TIMEOUT     書いたものが読む側に出るまで待つ時間の上限（既定は 10s）
//	LOGIFORGE_OAUTH_CLIENT_ID、LOGIFORGE_OAUTH_CLIENT_SECRET、LOGIFORGE_OAUTH_REDIRECT_URI
//	                           WebUI のサインイン（Forgejo の OAuth2 か、GitHub の OAuth App）。3 つともなければ、サインインの中継を動かさない
//	LOGIFORGE_FORGEJO_PUBLIC_URL  ブラウザから Forgejo への URL（authorize に使う）。既定は LOGIFORGE_FORGEJO_URL
func runServe(ctx context.Context) error {
	var storeDir, keys string
	if err := envs(map[string]*string{"LOGIFORGE_STORE_DIR": &storeDir, "LOGIFORGE_RECORD_KEYS": &keys}); err != nil {
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
	st := &store.Dir{Root: storeDir}
	fs, err := setupForge(ring, true)
	if err != nil {
		return err
	}
	forge, auth := fs.forge, fs.auth
	// 反映の手順でも、worker の記録の控えを合わせて読む（design/backend/merge.md の手順 1）。
	forge.Log = snapstore.RecordLog{Store: st}
	// apply request の作成で git を読む、作業用の複製。worker と同じ、読むだけのトークンで読む。
	reader := fs.reader
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
	b := &backend.Backend{Forge: forge, Auth: auth, Snap: &snapstore.Adapter{Store: st}, Git: git}
	if v := os.Getenv("LOGIFORGE_WAIT_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("LOGIFORGE_WAIT_TIMEOUT: %w", err)
		}
		b.WaitTimeout = d
	}
	oauth, err := oauthConfig(fs.webURL, fs.kind)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: addr, Handler: api.NewServer(b, slog.Default(), api.Options{OAuth: oauth, WebUI: webui.Dist()}), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("バックエンドを始めた", "version", version.Get().Version, "addr", addr)
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
