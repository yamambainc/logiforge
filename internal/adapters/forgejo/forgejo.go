package forgejo

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
)

// New は、Forgejo の GitForge アダプタと認証のアダプタを作る。
func New(cfg Config, keys pullreq.Keyring) (*pullreq.Adapter, *Auth) {
	c := &client{cfg: cfg}
	return &pullreq.Adapter{Repos: repos{c, keys}, API: api{c}, Keys: keys}, &Auth{c}
}

// repos は、organization の repository の列挙と用意である。
type repos struct {
	*client
	// keys は、用意のコミットの署名に使う。
	keys pullreq.Keyring
}

func (r repos) w(ctx context.Context, method, path string, in, out any) error {
	return r.do(ctx, ports.TokenFrom(ctx), method, path, in, out)
}

func (r repos) ListRepositories(ctx context.Context) ([]ports.Repository, error) {
	return r.list(ctx, ports.TokenFrom(ctx))
}

func (r repos) list(ctx context.Context, token string) ([]ports.Repository, error) {
	type repoJSON struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	}
	rs, err := list[repoJSON](ctx, r.client, token, "/orgs/"+url.PathEscape(r.cfg.Org)+"/repos")
	if err != nil {
		return nil, err
	}
	hosts := map[string]bool{}
	names := map[string]bool{}
	for _, d := range r.cfg.Directories {
		git, _ := ports.SplitRef(d.GitForgeRef)
		hosts[strings.ToLower(git)], names[d.Name] = true, true
	}
	out := make([]ports.Repository, 0, len(rs)+len(r.cfg.Directories))
	for _, x := range rs {
		// ディレクトリを置く git リポジトリ（プロジェクトのリポジトリ）と、ディレクトリと同じ名前のものは、数えない。
		if hosts[strings.ToLower(x.FullName)] || names[x.Name] {
			continue
		}
		out = append(out, ports.Repository{Name: x.Name, GitForgeRef: x.FullName})
	}
	out = append(out, r.cfg.Directories...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GitForgeRef は、名前から gitforge_ref（<organization>/<名前>）を求める。repository がまだなくてもよい。
func (r repos) GitForgeRef(_ context.Context, name string) (string, error) {
	for _, d := range r.cfg.Directories {
		if d.Name == name {
			return d.GitForgeRef, nil
		}
	}
	return r.cfg.Org + "/" + name, nil
}

// GitURL は、クライアントが clone と push に使う git の URL を返す。
func (r repos) GitURL(gitForgeRef string) string {
	return strings.TrimRight(r.cfg.URL, "/") + "/" + gitRef(gitForgeRef) + ".git"
}

// MainHead は、main の先頭のコミットの SHA を返す。
func (r repos) MainHead(ctx context.Context, gitForgeRef string) (string, error) {
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return "", err
	}
	var br struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := r.w(ctx, http.MethodGet, rp+"/branches/main", nil, &br); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return "", ports.ErrNotFound
		}
		return "", err
	}
	return br.Commit.ID, nil
}

