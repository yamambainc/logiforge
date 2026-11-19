package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
)

// Config は、GitHub アダプタの設定である（design/settings/README.md）。
type Config struct {
	// APIURL は、REST の URL。空なら https://api.github.com。
	APIURL string
	// WebURL は、画面と git の URL。空なら https://github.com。
	WebURL string
	// Repositories は、repository の名前と `owner/name` の組。名前の正本である（design/adapters/README.md の「repository の登録」）。
	Repositories []ports.Repository
	// ClientID と ClientSecret は、Logiforge の GitHub App の資格情報。主体のトークンが、この App のものかを確かめる。
	// なければ、どの主体も user とみなさない（design/settings/users.md の「トークンの出どころ」）。
	ClientID, ClientSecret string
	// HTTP は、使う HTTP のクライアント。nil なら、時間の上限（30 秒）を付けたクライアント。
	HTTP *http.Client
	// MergeWait は、merge できるかを GitHub が確かめ終わるまで待つ時間の上限。0 なら 15 秒。
	MergeWait time.Duration
}

func (c Config) webURL() string {
	if c.WebURL == "" {
		return DefaultWebURL
	}
	return strings.TrimRight(c.WebURL, "/")
}

// forge は、ctx の主体のトークン（ports.WithToken）で REST を呼ぶ。
type forge struct {
	cfg  Config
	c    *client
	keys pullreq.Keyring
}

// w は、ctx の主体のトークンで呼ぶ。トークンがなければ、認証なしで呼ぶ（public のリポジトリを読むとき）。
func (f *forge) w(ctx context.Context, _ string, method, path string, in, out any) error {
	return f.c.do(ctx, ports.TokenFrom(ctx), method, path, in, out)
}

// New は、GitHub の GitForge アダプタと認証のアダプタを作る。keys は、記録と、用意の 2 つ目のコミットの署名に使う。
func New(cfg Config, keys pullreq.Keyring) (*pullreq.Adapter, *Auth, error) {
	if err := cfg.check(); err != nil {
		return nil, nil, err
	}
	f := &forge{cfg: cfg, c: newClient(cfg.APIURL, cfg.HTTP), keys: keys}
	return &pullreq.Adapter{Repos: repos{f}, API: api{f}, Keys: keys}, &Auth{f: f}, nil
}

func (c Config) check() error {
	if err := ports.CheckDirectories(c.Repositories); err != nil {
		return fmt.Errorf("github: %w", err)
	}
	seen := map[string]bool{}
	for _, r := range c.Repositories {
		if r.Name == "" || seen[r.Name] {
			return fmt.Errorf("github: repository の名前が空か、重なっている: %q", r.Name)
		}
		seen[r.Name] = true
		if _, err := repoPath(r.GitForgeRef); err != nil {
			return err
		}
	}
	return nil
}

// ParseRepositories は、`名前=owner/name,…` の形の設定を読む。
func ParseRepositories(v string) ([]ports.Repository, error) {
	var out []ports.Repository
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, ref, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("github: repository の組は 名前=owner/name の形で書く: %q", part)
		}
		if _, err := repoPath(ref); err != nil {
			return nil, err
		}
		out = append(out, ports.Repository{Name: name, GitForgeRef: ref})
	}
	return out, nil
}

// repos は、設定に登録した repository の列挙と用意である。
type repos struct{ *forge }

func (r repos) ListRepositories(context.Context) ([]ports.Repository, error) {
	return sortedRepos(r.cfg.Repositories), nil
}

