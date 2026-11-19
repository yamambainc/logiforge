package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yamambainc/logiforge/internal/adapters/forgejo"
	"github.com/yamambainc/logiforge/internal/adapters/github"
	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/api"
	"github.com/yamambainc/logiforge/internal/backend"
	"github.com/yamambainc/logiforge/internal/ids"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/settings"
	"github.com/yamambainc/logiforge/internal/store"
)

// setupService は、初回のセットアップの中身である（design/settings/setup.md）。設定の DB に、そのつど置く。
type setupService struct {
	db    *settings.DB
	keys  settings.Keys
	store *store.Dir
}

var _ api.Setup = (*setupService)(nil)

func problem(code backend.Code, format string, a ...any) error {
	return &backend.Error{Code: code, Detail: fmt.Sprintf(format, a...)}
}

func (s *setupService) load(ctx context.Context) (*settings.Config, error) {
	return s.db.Load(ctx, s.keys)
}

func (s *setupService) State(ctx context.Context) (api.SetupState, error) {
	done, err := s.db.SetupDone(ctx)
	if err != nil {
		return api.SetupState{}, err
	}
	started, err := s.db.SetupStarted(ctx)
	if err != nil {
		return api.SetupState{}, err
	}
	c, err := s.load(ctx)
	if err != nil {
		return api.SetupState{}, err
	}
	st := stateOf(c, done)
	st.Started = started || done
	return st, nil
}

// stateOf は、設定から、次に進める段を決める。
func stateOf(c *settings.Config, done bool) api.SetupState {
	st := api.SetupState{Done: done}
	if c.GitForge != "" {
		g := c.GitForge
		st.GitForge = &g
	}
	if c.GitForge == gitforgeGitHub && c.GitHub.Slug != "" {
		st.GitHubApp = &api.SetupGitHubApp{Slug: c.GitHub.Slug, InstallURL: github.InstallURL(c.GitHub.WebURL, c.GitHub.Slug)}
	}
	if done {
		return st
	}
	step := "signin"
	switch {
	case c.GitForge == "":
		step = "gitforge"
	case c.GitForge == gitforgeGitHub && (c.GitHub.AppID == "" || c.GitHub.AppPrivateKey == "" || c.GitHub.ClientSecret == ""):
		step = "github_app"
	case c.GitForge == gitforgeForgejo && (c.Forgejo.URL == "" || c.Forgejo.Org == "" || c.Forgejo.WorkerToken == "" || c.Forgejo.OAuthClientSecret == ""):
		step = "forgejo"
	}
	st.Step = &step
	return st
}

// inProgress は、ほかのブラウザでセットアップが進んでいるときの誤りである。
var inProgress = problem(backend.CodeSetupInProgress, "セットアップは、別のブラウザで進んでいる。最初からやり直すときは、設定の DB を消して起動し直す")

func (s *setupService) Authorize(ctx context.Context, token string) error {
	done, err := s.db.SetupDone(ctx)
	if err != nil {
		return err
	}
	if done {
		return problem(backend.CodeSetupDone, "セットアップは、もう終わっている")
	}
	started, err := s.db.SetupStarted(ctx)
	if err != nil {
		return err
	}
	if !started {
		return problem(backend.CodeValidationFailed, "セットアップが始まっていない。先に構成を選ぶ")
	}
	ok, err := s.db.CheckSetupToken(ctx, token)
	if err != nil {
		return err
	}
	if !ok {
		return inProgress
	}
	return nil
}

func (s *setupService) Start(ctx context.Context, kind, webuiURL string) (api.SetupState, error) {
	v, err := gitforgeValues(kind, webuiURL)
	if err != nil {
		return api.SetupState{}, err
	}
	tok, err := s.db.StartSetup(ctx, s.keys, v)
	switch {
	case errors.Is(err, settings.ErrSetupDone):
		return api.SetupState{}, problem(backend.CodeSetupDone, "セットアップは、もう終わっている")
	case errors.Is(err, settings.ErrSetupStarted):
		return api.SetupState{}, inProgress
	case err != nil:
		return api.SetupState{}, err
	}
	st, err := s.State(ctx)
	st.SetupToken = tok
	return st, err
}

