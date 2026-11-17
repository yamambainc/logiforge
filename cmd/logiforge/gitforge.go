package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/forgejo"
	"github.com/yamambainc/logiforge/internal/adapters/github"
	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
)

// 構成（LOGIFORGE_GITFORGE）。サーバの設定で 1 つに決める（design/adapters/README.md の「組み合わせ」）。
const (
	gitforgeForgejo = "forgejo"
	gitforgeGitHub  = "github"
)

func gitforgeKind() (string, error) {
	switch v := os.Getenv("LOGIFORGE_GITFORGE"); v {
	case "", gitforgeForgejo:
		return gitforgeForgejo, nil
	case gitforgeGitHub:
		return gitforgeGitHub, nil
	default:
		return "", fmt.Errorf("LOGIFORGE_GITFORGE は forgejo か github：%q", v)
	}
}

// readSource は、git を読む URL と資格情報と、記録を読むための読み方である。worker と、バックエンドの作業用の複製が使う。
// 読むのは、いつも、読むだけのトークン（GitHub の public のリポジトリでは、トークンなし）である。
type readSource interface {
	ListRepositories(ctx context.Context) ([]ports.Repository, error)
	GitForgeRef(ctx context.Context, name string) (string, error)
	GitURL(gitForgeRef string) string
	GitEnv() []string
	RepoID(ctx context.Context, gitForgeRef string) (string, error)
	CommentsSince(ctx context.Context, gitForgeRef string, since time.Time) ([]pullreq.Comment, error)
}

// forgeSetup は、構成から作った、GitForge のアダプタと認証と、読むだけの側である。
type forgeSetup struct {
	forge *pullreq.Adapter
	auth  ports.Authenticator
	// reader は、読むだけのトークンで git と記録を読む。readToken は、そのトークン（GitForge のアダプタを読むだけで呼ぶときに使う）。
	reader    readSource
	readToken string
	// webURL は、WebUI のサインイン（OAuth）に使う GitForge の URL。kind は、構成（forgejo か github）。
	webURL, kind string
}

