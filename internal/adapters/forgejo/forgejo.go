package forgejo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	return &pullreq.Adapter{Repos: repos{c, keys}, API: api{c}, Keys: keys}, &Auth{c: c}
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
		// 根のコミットにも、署名を付ける。worker は、それで用意のコミットと決める（design/backend/verify/README.md）。
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

// Auth は、Forgejo のトークンで主体を確かめる（design/adapters/forgejo.md の「認証」）。
// 権限を問い合わせた結果だけを、60 秒持つ（design/settings/users.md の「権限のキャッシュ」）。
type Auth struct {
	c     *client
	cache ports.PermCache
}

var _ ports.Authenticator = (*Auth)(nil)

// orgTeam は、主体の team のうち、設定した organization のものである。
type orgTeam struct {
	Name         string            `json:"name"`
	Permission   string            `json:"permission"`
	CanCreate    bool              `json:"can_create_org_repo"`
	UnitsMap     map[string]string `json:"units_map"`
	Organization struct {
		Username string `json:"username"`
	} `json:"organization"`
}

// writes は、team が、repository のコードに書けるものかである（Write 以上）。
// Forgejo は、team の permission を units_map から決める。コードが read で、issue と pull request が write の team は、read になる。
// そのため、コードの単位（repo.code）の write か、team の admin か owner を見る。
func (t orgTeam) writes() bool {
	switch t.Permission {
	case "admin", "owner":
		return true
	}
	return t.UnitsMap["repo.code"] == "write" || (t.UnitsMap == nil && t.Permission == "write")
}

// Authenticate は、トークンの主体を確かめる。
//   - user：Logiforge の OAuth2 のアプリケーションのトークンを持ち、設定した organization の team のどれかで、
//     コードに書ける（repo.code が write か、team が admin か owner）人
//   - client：それ以外の、organization の人とエージェント
//
// organization に属さない主体は、受け付けない。
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
	teams, err := a.teams(ctx, token)
	if err != nil {
		return ports.Principal{}, err
	}
	if len(teams) == 0 {
		return ports.Principal{}, ports.ErrUnauthenticated
	}
	p := ports.Principal{Login: u.Login, Kind: ports.PrincipalClient}
	if a.c.cfg.ClientID == "" {
		return p, nil
	}
	if p.WebUI, err = a.ownToken(ctx, token); err != nil {
		return ports.Principal{}, err
	}
	if !p.WebUI {
		return p, nil
	}
	for _, t := range teams {
		if t.writes() {
			p.Kind = ports.PrincipalUser
			break
		}
	}
	return p, nil
}

// teams は、主体の team のうち、設定した organization のものを返す。
func (a *Auth) teams(ctx context.Context, token string) ([]orgTeam, error) {
	if !ports.Fresh(ctx) {
		if v, ok := a.cache.Get(token, "teams"); ok {
			return v.([]orgTeam), nil
		}
	}
	all, err := list[orgTeam](ctx, a.c, token, "/user/teams")
	if err != nil {
		return nil, err
	}
	var out []orgTeam
	for _, t := range all {
		if t.Organization.Username == a.c.cfg.Org {
			out = append(out, t)
		}
	}
	a.cache.Put(token, "teams", out)
	return out, nil
}