// gitforgeValues は、構成の段の入力を確かめ、設定の DB に置く値にする。
func gitforgeValues(kind, webuiURL string) (map[string]string, error) {
	if kind != gitforgeGitHub && kind != gitforgeForgejo {
		return nil, problem(backend.CodeValidationFailed, "構成は github か forgejo")
	}
	v := map[string]string{"gitforge": kind}
	if webuiURL != "" {
		if err := checkWebUIURL(webuiURL); err != nil {
			return nil, err
		}
		v["webui_url"] = strings.TrimRight(webuiURL, "/")
	}
	return v, nil
}

func (s *setupService) SetGitForge(ctx context.Context, kind, webuiURL string) (api.SetupState, error) {
	v, err := gitforgeValues(kind, webuiURL)
	if err != nil {
		return api.SetupState{}, err
	}
	if err := s.db.Put(ctx, s.keys, v, ""); err != nil {
		return api.SetupState{}, err
	}
	return s.State(ctx)
}

func checkWebUIURL(v string) error {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return problem(backend.CodeValidationFailed, "WebUI の URL は、http か https の URL にする：%q", v)
	}
	return nil
}

func (s *setupService) requireKind(c *settings.Config, kind string) error {
	if c.GitForge != kind {
		return problem(backend.CodeValidationFailed, "構成が %s でない。先に構成を選ぶ", kind)
	}
	return nil
}

func (s *setupService) GitHubManifest(ctx context.Context, in api.SetupManifestInput) (api.SetupManifest, error) {
	c, err := s.load(ctx)
	if err != nil {
		return api.SetupManifest{}, err
	}
	if err := s.requireKind(c, gitforgeGitHub); err != nil {
		return api.SetupManifest{}, err
	}
	if in.OwnerKind != "organization" && in.OwnerKind != "user" {
		return api.SetupManifest{}, problem(backend.CodeValidationFailed, "owner_kind は organization か user")
	}
	if err := checkWebUIURL(in.WebUIURL); err != nil {
		return api.SetupManifest{}, err
	}
	m, post, err := github.Manifest(github.ManifestInput{Owner: in.Owner, Organization: in.OwnerKind == "organization",
		WebUIURL: in.WebUIURL, Public: in.Public, WebURL: c.GitHub.WebURL})
	if err != nil {
		return api.SetupManifest{}, problem(backend.CodeValidationFailed, "%v", err)
	}
	if err := s.db.Put(ctx, s.keys, map[string]string{"webui_url": strings.TrimRight(in.WebUIURL, "/")}, ""); err != nil {
		return api.SetupManifest{}, err
	}
	return api.SetupManifest{Manifest: m, PostURL: post}, nil
}

func (s *setupService) GitHubConversion(ctx context.Context, code string) (api.SetupGitHubApp, error) {
	c, err := s.load(ctx)
	if err != nil {
		return api.SetupGitHubApp{}, err
	}
	if err := s.requireKind(c, gitforgeGitHub); err != nil {
		return api.SetupGitHubApp{}, err
	}
	app, err := github.ConvertManifest(ctx, c.GitHub.APIURL, nil, code)
	if errors.Is(err, github.ErrManifestCode) {
		return api.SetupGitHubApp{}, problem(backend.CodeValidationFailed, "%v", err)
	}
	if err != nil {
		return api.SetupGitHubApp{}, problem(backend.CodeUnavailable, "GitHub で App の秘密を受け取れない：%v", err)
	}
	if err := s.db.Put(ctx, s.keys, map[string]string{
		"github.app_id": app.ID, "github.slug": app.Slug, "github.client_id": app.ClientID,
		"github.client_secret": app.ClientSecret, "github.app_private_key": app.PrivateKeyPEM,
	}, ""); err != nil {
		return api.SetupGitHubApp{}, err
	}
	slog.Info("セットアップで GitHub App を作った", "app_id", app.ID, "slug", app.Slug)
	return api.SetupGitHubApp{Slug: app.Slug, InstallURL: github.InstallURL(c.GitHub.WebURL, app.Slug)}, nil
}