// SetupRepository は、repository を用意する（design/adapters/forgejo.md の「repository の用意」）。
//  1. repository がなければ作る（auto_init なし）。
//  2. main がなければ、files を 1 つのコミットで main に置く。
//  3. main を守る設定がなければ作る。
//
// 途中で止まっても、呼び直せば続きから行う。main が呼ぶ前からあったときは、守る設定を確かめたあとに ErrMainExists を返す。
func (r repos) SetupRepository(ctx context.Context, gitForgeRef string, files []ports.File, message string) error {
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return err
	}
	if _, dir := ports.SplitRef(gitForgeRef); dir != "" {
		return r.setupDirectory(ctx, gitForgeRef, rp, dir, files, message)
	}
	owner, name, _ := strings.Cut(gitForgeRef, "/")
	if err := r.w(ctx, http.MethodGet, rp, nil, nil); statusOf(err) == http.StatusNotFound {
		body := map[string]any{"name": name, "auto_init": false, "default_branch": "main", "private": true}
		if err := r.w(ctx, http.MethodPost, "/orgs/"+url.PathEscape(owner)+"/repos", body, nil); err != nil && statusOf(err) != http.StatusConflict {
			return err
		}
	} else if err != nil {
		return err
	}

	mainExisted := true
	if err := r.w(ctx, http.MethodGet, rp+"/branches/main", nil, nil); statusOf(err) == http.StatusNotFound {
		mainExisted = false
		// 根のコミットにも、署名を付ける。worker は、それで用意のコミットと決める（design/backend/verify.md）。
		repoID, err := api{r.client}.RepoID(ctx, gitForgeRef)
		if err != nil {
			return err
		}
		if message, err = r.keys.InitMessage(message, pullreq.InitScope{Kind: ports.InitRoot, RepoID: repoID}, files); err != nil {
			return err
		}
		var ops []map[string]string
		for _, f := range files {
			ops = append(ops, map[string]string{"operation": "create", "path": f.Path, "content": base64.StdEncoding.EncodeToString(f.Content)})
		}
		if err := r.w(ctx, http.MethodPost, rp+"/contents", map[string]any{"branch": "main", "message": message, "files": ops}, nil); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if err := r.w(ctx, http.MethodGet, rp+"/branch_protections/main", nil, nil); statusOf(err) == http.StatusNotFound {
		body := map[string]any{
			"rule_name": "main", "enable_push": false,
			// merge できる人は絞らない。reviewer が merge する（design/adapters/forgejo.md の「repository の用意」）。
			"enable_merge_whitelist":   false,
			"block_on_outdated_branch": false,
		}
		if err := r.w(ctx, http.MethodPost, rp+"/branch_protections", body, nil); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if mainExisted {
		return ports.ErrMainExists
	}
	return nil
}

// Auth は、Forgejo のトークンで主体を確かめる（design/adapters/forgejo.md の「認証」）。結果を保持しない。
type Auth struct{ c *client }

var _ ports.Authenticator = (*Auth)(nil)

func (a *Auth) Authenticate(ctx context.Context, token string) (ports.Principal, error) {
	var u user
	if err := a.c.do(ctx, token, http.MethodGet, "/user", nil, &u); err != nil {
		if s := statusOf(err); s == http.StatusUnauthorized || s == http.StatusForbidden {
			return ports.Principal{}, ports.ErrUnauthenticated
		}
		return ports.Principal{}, err
	}
	// worker のユーザーは、Logiforge の API の主体ではない。
	if a.c.cfg.WorkerLogin != "" && u.Login == a.c.cfg.WorkerLogin {
		return ports.Principal{}, ports.ErrUnauthenticated
	}
	type teamJSON struct {
		Name         string `json:"name"`
		Organization struct {
			Username string `json:"username"`
		} `json:"organization"`
	}
	teams, err := list[teamJSON](ctx, a.c, token, "/user/teams")
	if err != nil {
		return ports.Principal{}, err
	}
	member, isUser := false, false
	for _, t := range teams {
		if t.Organization.Username != a.c.cfg.Org {
			continue
		}
		member = true
		if t.Name == a.c.cfg.UserTeam {
			isUser = true
		}
	}
	if !member {
		return ports.Principal{}, ports.ErrUnauthenticated
	}
	kind := ports.PrincipalClient
	if isUser {
		kind = ports.PrincipalUser
	}
	return ports.Principal{Login: u.Login, Kind: kind}, nil
}

// CanRead は、主体が repository を読めるかを返す。Forgejo を使う構成では、organization のすべての主体が、
// organization のすべての repository を読める（design/adapters/forgejo.md の「認証」）。主体が organization にいることは
// Authenticate が確かめているので、ここでは主体のトークンで Forgejo を呼ばない。
// なぜ：利用者のトークンに read:repository を求めずに済むから（WebUI のトークンは読むだけの最小のスコープにする。design/webui/auth.md）。
// repository があるかは、呼ぶ側がスナップショットで確かめる。
func (a *Auth) CanRead(_ context.Context, _ string, repo string) (bool, error) {
	if _, err := repoPath(a.c.cfg.Org + "/" + repo); err != nil {
		return false, err
	}
	return true, nil
}

// setupDirectory は、ディレクトリの repository の用意のコミットを、利用者のトークンで、プロジェクトのリポジトリの main に置く
// （design/directory/history.md の「用意のコミット」）。プロジェクトのリポジトリは作らない。main を守る設定も変えない。
// ディレクトリがすでにあれば、ErrMainExists を返す。
func (r repos) setupDirectory(ctx context.Context, ref, rp, dir string, files []ports.File, message string) error {
	if err := r.w(ctx, http.MethodGet, rp+"/branches/main", nil, nil); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return fmt.Errorf("%w: プロジェクトのリポジトリ %s か、その main がない", ports.ErrNotFound, gitRef(ref))
		}
		return err
	}
	if err := r.w(ctx, http.MethodGet, rp+"/contents/"+escapePath(dir)+"?ref=main", nil, nil); err == nil {
		return ports.ErrMainExists
	} else if statusOf(err) != http.StatusNotFound {
		return err
	}
	repoID, err := api{r.client}.RepoID(ctx, ref)
	if err != nil {
		return err
	}
	if message, err = r.keys.InitMessage(message, pullreq.InitScope{Kind: ports.InitDirectory, RepoID: repoID, Directory: dir, MigratedFrom: migratedFrom(message)}, files); err != nil {
		return err
	}
	var ops []map[string]string
	for _, f := range files {
		ops = append(ops, map[string]string{"operation": "create", "path": dir + "/" + f.Path, "content": base64.StdEncoding.EncodeToString(f.Content)})
	}
	return r.w(ctx, http.MethodPost, rp+"/contents", map[string]any{"branch": "main", "message": message, "files": ops}, nil)
}

// migratedFrom は、メッセージの trailer Logiforge-Migrated-From: の値を返す。なければ空（design/directory/migrate.md）。
func migratedFrom(message string) string {
	for _, l := range strings.Split(message, "\n") {
		if v, ok := strings.CutPrefix(l, "Logiforge-Migrated-From: "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// Archived は、git リポジトリが archive（読むだけ）かを、主体のトークンで読む。
func (r repos) Archived(ctx context.Context, gitForgeRef string) (bool, error) {
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return false, err
	}
	var x struct {
		Archived bool `json:"archived"`
	}
	if err := r.w(ctx, http.MethodGet, rp, nil, &x); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return false, ports.ErrNotFound
		}
		return false, err
	}
	return x.Archived, nil
}