// optEnv は、なくてもよい環境変数を読む（name_FILE も読む）。なければ空。
func optEnv(name string) (string, error) {
	if os.Getenv(name) == "" && os.Getenv(name+"_FILE") == "" {
		return "", nil
	}
	return env(name)
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

// setupForge は、構成ごとの環境変数から、GitForge のアダプタを作る。keys は、バックエンドでは秘密鍵、worker では公開鍵だけ。
// serve が true なら、バックエンドの設定（user の主体の決め方）も読む。
//
// Forgejo：LOGIFORGE_FORGEJO_URL、LOGIFORGE_FORGEJO_ORG、LOGIFORGE_WORKER_LOGIN、LOGIFORGE_WORKER_TOKEN（読むだけのユーザー）、
// バックエンドは LOGIFORGE_USER_TEAM も。
//
// GitHub（design/adapters/github.md）：
//
//	LOGIFORGE_GITHUB_REPOSITORIES  repository の名前と owner/name の組（名前=owner/name,…）。ディレクトリの repository は 名前=owner/name:パス
//
// Forgejo のディレクトリの repository は、LOGIFORGE_DIRECTORIES（名前=owner/name:パス,…）に書く。
//
//	LOGIFORGE_GITHUB_READ_TOKEN    読むだけのトークン（Contents、Issues、Pull requests の Read）。public のリポジトリだけなら、なくてもよい
//	LOGIFORGE_GITHUB_API_URL、LOGIFORGE_GITHUB_URL  REST の URL と、画面と git の URL（既定は github.com）
//	バックエンド：LOGIFORGE_USER_LOGINS、LOGIFORGE_USER_TEAM（user とみなすアカウント名と team（org/slug）。「,」で並べる）、
//	            LOGIFORGE_OAUTH_CLIENT_ID、LOGIFORGE_OAUTH_CLIENT_SECRET（Logiforge の OAuth App。トークンがそのものかを確かめる）
func setupForge(keys pullreq.Keyring, serve bool) (forgeSetup, error) {
	kind, err := gitforgeKind()
	if err != nil {
		return forgeSetup{}, err
	}
	if kind == gitforgeGitHub {
		return setupGitHub(keys, serve)
	}
	var url, org, login, token, team string
	need := map[string]*string{"LOGIFORGE_FORGEJO_URL": &url, "LOGIFORGE_FORGEJO_ORG": &org, "LOGIFORGE_WORKER_LOGIN": &login, "LOGIFORGE_WORKER_TOKEN": &token}
	if serve {
		need["LOGIFORGE_USER_TEAM"] = &team
	}
	if err := envs(need); err != nil {
		return forgeSetup{}, err
	}
	// ディレクトリの repository（LOGIFORGE_DIRECTORIES=名前=owner/name:パス,…。design/directory/README.md の「登録」）。
	dirs, err := ports.ParseDirectories(os.Getenv("LOGIFORGE_DIRECTORIES"))
	if err != nil {
		return forgeSetup{}, fmt.Errorf("LOGIFORGE_DIRECTORIES: %w", err)
	}
	forge, auth := forgejo.New(forgejo.Config{URL: url, Org: org, UserTeam: team, WorkerLogin: login, Directories: dirs}, keys)
	reader := forgejo.NewWorkerSource(forgejo.Config{URL: url, Org: org, Directories: dirs}, login, token)
	return forgeSetup{forge: forge, auth: auth, reader: reader, readToken: token, webURL: url, kind: gitforgeForgejo}, nil
}

func setupGitHub(keys pullreq.Keyring, serve bool) (forgeSetup, error) {
	var repos string
	if err := envs(map[string]*string{"LOGIFORGE_GITHUB_REPOSITORIES": &repos}); err != nil {
		return forgeSetup{}, err
	}
	rs, err := github.ParseRepositories(repos)
	if err != nil {
		return forgeSetup{}, err
	}
	token, err := optEnv("LOGIFORGE_GITHUB_READ_TOKEN")
	if err != nil {
		return forgeSetup{}, err
	}
	if token == "" {
		// トークンなしの REST の上限は 1 時間に 60 回で、記録を読むだけで超える（design/adapters/github.md の「アカウントとトークン」）。
		slog.Warn("LOGIFORGE_GITHUB_READ_TOKEN がない。トークンなしで読む（public のリポジトリだけ。REST の上限は 1 時間に 60 回で、続けて使うには足りない）")
	}
	cfg := github.Config{APIURL: os.Getenv("LOGIFORGE_GITHUB_API_URL"), WebURL: os.Getenv("LOGIFORGE_GITHUB_URL"), Repositories: rs}
	if serve {
		cfg.UserLogins = splitList(os.Getenv("LOGIFORGE_USER_LOGINS"))
		cfg.UserTeams = splitList(os.Getenv("LOGIFORGE_USER_TEAM"))
		// 主体のトークンが Logiforge の OAuth App のものかを、WebUI のサインインと同じ資格情報で確かめる。
		// サインインを設定しなければ、user の主体はいない。
		if cfg.ClientID, err = optEnv("LOGIFORGE_OAUTH_CLIENT_ID"); err != nil {
			return forgeSetup{}, err
		}
		if cfg.ClientID != "" {
			if err := envs(map[string]*string{"LOGIFORGE_OAUTH_CLIENT_SECRET": &cfg.ClientSecret}); err != nil {
				return forgeSetup{}, err
			}
		}
	}
	forge, auth, err := github.New(cfg, keys)
	if err != nil {
		return forgeSetup{}, err
	}
	reader, err := github.NewWorkerSource(cfg, token)
	if err != nil {
		return forgeSetup{}, err
	}
	web := cfg.WebURL
	if web == "" {
		web = github.DefaultWebURL
	}
	return forgeSetup{forge: forge, auth: auth, reader: reader, readToken: token, webURL: strings.TrimRight(web, "/"), kind: gitforgeGitHub}, nil
}