func (s *setupService) SetRepository(ctx context.Context, r api.SetupRepository) (api.SetupState, error) {
	c, err := s.load(ctx)
	if err != nil {
		return api.SetupState{}, err
	}
	if err := s.requireKind(c, gitforgeGitHub); err != nil {
		return api.SetupState{}, err
	}
	if c.GitHub.AppID == "" || c.GitHub.AppPrivateKey == "" {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "先に GitHub App を作る")
	}
	if !ids.RepositoryNamePattern.MatchString(r.Name) {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "repository の名前は、小文字の英数字とハイフンで、63 文字以下")
	}
	if err := checkGitHubRepository(ctx, c, nil, settings.Repository{Name: r.Name, GitForgeRef: r.GitForgeRef, Directory: r.Directory}); err != nil {
		return api.SetupState{}, err
	}
	if err := s.db.SetFirstRepository(ctx, settings.Repository{Name: r.Name, GitForgeRef: r.GitForgeRef, Directory: r.Directory}, ""); err != nil {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "%v", err)
	}
	return s.State(ctx)
}

func (s *setupService) SetForgejo(ctx context.Context, in api.SetupForgejo) (api.SetupState, error) {
	c, err := s.load(ctx)
	if err != nil {
		return api.SetupState{}, err
	}
	if err := s.requireKind(c, gitforgeForgejo); err != nil {
		return api.SetupState{}, err
	}
	for n, v := range map[string]string{"url": in.URL, "organization": in.Organization, "client_id": in.ClientID,
		"client_secret": in.ClientSecret, "worker_user": in.WorkerUser, "worker_token": in.WorkerToken} {
		if strings.TrimSpace(v) == "" {
			return api.SetupState{}, problem(backend.CodeValidationFailed, "%s がない", n)
		}
	}
	for _, u := range []string{in.URL, in.PublicURL} {
		if u == "" {
			continue
		}
		if err := checkWebUIURL(u); err != nil {
			return api.SetupState{}, problem(backend.CodeValidationFailed, "Forgejo の URL は、http か https の URL にする：%q", u)
		}
	}
	url := strings.TrimRight(in.URL, "/")
	if err := forgejo.CheckWorkerToken(ctx, forgejo.Config{URL: url, Org: in.Organization}, in.WorkerUser, in.WorkerToken); err != nil {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "worker のトークン：%v", err)
	}
	if err := s.db.Put(ctx, s.keys, map[string]string{
		"forgejo.url": url, "forgejo.public_url": strings.TrimRight(in.PublicURL, "/"), "forgejo.org": in.Organization,
		"forgejo.oauth_client_id": in.ClientID, "forgejo.oauth_client_secret": in.ClientSecret,
		"forgejo.worker_login": in.WorkerUser, "forgejo.worker_token": in.WorkerToken,
	}, ""); err != nil {
		return api.SetupState{}, err
	}
	return s.State(ctx)
}