func sortedRepos(rs []ports.Repository) []ports.Repository {
	out := slices.Clone(rs)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GitForgeRef は、名前から gitforge_ref（owner/name）を、設定から引く。登録されていなければ ports.ErrNotRegistered を返す。
func (r repos) GitForgeRef(_ context.Context, name string) (string, error) {
	return lookup(r.cfg.Repositories, name)
}

func lookup(rs []ports.Repository, name string) (string, error) {
	for _, x := range rs {
		if x.Name == name {
			return x.GitForgeRef, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ports.ErrNotRegistered, name)
}

// GitURL は、クライアントが clone と push に使う git の URL を返す。
func (r repos) GitURL(gitForgeRef string) string {
	return r.cfg.webURL() + "/" + gitRef(gitForgeRef) + ".git"
}

// MainHead は、main の先頭のコミットの SHA を返す。
func (r repos) MainHead(ctx context.Context, gitForgeRef string) (string, error) {
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return "", err
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	// 空の repository では 409、main がなければ 404 を返す。
	if err := r.w(ctx, gitForgeRef, http.MethodGet, rp+"/git/ref/heads/main", nil, &ref); err != nil {
		if s := statusOf(err); s == http.StatusNotFound || s == http.StatusConflict {
			return "", ports.ErrNotFound
		}
		return "", err
	}
	return ref.Object.SHA, nil
}

// SetupRepository は、GitHub で作った空の repository に、用意のコミットを置く（design/adapters/github.md の「repository を用意する」）。
// 呼ぶのは、repository を作る利用者のトークンである。
//   - 根のコミット：contents API（PUT /contents）で、根の仮説のファイルを置く。空のリポジトリでは、ほかの方法がないから。
//   - 2 つ目のコミット：残りのファイル（背景情報）を、Git Data API で 1 つのコミットにまとめて置き、
//     trailer Logiforge-Init-Signature: を付ける（design/backend/verify/init.md の「repository の用意のコミット」）。
//
// main が呼ぶ前からあれば、用意の続き（2 つ目のコミット）だけを行い、ErrMainExists を返す。repository がなければ ErrNotFound を返す。
func (r repos) SetupRepository(ctx context.Context, gitForgeRef string, files []ports.File, message string) error {
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return err
	}
	if _, dir := ports.SplitRef(gitForgeRef); dir != "" {
		return r.setupDirectory(ctx, gitForgeRef, rp, dir, files, message)
	}
	if err := r.w(ctx, gitForgeRef, http.MethodGet, rp, nil, nil); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return fmt.Errorf("%w: GitHub の repository %s がない（GitHub でリポジトリを作り、利用者に Write を与える）", ports.ErrNotFound, gitForgeRef)
		}
		return err
	}
	if _, err := r.MainHead(ctx, gitForgeRef); err == nil {
		if err := r.resumeSetup(ctx, gitForgeRef, files, message); err != nil {
			return err
		}
		return ports.ErrMainExists
	} else if !errors.Is(err, ports.ErrNotFound) {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	var res struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	// 根のコミットにも、署名を付ける。なぜ：repository の admin が Write を与えてから用意するまでのあいだに、ほかの人が Logiforge-Init: 付きの
	// 根を push できるから。worker は、署名で用意のコミットと決める（design/backend/verify/init.md の「repository の用意のコミット」）。
	repoID, err := api(r).RepoID(ctx, gitForgeRef)
	if err != nil {
		return err
	}
	rootMessage, err := r.keys.InitMessage(message, pullreq.InitScope{Kind: ports.InitRoot, RepoID: repoID}, files[:1])
	if err != nil {
		return err
	}
	body := map[string]string{"message": rootMessage, "content": base64.StdEncoding.EncodeToString(files[0].Content), "branch": "main"}
	if err := r.w(ctx, gitForgeRef, http.MethodPut, rp+"/contents/"+escapePath(files[0].Path), body, &res); err != nil {
		return err
	}
	return r.putSecond(ctx, gitForgeRef, res.Commit.SHA, files[1:], message)
}

// putSecond は、根のコミット root の上に、files を 1 つのコミットにまとめて置き、main をそのコミットに進める。
// main が root でなくなっていれば（ほかの人が先に進めた）、進めない（fast-forward でない更新は GitHub が断る）。
func (r repos) putSecond(ctx context.Context, ref, root string, files []ports.File, message string) error {
	if len(files) == 0 {
		return nil
	}
	rp, _ := repoPath(ref)
	repoID, err := api(r).RepoID(ctx, ref)
	if err != nil {
		return err
	}
	var rootCommit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := r.w(ctx, ref, http.MethodGet, rp+"/git/commits/"+url.PathEscape(root), nil, &rootCommit); err != nil {
		return err
	}
	type entry struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	entries := make([]entry, 0, len(files))
	for _, f := range files {
		entries = append(entries, entry{Path: f.Path, Mode: "100644", Type: "blob", Content: string(f.Content)})
	}
	msg, err := r.keys.InitMessage(stripInitSignature(message), pullreq.InitScope{Kind: ports.InitSecond, RepoID: repoID, Parent: root}, files)
	if err != nil {
		return err
	}
	var tree, commit struct {
		SHA string `json:"sha"`
	}
	if err := r.w(ctx, ref, http.MethodPost, rp+"/git/trees", map[string]any{"base_tree": rootCommit.Tree.SHA, "tree": entries}, &tree); err != nil {
		return err
	}
	if err := r.w(ctx, ref, http.MethodPost, rp+"/git/commits", map[string]any{"message": msg, "tree": tree.SHA, "parents": []string{root}}, &commit); err != nil {
		return err
	}
	return r.w(ctx, ref, http.MethodPatch, rp+"/git/refs/heads/main", map[string]any{"sha": commit.SHA, "force": false}, nil)
}