// ownToken は、トークンが Logiforge の OAuth2 のアプリケーションのものかを、introspect で確かめる。
// active が true で、かつ aud に client_id があるときだけ、そうだとする。
// なぜ active だけで決めないか：Forgejo は、ほかのアプリケーションが出したトークンにも active: true を返すから。
// personal access token には、active: false が返る（design/adapters/forgejo.md の「認証」）。
func (a *Auth) ownToken(ctx context.Context, token string) (bool, error) {
	form := url.Values{"token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.c.cfg.URL, "/")+"/login/oauth/introspect", strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(a.c.cfg.ClientID, a.c.cfg.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := a.c.http().Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: forgejo: introspect：%v", ports.ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("forgejo: introspect が %d を返した（OAuth2 のアプリケーションの client_id と client secret を確かめる）", res.StatusCode)
	}
	var out struct {
		Active bool            `json:"active"`
		Aud    json.RawMessage `json:"aud"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out); err != nil {
		return false, fmt.Errorf("forgejo: introspect の応答を読めない：%w", err)
	}
	return out.Active && audHas(out.Aud, a.c.cfg.ClientID), nil
}

// audHas は、aud（文字列か、文字列の配列）に id があるかを返す。
func audHas(raw json.RawMessage, id string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == id
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, s := range many {
			if s == id {
				return true
			}
		}
	}
	return false
}

// CanWrite は、主体が repository に Write 以上の権限を持つかを返す。ディレクトリの repository では、git リポジトリの権限を見る。
// 主体のトークンで git リポジトリを読めなければ（404）、false である。まだないときだけでなく、主体の team に
// その git リポジトリが入っていないときも 404 になるから。
func (a *Auth) CanWrite(ctx context.Context, token, repo string) (bool, error) {
	return a.can(ctx, token, repo, false)
}

// CanCreate は、主体が repository を用意できるかを返す。git リポジトリがあれば、CanWrite と同じ。
// なければ（404）、organization に repository を作れる team（コードに書ける）にいるかを返す。作る前の git リポジトリには、権限がないから。
// 404 が「あるが見えない」ときは、作る要求を Forgejo が断る。
func (a *Auth) CanCreate(ctx context.Context, token, repo string) (bool, error) {
	return a.can(ctx, token, repo, true)
}

func (a *Auth) can(ctx context.Context, token, repo string, create bool) (bool, error) {
	ref := a.c.cfg.Org + "/" + repo
	for _, d := range a.c.cfg.Directories {
		if d.Name == repo {
			ref = d.GitForgeRef
		}
	}
	rp, err := repoPath(ref)
	if err != nil {
		return false, err
	}
	q := "push:" + rp
	if create {
		q = "create:" + rp
	}
	if !ports.Fresh(ctx) {
		if v, ok := a.cache.Get(token, q); ok {
			return v.(bool), nil
		}
	}
	var r struct {
		Permissions struct {
			Admin bool `json:"admin"`
			Push  bool `json:"push"`
		} `json:"permissions"`
	}
	ok := false
	switch err := a.c.do(ctx, token, http.MethodGet, rp, nil, &r); {
	case err == nil:
		ok = r.Permissions.Push || r.Permissions.Admin
	case statusOf(err) == http.StatusNotFound:
		if !create {
			break
		}
		teams, err := a.teams(ctx, token)
		if err != nil {
			return false, err
		}
		for _, t := range teams {
			if t.CanCreate && t.writes() {
				ok = true
			}
		}
	default:
		return false, err
	}
	a.cache.Put(token, q, ok)
	return ok, nil
}

// CanRead は、主体が repository を読めるかを返す。Forgejo を使う構成では、organization のすべての主体が、
// organization のすべての repository を読める（design/adapters/forgejo.md の「認証」）。主体が organization にいることは
// Authenticate が確かめているので、ここでは主体のトークンで Forgejo を呼ばない。
// なぜ：organization にいる主体は、どれも読めると決めているので、Forgejo に聞いても答えが変わらないから。
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

// Prepared は、repository の用意が済んでいるか（スナップショットツリーがもうあるか）を、主体のトークンで読む。
// git リポジトリ全体なら main があるか、ディレクトリの repository なら main にそのディレクトリがあるかで決める。
func (r repos) Prepared(ctx context.Context, gitForgeRef string) (bool, error) {
	head, err := r.MainHead(ctx, gitForgeRef)
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, dir := ports.SplitRef(gitForgeRef)
	if dir == "" {
		return true, nil
	}
	rp, _ := repoPath(gitForgeRef)
	err = r.w(ctx, http.MethodGet, rp+"/contents/"+escapePath(dir)+"?ref="+url.QueryEscape(head), nil, nil)
	if err == nil {
		return true, nil
	}
	if statusOf(err) == http.StatusNotFound {
		return false, nil
	}
	return false, err
}