func (s *setupService) Finish(ctx context.Context, token string) (api.SetupState, error) {
	c, err := s.load(ctx)
	if err != nil {
		return api.SetupState{}, err
	}
	st := stateOf(c, false)
	if *st.Step != "signin" {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "前の段が終わっていない（%s）", *st.Step)
	}
	if c.GitForge == gitforgeGitHub && len(c.Repositories) == 0 {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "先に最初の repository を登録する")
	}
	if c.WebUIURL == "" {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "WebUI の URL がない。構成を選び直す")
	}
	// 記録の鍵は、まだない。主体を確かめるだけなので、空の鍵で作る。
	fs, err := setupForge(c, pullreq.Keyring{}, true, nil)
	if err != nil {
		return api.SetupState{}, err
	}
	fresh := ports.WithFresh(ctx)
	p, err := fs.auth.Authenticate(fresh, token)
	if errors.Is(err, ports.ErrUnauthenticated) {
		return api.SetupState{}, problem(backend.CodeUnauthenticated, "トークンが、Logiforge のサインインのものでない")
	}
	if err != nil {
		return api.SetupState{}, err
	}
	if !p.WebUI {
		return api.SetupState{}, problem(backend.CodeUnauthenticated, "トークンが、Logiforge のサインインのものでない")
	}
	if p.Kind != ports.PrincipalUser {
		return api.SetupState{}, problem(backend.CodeForbidden, "%s は、利用者（user）でない。%s", p.Login, notUserHint(c))
	}
	if c.GitForge == gitforgeGitHub {
		ok, err := fs.auth.CanWrite(fresh, token, c.Repositories[0].Name)
		if err != nil {
			return api.SetupState{}, err
		}
		if !ok {
			return api.SetupState{}, problem(backend.CodeForbidden, "%s は、%s に Write がない", p.Login, c.Repositories[0].GitForgeRef)
		}
	}
	if used, err := storeUsed(s.store); err != nil {
		return api.SetupState{}, err
	} else if used {
		return api.SetupState{}, problem(backend.CodeValidationFailed, "置き場に、すでに取り込んだ repository がある。初回のセットアップは、空の置き場で行う")
	}
	if err := s.db.FinishSetup(ctx, s.keys, p.Login); errors.Is(err, settings.ErrSetupDone) {
		return api.SetupState{}, problem(backend.CodeSetupDone, "セットアップは、もう終わっている")
	} else if err != nil {
		return api.SetupState{}, err
	}
	slog.Info("セットアップを終えた", "user", p.Login, "gitforge", c.GitForge)
	return s.State(ctx)
}

func notUserHint(c *settings.Config) string {
	if c.GitForge == gitforgeGitHub {
		return "登録した GitHub リポジトリに Write がなければならない"
	}
	return "organization の、コードに書ける team にいなければならない"
}

// storeUsed は、置き場に、worker がすでに取り込んだものがあるかを返す（repositories の一覧か、keep/ の下）。
func storeUsed(st *store.Dir) (bool, error) {
	if _, err := st.Get(context.Background(), store.RepositoriesKey); err == nil {
		return true, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	es, err := os.ReadDir(filepath.Join(st.Root, strings.TrimSuffix(store.KeepPrefix, "/")))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(es) > 0, nil
}

// logSetupPending は、セットアップが終わっていなければ、そのことをログに出す。
func logSetupPending(ctx context.Context, db *settings.DB) error {
	done, err := db.SetupDone(ctx)
	if err != nil || done {
		return err
	}
	slog.Warn("初回のセットアップが終わっていない。WebUI の /setup を開いて進める")
	return nil
}

// checkGitHubRepository は、GitHub を使う構成で、repository r を、others（登録済みのもの）に足してよいかと、
// GitHub App の installation token で、その GitHub リポジトリを読めるかを確かめる。読めなければ、install の URL を detail に書く。
func checkGitHubRepository(ctx context.Context, c *settings.Config, others []settings.Repository, r settings.Repository) error {
	if c.GitHub.AppID == "" || c.GitHub.AppPrivateKey == "" {
		return problem(backend.CodeValidationFailed, "先に GitHub App を作る")
	}
	all, err := github.ParseRepositories(repoList(append(append([]settings.Repository(nil), others...), r), false))
	if err != nil {
		return problem(backend.CodeValidationFailed, "%v", err)
	}
	app, err := github.NewApp(github.Config{APIURL: c.GitHub.APIURL, WebURL: c.GitHub.WebURL, Repositories: all}, c.GitHub.AppID, []byte(c.GitHub.AppPrivateKey))
	if err != nil {
		return problem(backend.CodeValidationFailed, "%v", err)
	}
	if err := app.CanReadRepository(ctx, r.GitForgeRef); err != nil {
		return problem(backend.CodeValidationFailed, "GitHub App で %s を読めない。App を install し、このリポジトリを選ぶ：%s（%v）",
			r.GitForgeRef, github.InstallURL(c.GitHub.WebURL, c.GitHub.Slug), err)
	}
	return nil
}
