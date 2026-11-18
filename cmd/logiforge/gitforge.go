package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/forgejo"
	"github.com/yamambainc/logiforge/internal/adapters/github"
	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/settings"
)

// 構成。設定の DB で 1 つに決める（design/adapters/README.md の「組み合わせ」）。
const (
	gitforgeForgejo = settings.GitForgeForgejo
	gitforgeGitHub  = settings.GitForgeGitHub
)

// readSource は、git を読む URL と資格情報と、記録を読むための読み方である。worker と、バックエンドの作業用の複製が使う。
// 読むのは、いつも、読むだけのトークン（GitHub では、読むだけの App の installation token。public のリポジトリでは、トークンなしでもよい）である。
type readSource interface {
	ListRepositories(ctx context.Context) ([]ports.Repository, error)
	GitForgeRef(ctx context.Context, name string) (string, error)
	GitURL(gitForgeRef string) string
	// GitEnv と ReadToken は、git の操作と REST の呼び出しのたびに呼ぶ（installation token は切れるので）。
	GitEnv(ctx context.Context, gitForgeRef string) ([]string, error)
	ReadToken(ctx context.Context, gitForgeRef string) (string, error)
	RepoID(ctx context.Context, gitForgeRef string) (string, error)
	CommentsSince(ctx context.Context, gitForgeRef string, since time.Time) ([]pullreq.Comment, error)
}

// forgeSetup は、構成から作った、GitForge のアダプタと認証と、読むだけの側である。
type forgeSetup struct {
	forge *pullreq.Adapter
	auth  ports.Authenticator
	// reader は、読むだけのトークンで git と記録を読む。GitForge のアダプタを読むだけで呼ぶときも、reader.ReadToken を使う。
	reader readSource
	// webURL は、WebUI のサインイン（OAuth）に使う GitForge の URL。kind は、構成（forgejo か github）。
	webURL, kind string
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// repoList は、設定の DB の repository の一覧を、アダプタの形（名前=owner/name[:パス],…）にする。dirOnly なら、ディレクトリの repository だけ。
func repoList(rs []settings.Repository, dirOnly bool) string {
	var parts []string
	for _, r := range rs {
		if dirOnly && r.Directory == "" {
			continue
		}
		ref := r.GitForgeRef
		if r.Directory != "" {
			ref += ":" + r.Directory
		}
		parts = append(parts, r.Name+"="+ref)
	}
	return strings.Join(parts, ",")
}

// setupForge は、設定の DB の中身から、GitForge のアダプタを作る。keys は、バックエンドでは秘密鍵、worker では公開鍵だけ。
// serve が true なら、バックエンドの設定（user の主体の決め方）も使う（design/settings/README.md の「設定の DB に置くもの」）。
func setupForge(c *settings.Config, keys pullreq.Keyring, serve bool) (forgeSetup, error) {
	if c.GitForge == gitforgeGitHub {
		return setupGitHub(c, keys, serve)
	}
	f := c.Forgejo
	if f.URL == "" || f.Org == "" || f.WorkerLogin == "" || f.WorkerToken == "" {
		return forgeSetup{}, fmt.Errorf("設定の DB に、Forgejo の URL、organization、worker のユーザーとトークンがそろっていない")
	}
	dirs, err := ports.ParseDirectories(repoList(c.Repositories, true))
	if err != nil {
		return forgeSetup{}, fmt.Errorf("ディレクトリの repository：%w", err)
	}
	team := ""
	if serve {
		team = f.UserTeam
	}
	forge, auth := forgejo.New(forgejo.Config{URL: f.URL, Org: f.Org, UserTeam: team, WorkerLogin: f.WorkerLogin, Directories: dirs}, keys)
	reader := forgejo.NewWorkerSource(forgejo.Config{URL: f.URL, Org: f.Org, Directories: dirs}, f.WorkerLogin, f.WorkerToken)
	return forgeSetup{forge: forge, auth: auth, reader: reader, webURL: f.URL, kind: gitforgeForgejo}, nil
}

func setupGitHub(c *settings.Config, keys pullreq.Keyring, serve bool) (forgeSetup, error) {
	rs, err := github.ParseRepositories(repoList(c.Repositories, false))
	if err != nil {
		return forgeSetup{}, err
	}
	g := c.GitHub
	cfg := github.Config{APIURL: g.APIURL, WebURL: g.WebURL, Repositories: rs}
	if serve {
		cfg.UserLogins = splitList(g.UserLogins)
		cfg.UserTeams = splitList(g.UserTeams)
		// 主体のトークンが Logiforge のアプリのものかを、WebUI のサインインと同じ資格情報で確かめる。なければ、user の主体はいない。
		cfg.ClientID, cfg.ClientSecret = g.ClientID, g.ClientSecret
	}
	forge, auth, err := github.New(cfg, keys)
	if err != nil {
		return forgeSetup{}, err
	}
	app, err := githubApp(cfg, g)
	if err != nil {
		return forgeSetup{}, err
	}
	reader, err := github.NewWorkerSource(cfg, app)
	if err != nil {
		return forgeSetup{}, err
	}
	web := cfg.WebURL
	if web == "" {
		web = github.DefaultWebURL
	}
	return forgeSetup{forge: forge, auth: auth, reader: reader, webURL: strings.TrimRight(web, "/"), kind: gitforgeGitHub}, nil
}

// githubApp は、GitHub App を、設定の DB の App の ID と秘密鍵から作る。どちらもなければ nil（トークンなしで読む）。
func githubApp(cfg github.Config, g settings.GitHubConfig) (*github.App, error) {
	if g.AppID == "" && g.AppPrivateKey == "" {
		slog.Warn("GitHub App の ID がない。トークンなしで読む（public のリポジトリだけ。REST の上限は 1 時間に 60 回で、続けて使うには足りない）")
		return nil, nil
	}
	if g.AppID == "" || g.AppPrivateKey == "" {
		return nil, fmt.Errorf("GitHub App には、ID と秘密鍵の両方が要る")
	}
	return github.NewApp(cfg, g.AppID, []byte(g.AppPrivateKey))
}