// stripInitSignature は、メッセージから Logiforge-Init-Signature: の行を除く（送り直しで、署名を重ねないため）。
func stripInitSignature(message string) string {
	var out []string
	for _, l := range strings.Split(message, "\n") {
		if !strings.HasPrefix(l, snapshot.TrailerInitSignature+":") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// resumeSetup は、用意が根のコミットのあとで止まっていたら、2 つ目のコミットを置いて続ける。
// 送り直しでは、根の仮説の識別子が新しく決まる（バックエンドが作るたびに乱数で決める）。そのため、main にある根の仮説の
// 識別子に置き換えて、1 つ目のファイルの中身が同じときだけ続ける。中身が違えば、別の用意なので、何もしない。
//
// 続けるのは、main が根のコミット 1 つだけで、そのコミットに Logiforge-Init: があるときだけである。
func (r repos) resumeSetup(ctx context.Context, ref string, files []ports.File, message string) error {
	if len(files) < 2 {
		return nil
	}
	rp, _ := repoPath(ref)
	var head struct {
		SHA     string     `json:"sha"`
		Parents []struct{} `json:"parents"`
		Commit  struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := r.w(ctx, ref, http.MethodGet, rp+"/commits/main", nil, &head); err != nil {
		return err
	}
	if len(head.Parents) != 0 || !strings.Contains(head.Commit.Message, snapshot.TrailerInit+":") {
		return nil
	}
	// files[0] は、<根の dir>/<根の仮説のファイル>。根の dir の親（tree/）にある、ただ 1 つの dir を、main の根とする。
	rootDir := path.Dir(files[0].Path)
	newID := path.Base(rootDir)
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := r.w(ctx, ref, http.MethodGet, rp+"/contents/"+escapePath(path.Dir(rootDir))+"?ref=main", nil, &entries); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return nil
		}
		return err
	}
	if len(entries) != 1 || entries[0].Type != "dir" {
		return nil
	}
	oldID := entries[0].Name
	remap := func(f ports.File) ports.File {
		return ports.File{Path: path.Join(path.Dir(rootDir), oldID, strings.TrimPrefix(f.Path, rootDir+"/")),
			Content: bytes.ReplaceAll(f.Content, []byte(newID), []byte(oldID))}
	}
	first, err := r.content(ctx, ref, remap(files[0]).Path)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil || !bytes.Equal(first, remap(files[0]).Content) {
		return err
	}
	rest := make([]ports.File, 0, len(files)-1)
	for _, f := range files[1:] {
		rest = append(rest, remap(f))
	}
	return r.putSecond(ctx, ref, head.SHA, rest, message)
}

// content は、main のファイルの中身を読む。なければ ErrNotFound を返す。
func (r repos) content(ctx context.Context, ref, p string) ([]byte, error) {
	return r.contentAt(ctx, ref, p, "main")
}

// contentAt は、ブランチのファイルの中身を読む。なければ ErrNotFound を返す。
func (r repos) contentAt(ctx context.Context, ref, p, branch string) ([]byte, error) {
	rp, _ := repoPath(ref)
	var c struct {
		Content string `json:"content"`
	}
	if err := r.w(ctx, ref, http.MethodGet, rp+"/contents/"+escapePath(p)+"?ref="+url.QueryEscape(branch), nil, &c); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}
	return base64.StdEncoding.DecodeString(strings.ReplaceAll(c.Content, "\n", ""))
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// Auth は、GitHub のトークンで主体を確かめる（design/adapters/github.md の「認証」）。
// リポジトリの権限を問い合わせた結果だけを、60 秒持つ（design/settings/users.md の「権限のキャッシュ」）。
type Auth struct {
	f     *forge
	cache ports.PermCache
}

var _ ports.Authenticator = (*Auth)(nil)

// Authenticate は、トークンの主体を確かめる。GitHub への問い合わせは、主体のトークンで行う。
//   - user：Logiforge の GitHub App のトークンを持ち、登録した GitHub リポジトリのどれかに Write 以上の権限がある人
//   - client：それ以外の、GitHub のアカウント（gh の CLI のトークン、機械用アカウントなど）
func (a *Auth) Authenticate(ctx context.Context, token string) (ports.Principal, error) {
	var u user
	if err := a.f.c.do(ctx, token, http.MethodGet, "/user", nil, &u); err != nil {
		if s := statusOf(err); s == http.StatusUnauthorized || s == http.StatusForbidden {
			return ports.Principal{}, ports.ErrUnauthenticated
		}
		return ports.Principal{}, err
	}
	if u.Login == "" {
		return ports.Principal{}, ports.ErrUnauthenticated
	}
	p := ports.Principal{Login: u.Login, Kind: ports.PrincipalClient}
	if a.f.cfg.ClientID == "" {
		return p, nil
	}
	// Logiforge の App のトークンかを、App の資格情報で確かめる。
	// なぜ：ほかのトークン（gh のトークンなど）を持つ主体（エージェントなど）を、user にしないため。
	ours, err := a.ownToken(ctx, token)
	if err != nil {
		return ports.Principal{}, err
	}
	if !ours {
		return p, nil
	}
	p.WebUI = true
	perms, err := a.permissions(ctx, token)
	if err != nil {
		return ports.Principal{}, err
	}
	for _, r := range a.f.cfg.Repositories {
		git, _ := ports.SplitRef(r.GitForgeRef)
		if perms[strings.ToLower(git)] {
			p.Kind = ports.PrincipalUser
			break
		}
	}
	return p, nil
}

// CanWrite は、主体が、登録した repository の GitHub リポジトリに Write 以上の権限を持つかを返す。
func (a *Auth) CanWrite(ctx context.Context, token, repo string) (bool, error) {
	ref, err := lookup(a.f.cfg.Repositories, repo)
	if err != nil {
		return false, nil
	}
	perms, err := a.permissions(ctx, token)
	if err != nil {
		return false, err
	}
	git, _ := ports.SplitRef(ref)
	return perms[strings.ToLower(git)], nil
}

// CanCreate は、主体が repository を用意できるかを返す。GitHub では、登録した GitHub リポジトリを人が先に作るので、CanWrite と同じ。
func (a *Auth) CanCreate(ctx context.Context, token, repo string) (bool, error) {
	return a.CanWrite(ctx, token, repo)
}

// permissions は、主体の user access token で読めるリポジトリと、それぞれに push の権限があるかを返す（owner/name は小文字）。
// user access token で読めるのは、App が install されていて、かつ本人が読めるリポジトリだけである。
func (a *Auth) permissions(ctx context.Context, token string) (map[string]bool, error) {
	if !ports.Fresh(ctx) {
		if v, ok := a.cache.Get(token, "push"); ok {
			return v.(map[string]bool), nil
		}
	}
	type installation struct {
		ID int64 `json:"id"`
	}
	type repo struct {
		FullName    string `json:"full_name"`
		Permissions struct {
			Admin bool `json:"admin"`
			Push  bool `json:"push"`
		} `json:"permissions"`
	}
	insts, err := listIn[installation](ctx, a.f.c, token, "/user/installations", "installations")
	if err != nil {
		return nil, err
	}
	perms := map[string]bool{}
	for _, in := range insts {
		rs, err := listIn[repo](ctx, a.f.c, token, fmt.Sprintf("/user/installations/%d/repositories", in.ID), "repositories")
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			perms[strings.ToLower(r.FullName)] = r.Permissions.Push || r.Permissions.Admin
		}
	}
	a.cache.Put(token, "push", perms)
	return perms, nil
}

