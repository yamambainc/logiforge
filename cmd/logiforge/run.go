package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/forgejo"
	"github.com/yamambainc/logiforge/internal/adapters/github"
	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/adapters/snapstore"
	"github.com/yamambainc/logiforge/internal/api"
	"github.com/yamambainc/logiforge/internal/backend"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/settings"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/version"
	"github.com/yamambainc/logiforge/internal/workcopy"
	"github.com/yamambainc/logiforge/internal/worker"
	"github.com/yamambainc/logiforge/webui"
)

// 起動時に渡す環境変数（design/deploy/env.md）。ほかの設定は、設定の DB に置く。
const (
	envDB         = "LOGIFORGE_DB"
	envBackendKey = "LOGIFORGE_BACKEND_KEY"
	envSharedKey  = "LOGIFORGE_SHARED_KEY"
)

// openSettings は、設定の DB を開き、鍵を読む。backend が true なら、バックエンドの鍵も読む（worker には渡さない）。
func openSettings(backend bool) (*settings.DB, settings.Keys, error) {
	path, err := env(envDB)
	if err != nil {
		return nil, settings.Keys{}, err
	}
	var keys settings.Keys
	read := func(name string) (*settings.Key, error) {
		v, err := env(name)
		if err != nil {
			return nil, err
		}
		k, err := settings.ParseKey(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return k, nil
	}
	if keys.Shared, err = read(envSharedKey); err != nil {
		return nil, keys, err
	}
	if backend {
		if keys.Backend, err = read(envBackendKey); err != nil {
			return nil, keys, err
		}
		// 2 つの鍵が同じなら、worker が共有の鍵で、バックエンドだけの秘密を読める（design/settings/secrets.md の「2 つの鍵」）。
		if keys.Backend.ID() == keys.Shared.ID() {
			return nil, keys, fmt.Errorf("%s と %s が同じである。別の乱数にする", envBackendKey, envSharedKey)
		}
	} else if os.Getenv(envBackendKey) != "" || os.Getenv(envBackendKey+"_FILE") != "" {
		// worker に、バックエンドの鍵を渡さない（design/settings/secrets.md の「2 つの鍵」）。
		return nil, keys, fmt.Errorf("worker に %s を渡さない。記録の署名の秘密鍵と、GitHub App の client secret を、worker から隠すため", envBackendKey)
	}
	db, err := settings.Open(path, !backend)
	if err != nil {
		return nil, keys, err
	}
	return db, keys, nil
}

// keyringOf は、設定の DB の記録の鍵から、Keyring を作る。種がなければ（worker）、公開鍵だけの Keyring にする。
func keyringOf(ks []settings.RecordKey) (pullreq.Keyring, error) {
	if len(ks) > 0 && ks[0].Seed != nil {
		ids, seeds := make([]string, len(ks)), make([][]byte, len(ks))
		for i, k := range ks {
			ids[i], seeds[i] = k.ID, k.Seed
		}
		return pullreq.NewKeyring(ids, seeds)
	}
	ring := pullreq.Keyring{Public: map[string]ed25519.PublicKey{}}
	for _, k := range ks {
		ring.Public[k.ID] = k.Public
	}
	return ring, nil
}

// runServe は、バックエンドの API を動かす。
//
// バックエンドは、要求ごとに設定の DB の版を読み、変わっていれば、アダプタを作り直す。起動し直さずに、新しい設定が効く
// （design/settings/README.md の「設定を読むとき」）。設定がそろうまでは、setup_required を返す。
func runServe(ctx context.Context) error {
	db, keys, err := openSettings(true)
	if err != nil {
		return err
	}
	defer db.Close()
	storeDir, err := env("LOGIFORGE_STORE_DIR")
	if err != nil {
		return err
	}
	addr := os.Getenv("LOGIFORGE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	gitEnv, err := allowProtocol(nil)
	if err != nil {
		return err
	}
	cacheDir := os.Getenv("LOGIFORGE_GIT_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "logiforge-git")
	}
	if err := logSetupPending(ctx, db); err != nil {
		return err
	}
	l := &liveServer{db: db, keys: keys, store: &store.Dir{Root: storeDir}}
	l.setup = api.SetupHandler(&setupService{db: db, keys: keys, store: l.store}, slog.Default())
	// 作業用の複製は、使い捨てのキャッシュである。設定が変わっても、作り直さずに使い続ける。URL と資格情報は、今の設定のものを使う。
	// 設定がそろっていないときや、読みに失敗したあとは、reader がない。そのときは、空の URL と誤りにして、git を呼ばせない。
	l.git, err = workcopy.NewCache(cacheDir, gitEnv, func(ref string) string {
		if r := l.reader(); r != nil {
			return r.GitURL(ref)
		}
		return ""
	})
	if err != nil {
		return fmt.Errorf("作業用の git の複製の置き場を作れない：%w", err)
	}
	l.git.Credentials = func(ctx context.Context, ref string) ([]string, error) {
		r := l.reader()
		if r == nil {
			return nil, &backend.Error{Code: backend.CodeUnavailable, Detail: "設定の DB を読み直している。少し待って送り直す"}
		}
		return r.GitEnv(ctx, ref)
	}
	defer os.RemoveAll(l.git.Dir)
	go mintLoop(ctx, db, keys)
	srv := &http.Server{Addr: addr, Handler: l, ReadHeaderTimeout: 10 * time.Second}
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

// liveServer は、設定の版ごとに作った API のハンドラーを持つ。
type liveServer struct {
	db    *settings.DB
	keys  settings.Keys
	store *store.Dir
	git   *workcopy.Cache
	setup http.Handler

	mu   sync.Mutex
	rev  int64
	h    http.Handler
	read readSource
}

func (l *liveServer) reader() readSource {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.read
}

func (l *liveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, err := l.current(r.Context())
	if err != nil {
		slog.Error("設定の DB を読めない", "err", err)
		api.Unavailable("設定の DB を読めない。少し待って送り直す").ServeHTTP(w, r)
		return
	}
	h.ServeHTTP(w, r)
}

// current は、今の設定の版のハンドラーを返す。版が変わっていれば、作り直す。
func (l *liveServer) current(ctx context.Context) (http.Handler, error) {
	rev, err := l.db.Revision(ctx)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.h != nil && rev == l.rev {
		return l.h, nil
	}
	h, read, err := l.build(ctx)
	switch {
	case errors.Is(err, settings.ErrNotReady):
		// 設定が足りなければ、設定が書かれるまで setup_required にする。前の版の設定では動かし続けない。
		// セットアップの API と WebUI と、置いたあとならサインインの中継は配る（design/api/endpoints/setup.md）。
		slog.Warn("設定がそろっていない", "revision", rev, "err", err)
		var opt api.Options
		if opt, err = l.setupOptions(ctx); err != nil {
			slog.Error("設定の DB の中身を読めない。次の要求で読み直す", "revision", rev, "err", err)
			l.h, l.read = nil, nil
			return nil, err
		}
		h, read = api.SetupRequired(settings.ErrNotReady.Error(), opt), nil
	case err != nil:
		// 読みの失敗（ロックの待ちの上限、I/O の誤り、書きかけのジャーナル）や、形の誤りは、覚えない。次の要求で読み直す。
		slog.Error("設定の DB の中身から、バックエンドを作れない。次の要求で読み直す", "revision", rev, "err", err)
		l.h, l.read = nil, nil
		return nil, err
	default:
		slog.Info("設定の DB を読んだ", "revision", rev)
	}
	l.rev, l.h, l.read = rev, h, read
	return h, nil
}

func (l *liveServer) build(ctx context.Context) (http.Handler, readSource, error) {
	c, err := l.db.Load(ctx, l.keys)
	if err != nil {
		return nil, nil, err
	}
	if err := c.Ready(); err != nil {
		return nil, nil, err
	}
	ring, err := keyringOf(c.RecordKeys)
	if err != nil {
		return nil, nil, err
	}
	fs, err := setupForge(c, ring, true, nil)
	if err != nil {
		return nil, nil, err
	}
	// 反映の手順でも、worker の記録の控えを合わせて読む（design/backend/merge.md の手順 1）。
	fs.forge.Log = snapstore.RecordLog{Store: l.store}
	b := &backend.Backend{Forge: fs.forge, Auth: fs.auth, Snap: &snapstore.Adapter{Store: l.store}, Git: l.git}
	if c.Intervals.WaitTimeout != "" {
		b.WaitTimeout = settings.Duration(c.Intervals.WaitTimeout, b.WaitTimeout)
	}
	st := api.SettingsHandler(&settingsService{db: l.db, keys: l.keys, auth: fs.auth}, slog.Default())
	return api.NewServer(b, slog.Default(), api.Options{OAuth: oauthOf(c), WebUI: webui.Dist(), Setup: l.setup, Settings: st}), fs.reader, nil
}

// setupOptions は、セットアップが終わるまでに配るものである。
func (l *liveServer) setupOptions(ctx context.Context) (api.Options, error) {
	c, err := l.db.Load(ctx, l.keys)
	if err != nil {
		return api.Options{}, err
	}
	web := c.GitHub.WebURL
	if web == "" {
		web = github.DefaultWebURL
	}
	return api.Options{OAuth: oauthOf(c), WebUI: webui.Dist(), Setup: l.setup, SetupFormAction: originOf(web)}, nil
}

// originOf は、URL のオリジン（scheme://host）である。
func originOf(v string) string {
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// oauthOf は、WebUI のサインインの中継の設定を作る（design/webui/oauth-relay.md）。client_id がなければ nil（中継を動かさない）。
func oauthOf(c *settings.Config) *api.OAuth {
	id, secret, public, web := c.Forgejo.OAuthClientID, c.Forgejo.OAuthClientSecret, c.Forgejo.PublicURL, c.Forgejo.URL
	if c.GitForge == gitforgeGitHub {
		id, secret, public, web = c.GitHub.ClientID, c.GitHub.ClientSecret, "", c.GitHub.WebURL
		if web == "" {
			web = github.DefaultWebURL
		}
	}
	if c.GitForge == "" || id == "" || secret == "" || c.WebUIURL == "" {
		return nil
	}
	return &api.OAuth{ForgejoURL: trimSlash(web), PublicURL: public, ClientID: id, ClientSecret: secret,
		RedirectURI: trimSlash(c.WebUIURL) + "/oauth/callback", GitForge: c.GitForge}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// runWorker は、worker を動かす。worker は、設定の DB を読むだけで開き、共有の鍵だけを持つ。
// worker は、どのポートでも待ち受けない。見回りの間隔ごとに git を見て、置き場に書く（design/backend/worker/README.md）。
//
// repository の一覧を読み直す間隔ごとに、設定の DB の版を確かめ、変わっていれば、見回りを止めて、新しい設定で始め直す。
// 設定がそろうまでは、見回りをせずに待つ。待つあいだも、ハートビートを書く（design/backend/worker/operation.md の「ハートビート」）。
func runWorker(ctx context.Context) error {
	var dir, storeDir string
	if err := envs(map[string]*string{"LOGIFORGE_WORKER_DIR": &dir, "LOGIFORGE_STORE_DIR": &storeDir}); err != nil {
		return err
	}
	// 設定の DB は、バックエンドが作る。worker は読むだけで開くので、まだなければ、ハートビートを書きながら待つ。
	var db *settings.DB
	var keys settings.Keys
	for {
		var err error
		db, keys, err = openSettings(false)
		if err == nil {
			break
		}
		if !errors.Is(err, settings.ErrNoDB) {
			return err
		}
		slog.Warn("設定の DB がまだない。バックエンドが作るまで待つ", "err", err)
		if !wait(ctx, time.Second, dir) {
			return ctx.Err()
		}
	}
	defer db.Close()
	// worker は 1 つだけ動かす。ほかの worker が同じディレクトリで動いていれば、起動しない。
	// 置き場が手元のディレクトリなので、置き場の根にもロックを取る。
	release, err := worker.LockAll(dir, storeDir)
	if err != nil {
		return err
	}
	defer release()
	gitEnv, err := allowProtocol(nil)
	if err != nil {
		return err
	}
	st := &store.Dir{Root: storeDir}
	slog.Info("worker を始めた", "version", version.Get().Version)
	for {
		rev, err := db.Revision(ctx)
		if err != nil {
			slog.Error("設定の DB を読めない。待って読み直す", "err", err)
			if !wait(ctx, time.Second, dir) {
				return ctx.Err()
			}
			continue
		}
		w, listInterval, err := buildWorker(ctx, db, keys, dir, gitEnv, st)
		switch {
		case errors.Is(err, settings.ErrNotReady):
			slog.Warn("設定がそろうまで、見回りをせずに待つ", "err", err)
			if !waitChange(ctx, db, rev, time.Second, dir) {
				return ctx.Err()
			}
			continue
		case err != nil:
			// 読みの失敗（ロックの待ちの上限、I/O の誤り、バックエンドが書く途中で落ちたときのジャーナル）は、
			// 設定が変わらなくても、間をおいて読み直す。バックエンドが次に開けば、ジャーナルは戻る。
			slog.Error("設定の DB を読めない、または中身から worker を作れない。待って読み直す", "err", err)
			if !wait(ctx, 5*time.Second, dir) {
				return ctx.Err()
			}
			continue
		}
		wctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			// 一覧を読み直す間隔ごとに、設定の版を確かめる。変われば、見回りを止める。
			if waitChange(wctx, db, rev, listInterval, "") {
				slog.Info("設定の DB が変わった。新しい設定で見回りを始め直す")
			}
			cancel()
		}()
		err = w.Run(wctx)
		cancel()
		<-done
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
}

// wait は、d だけ待つ。dir が空でなければ、待つあいだ、ハートビートを書く。ctx が終われば false。
func wait(ctx context.Context, d time.Duration, dir string) bool {
	if dir != "" {
		if err := worker.WriteHeartbeat(dir, time.Now()); err != nil {
			slog.Error("ハートビートを書けない", "err", err)
		}
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// waitChange は、設定の版が rev から変わるまで、every ごとに確かめて待つ。変われば true、ctx が終われば false。
func waitChange(ctx context.Context, db *settings.DB, rev int64, every time.Duration, dir string) bool {
	for {
		if !wait(ctx, every, dir) {
			return false
		}
		r, err := db.Revision(ctx)
		if err != nil {
			slog.Error("設定の DB を読めない。待って読み直す", "err", err)
			continue
		}
		if r != rev {
			return true
		}
	}
}

// buildWorker は、設定の DB の中身から、worker を作る。設定がそろっていなければ、誤りを返す。
func buildWorker(ctx context.Context, db *settings.DB, keys settings.Keys, dir string, gitEnv []string, st *store.Dir) (*worker.Worker, time.Duration, error) {
	c, err := db.Load(ctx, keys)
	if err != nil {
		return nil, 0, err
	}
	if err := c.Ready(); err != nil {
		return nil, 0, err
	}
	pub, err := keyringOf(c.RecordKeys)
	if err != nil {
		return nil, 0, err
	}
	pub = pub.VerifyOnly()
	interval := settings.Duration(c.Intervals.Patrol, worker.DefaultInterval)
	gitTimeout := settings.Duration(c.Intervals.GitTimeout, worker.DefaultGitTimeout)
	listInterval := settings.Duration(c.Intervals.List, worker.DefaultListInterval)
	recordInterval := settings.Duration(c.Intervals.Record, 0)
	// 反映かの判定は、バックエンドと同じ規則で、読むだけのトークンで行う。記録は、公開鍵だけで確かめる。
	var tokens github.TokenSource
	if c.GitForge == gitforgeGitHub && c.GitHub.AppID != "" {
		tokens = dbTokens{db: db, keys: keys}
	}
	fs, err := setupForge(c, pub, false, tokens)
	if err != nil {
		return nil, 0, err
	}
	// installation token は、切れるまで github.RefreshBefore より長く残っているときだけ使い回す。
	// git の操作がそれより長くかかると、ls-remote のあとの fetch を始めるときに切れているおそれがある。
	if fs.kind == gitforgeGitHub && gitTimeout >= github.RefreshBefore {
		return nil, 0, fmt.Errorf("git の操作の時間の上限（%v）は、GitHub を使う構成では %v より短くする", gitTimeout, github.RefreshBefore)
	}
	src := fs.reader
	fs.forge.Log = snapstore.RecordLog{Store: st}
	v := verifier{b: &backend.Backend{Forge: fs.forge, Snap: &snapstore.Adapter{Store: st}}, src: src, keys: pub}
	records := snapstore.LogSyncer{Source: src, Keys: pub, Store: st}
	w := worker.New(worker.Config{Dir: dir, GitEnv: gitEnv, GitCredentials: src.GitEnv, Interval: interval, GitTimeout: gitTimeout, ListInterval: listInterval,
		Verifier: v, Records: records, RecordInterval: recordInterval}, src, st)
	slog.Info("worker の設定を読んだ", "interval", interval, "git_timeout", gitTimeout)
	return w, listInterval, nil
}

// workerHealth は、worker のハートビートを確かめる（`logiforge worker-health`）。見回りの間隔は、設定の DB から読む。
func workerHealth() error {
	dir := os.Getenv("LOGIFORGE_WORKER_DIR")
	if dir == "" {
		return fmt.Errorf("環境変数 LOGIFORGE_WORKER_DIR がない")
	}
	interval := worker.DefaultInterval
	if path, err := env(envDB); err == nil {
		if db, err := settings.Open(path, true); err == nil {
			if c, err := db.Load(context.Background(), settings.Keys{}); err == nil {
				interval = settings.Duration(c.Intervals.Patrol, interval)
			}
			db.Close()
		}
	}
	// 設定がそろう前の worker は、既定の間隔でハートビートを書く。設定の間隔が長くても、その間隔で確かめれば足りる。
	return worker.CheckHeartbeat(dir, max(interval, worker.DefaultInterval), time.Now())
}

// runSettings は、`logiforge settings` のコマンドである。
//
//	logiforge settings load <ファイル>   JSON の設定を、設定の DB に書く（全体を置き換える。記録の鍵は足すだけ）
//	logiforge settings rekey --old-backend-key-file <ファイル> --old-shared-key-file <ファイル>
//	                                    すべての秘密を、新しい鍵（LOGIFORGE_BACKEND_KEY と LOGIFORGE_SHARED_KEY）で暗号化し直す
func runSettings(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("使い方：logiforge settings load <ファイル> | rekey --old-backend-key-file <ファイル> --old-shared-key-file <ファイル>")
	}
	db, keys, err := openSettings(true)
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "load":
		if len(args) != 2 {
			return errors.New("使い方：logiforge settings load <ファイル>（- なら標準入力）")
		}
		var r io.Reader = os.Stdin
		if args[1] != "-" {
			f, err := os.Open(args[1])
			if err != nil {
				return err
			}
			defer f.Close()
			r = f
		}
		var c settings.Config
		dec := json.NewDecoder(r)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return fmt.Errorf("設定の JSON を読めない：%w", err)
		}
		// worker に渡すトークンは、書けないものに限る（design/adapters/forgejo.md の「worker のトークンを確かめる」）。
		if c.GitForge == gitforgeForgejo && c.Forgejo.WorkerToken != "" {
			if err := forgejo.CheckWorkerToken(ctx, forgejo.Config{URL: c.Forgejo.URL, Org: c.Forgejo.Org}, c.Forgejo.WorkerLogin, c.Forgejo.WorkerToken); err != nil {
				return err
			}
		}
		return db.Save(ctx, keys, &c, "")
	case "rekey":
		fl := flag.NewFlagSet("rekey", flag.ContinueOnError)
		ob := fl.String("old-backend-key-file", "", "古いバックエンドの鍵のファイル")
		os_ := fl.String("old-shared-key-file", "", "古い共有の鍵のファイル")
		if err := fl.Parse(args[1:]); err != nil {
			return err
		}
		read := func(p string) (*settings.Key, error) {
			if p == "" {
				return nil, errors.New("--old-backend-key-file と --old-shared-key-file の両方が要る")
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			return settings.ParseKey(string(b))
		}
		var old settings.Keys
		if old.Backend, err = read(*ob); err != nil {
			return err
		}
		if old.Shared, err = read(*os_); err != nil {
			return err
		}
		n, err := db.Rekey(ctx, old, keys)
		if err != nil {
			return err
		}
		fmt.Printf("%d 個の秘密を、新しい鍵で暗号化し直した\n", n)
		return nil
	}
	return fmt.Errorf("logiforge settings に %q はない（load か rekey）", args[0])
}

// dbTokens は、worker が、バックエンドが作って設定の DB に置いた installation token を読むものである。
type dbTokens struct {
	db   *settings.DB
	keys settings.Keys
}

// Token は、gitForgeRef の owner のトークンを返す。ないか、切れるまでの残りが github.RefreshBefore より短ければ、誤りにする。
// そのとき、worker は、その repository の見回りを飛ばし、次の見回りで読み直す（design/adapters/github-app.md の「worker のトークン」）。
func (t dbTokens) Token(ctx context.Context, gitForgeRef string) (string, error) {
	git, _ := ports.SplitRef(gitForgeRef)
	owner, _, _ := strings.Cut(git, "/")
	wt, ok, err := t.db.WorkerTokenOf(ctx, t.keys, owner)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w: owner %s の worker のトークンを、バックエンドがまだ作っていない（バックエンドが動いているかを確かめる）", ports.ErrUnavailable, owner)
	}
	if left := time.Until(wt.ExpiresAt); left < github.RefreshBefore {
		return "", fmt.Errorf("%w: owner %s の worker のトークンが、%v で切れる。バックエンドが作り直すのを待つ（バックエンドが動いているかを確かめる）", ports.ErrUnavailable, owner, left.Round(time.Second))
	}
	return wt.Token, nil
}

// mintLoop は、worker のための installation token を作り直し続ける（design/adapters/github-app.md の「worker のトークン」）。
// 5 秒ごとに確かめ、ないか、切れるまでの残りが mintBefore より短いか、絞ったリポジトリが登録と違えば、作り直す。
func mintLoop(ctx context.Context, db *settings.DB, keys settings.Keys) {
	const base, max = 5 * time.Second, time.Minute
	delay := base
	var lastErr string
	var lastLog time.Time
	for {
		rev, _ := db.Revision(ctx)
		if err := mintWorkerTokens(ctx, db, keys, time.Now()); err != nil {
			// 失敗したら、1 分まで間隔を倍にする。ログは、誤りが変わったときと、10 分ごとにだけ出す
			// （App が owner に install されていないときなどに、5 秒ごとに GitHub に問い合わせ、ログを埋めないため）。
			if msg := err.Error(); msg != lastErr || time.Since(lastLog) >= 10*time.Minute {
				slog.Error("worker のトークンを作れない。間をおいて作り直す", "err", err, "retry", delay)
				lastErr, lastLog = msg, time.Now()
			}
			delay = min(delay*2, max)
		} else {
			delay, lastErr = base, ""
		}
		// 設定が変わったら（repository の登録など）、間隔を待たずに作り直す。
		for waited := time.Duration(0); waited < delay; waited += base {
			select {
			case <-ctx.Done():
				return
			case <-time.After(base):
			}
			if r, err := db.Revision(ctx); err == nil && r != rev {
				delay = base
				break
			}
		}
	}
}

// mintBefore は、worker のトークンを作り直す、切れるまでの残りの時間である。
// なぜ 40 分：installation token は 1 時間で切れ、worker は残りが 20 分（github.RefreshBefore）を切ると使わないので、
// そのあいだの 20 分に作り直せばよいから。バックエンドが止まっても、worker は、そこから 20〜40 分読める。
const mintBefore = 40 * time.Minute

func mintWorkerTokens(ctx context.Context, db *settings.DB, keys settings.Keys, now time.Time) error {
	c, err := db.Load(ctx, keys)
	if err != nil {
		return err
	}
	if c.GitForge != gitforgeGitHub || c.GitHub.AppID == "" || c.GitHub.AppPrivateKey == "" || len(c.Repositories) == 0 {
		return db.DeleteWorkerTokens(ctx, map[string]bool{})
	}
	rs, err := github.ParseRepositories(repoList(c.Repositories, false))
	if err != nil {
		return err
	}
	cfg := github.Config{APIURL: c.GitHub.APIURL, WebURL: c.GitHub.WebURL, Repositories: rs}
	app, err := github.NewApp(cfg, c.GitHub.AppID, []byte(c.GitHub.AppPrivateKey))
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	var errs []error
	for owner, names := range app.Owners() {
		keep[owner] = true
		t, ok, err := db.WorkerTokenOf(ctx, keys, owner)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if ok && t.ExpiresAt.Sub(now) > mintBefore && slices.Equal(t.Repositories, names) {
			continue
		}
		tok, exp, err := app.Mint(ctx, owner)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := db.PutWorkerToken(ctx, keys, settings.WorkerToken{Owner: owner, Token: tok, ExpiresAt: exp, Repositories: names}); err != nil {
			errs = append(errs, err)
			continue
		}
		slog.Info("worker のトークンを作った", "owner", owner, "expires_at", exp)
	}
	if err := db.DeleteWorkerTokens(ctx, keep); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