// ownToken は、トークンが Logiforge の GitHub App のものかを、POST /applications/{client_id}/token で確かめる。
func (a *Auth) ownToken(ctx context.Context, token string) (bool, error) {
	basic := basicPrefix + base64.StdEncoding.EncodeToString([]byte(a.f.cfg.ClientID+":"+a.f.cfg.ClientSecret))
	err := a.f.c.do(ctx, basic, http.MethodPost, "/applications/"+url.PathEscape(a.f.cfg.ClientID)+"/token", map[string]string{"access_token": token}, nil)
	switch s := statusOf(err); {
	case err == nil:
		return true, nil
	case s == http.StatusNotFound || s == http.StatusUnprocessableEntity:
		return false, nil
	default:
		return false, err
	}
}

// CanRead は、主体のトークンで、登録した GitHub の repository を読めるかを確かめる。
func (a *Auth) CanRead(ctx context.Context, token, repo string) (bool, error) {
	ref, err := lookup(a.f.cfg.Repositories, repo)
	if err != nil {
		return false, nil
	}
	rp, err := repoPath(ref)
	if err != nil {
		return false, err
	}
	if err := a.f.c.do(ctx, token, http.MethodGet, rp, nil, nil); err != nil {
		if s := statusOf(err); s == http.StatusNotFound || s == http.StatusForbidden || s == http.StatusUnauthorized {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// setupDirectory は、ディレクトリの repository の用意のコミットを、利用者のトークンで、プロジェクトのリポジトリの main に置く
// （design/directory/history.md の「用意のコミット」）。今の main の木にディレクトリのファイルを足したコミットを Git Data API で作り、
// main を fast-forward で進める。main がそのあいだに進んでいたら、読み直して作り直す。ディレクトリがすでにあれば ErrMainExists。
func (r repos) setupDirectory(ctx context.Context, ref, rp, dir string, files []ports.File, message string) error {
	repoID, err := api(r).RepoID(ctx, ref)
	if err != nil {
		return err
	}
	signed, err := r.keys.InitMessage(stripInitSignature(message), pullreq.InitScope{Kind: ports.InitDirectory, RepoID: repoID, Directory: dir, MigratedFrom: migratedFrom(message)}, files)
	if err != nil {
		return err
	}
	type entry struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	entries := make([]entry, 0, len(files))
	for _, f := range files {
		entries = append(entries, entry{Path: dir + "/" + f.Path, Mode: "100644", Type: "blob", Content: string(f.Content)})
	}
	for attempt := 0; ; attempt++ {
		head, err := r.MainHead(ctx, ref)
		if errors.Is(err, ports.ErrNotFound) {
			return fmt.Errorf("%w: プロジェクトのリポジトリ %s の main がない", ports.ErrNotFound, gitRef(ref))
		}
		if err != nil {
			return err
		}
		// ディレクトリがすでにあれば、用意しない（そのディレクトリを変えた、前のコミットがあってはいけない）。
		if err := r.w(ctx, ref, http.MethodGet, rp+"/contents/"+escapePath(dir)+"?ref="+url.QueryEscape(head), nil, nil); err == nil {
			return ports.ErrMainExists
		} else if statusOf(err) != http.StatusNotFound {
			return err
		}
		var commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		}
		if err := r.w(ctx, ref, http.MethodGet, rp+"/git/commits/"+url.PathEscape(head), nil, &commit); err != nil {
			return err
		}
		var t, c struct {
			SHA string `json:"sha"`
		}
		if err := r.w(ctx, ref, http.MethodPost, rp+"/git/trees", map[string]any{"base_tree": commit.Tree.SHA, "tree": entries}, &t); err != nil {
			return err
		}
		if err := r.w(ctx, ref, http.MethodPost, rp+"/git/commits", map[string]any{"message": signed, "tree": t.SHA, "parents": []string{head}}, &c); err != nil {
			return err
		}
		err = r.w(ctx, ref, http.MethodPatch, rp+"/git/refs/heads/main", map[string]any{"sha": c.SHA, "force": false}, nil)
		// main が進んでいれば（fast-forward でない）、422。読み直して、もう一度作る。
		if statusOf(err) == http.StatusUnprocessableEntity && attempt < 3 {
			continue
		}
		return err
	}
}

// migratedFrom は、メッセージの trailer Logiforge-Migrated-From: の値を返す。なければ空（design/directory/migrate.md）。
func migratedFrom(message string) string {
	for _, l := range strings.Split(message, "\n") {
		if v, ok := strings.CutPrefix(l, snapshot.TrailerMigratedFrom+": "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
	if err := r.w(ctx, gitForgeRef, http.MethodGet, rp, nil, &x); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return false, ports.ErrNotFound
		}
		return false, err
	}
	return x.Archived, nil
}
