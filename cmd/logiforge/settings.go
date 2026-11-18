package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/forgejo"
	"github.com/yamambainc/logiforge/internal/adapters/github"
	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/api"
	"github.com/yamambainc/logiforge/internal/backend"
	"github.com/yamambainc/logiforge/internal/ids"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/settings"
)

// settingsService は、設定の API の中身である（design/api/endpoints/settings.md）。設定の版ごとに作る。
// 書くものは、Put などで、送った項目だけを置き換える。Save（全体の置き換え）は使わない。秘密を消さないため。
type settingsService struct {
	db   *settings.DB
	keys settings.Keys
	auth ports.Authenticator
}

var _ api.SettingsService = (*settingsService)(nil)

// user は、token の主体が、WebUI のサインインの user かを確かめる。fresh なら、権限のキャッシュを使わない
// （design/settings/users.md の「権限のキャッシュ」：設定を変える要求では、キャッシュを使わない）。
func (s *settingsService) user(ctx context.Context, token string, fresh bool) (ports.Principal, error) {
	if token == "" {
		return ports.Principal{}, problem(backend.CodeUnauthenticated, "トークンがない")
	}
	if fresh {
		ctx = ports.WithFresh(ctx)
	}
	p, err := s.auth.Authenticate(ctx, token)
	if errors.Is(err, ports.ErrUnauthenticated) {
		return p, problem(backend.CodeUnauthenticated, "トークンが正しくないか、Logiforge の主体でない")
	}
	if err != nil {
		return p, err
	}
	if p.Kind != ports.PrincipalUser || !p.WebUI {
		// CLI のトークンでは、設定を読み書きできない（design/settings/users.md の「トークンの出どころ」）。
		return p, problem(backend.CodeForbidden, "設定は、WebUI でサインインした利用者（user）だけが使える")
	}
	return p, nil
}

// asUser は、設定を c2 にしたとしたら、token の主体が、WebUI のサインインの user のままかを、キャッシュを使わずに確かめる。
// 設定を変える前に呼ぶ。なぜ：サインインの資格情報や repository の一覧を変えて、変えた本人も含めて、だれも user でなくなると、
// 設定の画面から戻せないから（design/settings/users.md の「締め出されたとき」）。
func asUser(ctx context.Context, c2 *settings.Config, token string) (ports.Authenticator, error) {
	fs, err := setupForge(c2, pullreq.Keyring{}, true, nil)
	if err != nil {
		return nil, problem(backend.CodeValidationFailed, "変えたあとの設定で、GitForge につなげない：%v", err)
	}
	p, err := fs.auth.Authenticate(ports.WithFresh(ctx), token)
	if errors.Is(err, ports.ErrUnauthenticated) || err == nil && (p.Kind != ports.PrincipalUser || !p.WebUI) {
		return nil, problem(backend.CodeValidationFailed, "変えたあとの設定では、あなたが利用者（user）でなくなる。サインインの資格情報か repository の一覧を確かめる")
	}
	if err != nil {
		return nil, problem(backend.CodeValidationFailed, "変えたあとの設定で、あなたが利用者かを確かめられない：%v", err)
	}
	return fs.auth, nil
}

func secretState(set bool, ch map[string]settings.Changed, name string) api.SecretState {
	st := api.SecretState{Set: set}
	if c, ok := ch[name]; ok && set {
		by, at := c.By, c.At
		st.UpdatedBy, st.UpdatedAt = &by, &at
	}
	return st
}

func seconds(v string) float64 {
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d.Seconds()
}

func (s *settingsService) settingsOf(ctx context.Context) (api.Settings, error) {
	c, err := s.db.Load(ctx, s.keys)
	if err != nil {
		return api.Settings{}, err
	}
	ch, last, err := s.db.Changes(ctx)
	if err != nil {
		return api.Settings{}, err
	}
	out := api.Settings{GitForge: c.GitForge, WebUIURL: c.WebUIURL, UpdatedBy: last.By, UpdatedAt: last.At,
		Intervals: api.IntervalSettings{PatrolSeconds: seconds(c.Intervals.Patrol), ListSeconds: seconds(c.Intervals.List),
			RecordsSeconds: seconds(c.Intervals.Record), GitTimeoutSeconds: seconds(c.Intervals.GitTimeout), WriteWaitSeconds: seconds(c.Intervals.WaitTimeout)}}
	switch c.GitForge {
	case gitforgeGitHub:
		g := c.GitHub
		id, _ := strconv.ParseInt(g.AppID, 10, 64)
		out.GitHub = &api.GitHubSettings{AppID: id, ClientID: g.ClientID, Slug: g.Slug, APIURL: g.APIURL, WebURL: g.WebURL,
			PrivateKey: secretState(g.AppPrivateKey != "", ch, "github.app_private_key"), ClientSecret: secretState(g.ClientSecret != "", ch, "github.client_secret")}
	case gitforgeForgejo:
		f := c.Forgejo
		out.Forgejo = &api.ForgejoSettings{URL: f.URL, PublicURL: f.PublicURL, Organization: f.Org, ClientID: f.OAuthClientID, WorkerUser: f.WorkerLogin,
			ClientSecret: secretState(f.OAuthClientSecret != "", ch, "forgejo.oauth_client_secret"), WorkerToken: secretState(f.WorkerToken != "", ch, "forgejo.worker_token")}
	}
	return out, nil
}

func (s *settingsService) Get(ctx context.Context, token string) (api.Settings, error) {
	if _, err := s.user(ctx, token, false); err != nil {
		return api.Settings{}, err
	}
	return s.settingsOf(ctx)
}

// gitTimeoutLimit は、GitHub を使う構成の、git の操作の時間の上限の上限である。installation token が切れないように
// （design/adapters/github-app.md の「installation token」）。
const gitTimeoutLimit = github.RefreshBefore

func (s *settingsService) UpdateGitForge(ctx context.Context, token string, in api.GitForgeSettingsInput) (api.Settings, error) {
	p, err := s.user(ctx, token, true)
	if err != nil {
		return api.Settings{}, err
	}
	c, err := s.db.Load(ctx, s.keys)
	if err != nil {
		return api.Settings{}, err
	}
	v := map[string]string{}
	str := func(name string, p *string, check func(string) error) error {
		if p == nil {
			return nil
		}
		val := strings.TrimSpace(*p)
		if check != nil && val != "" {
			if err := check(val); err != nil {
				return err
			}
		}
		v[name] = val
		return nil
	}
	urlCheck := func(what string) func(string) error {
		return func(u string) error {
			if checkWebUIURL(u) != nil {
				return problem(backend.CodeValidationFailed, "%s は、http か https の URL にする：%q", what, u)
			}
			return nil
		}
	}
	if in.WebUIURL != nil && strings.TrimSpace(*in.WebUIURL) == "" {
		return api.Settings{}, problem(backend.CodeValidationFailed, "WebUI の URL は消せない")
	}
	if err := str("webui_url", in.WebUIURL, urlCheck("WebUI の URL")); err != nil {
		return api.Settings{}, err
	}
	if in.GitHub != nil && c.GitForge != gitforgeGitHub || in.Forgejo != nil && c.GitForge != gitforgeForgejo {
		return api.Settings{}, problem(backend.CodeValidationFailed, "構成（%s）は変えられない。その構成の設定だけを送る", c.GitForge)
	}
	required := map[string]bool{}
	if g := in.GitHub; g != nil {
		if g.AppID != nil {
			if *g.AppID <= 0 {
				return api.Settings{}, problem(backend.CodeValidationFailed, "app_id は正の数")
			}
			v["github.app_id"] = strconv.FormatInt(*g.AppID, 10)
		}
		for name, ptr := range map[string]*string{"github.client_id": g.ClientID, "github.slug": g.Slug, "github.client_secret": g.ClientSecret} {
			if err := str(name, ptr, nil); err != nil {
				return api.Settings{}, err
			}
			required[name] = true
		}
		if err := str("github.app_private_key", g.PrivateKey, func(k string) error {
			if github.CheckPrivateKey([]byte(k)) != nil {
				return problem(backend.CodeValidationFailed, "private_key は、GitHub App の秘密鍵（.pem）にする")
			}
			return nil
		}); err != nil {
			return api.Settings{}, err
		}
		required["github.app_private_key"] = true
		if err := str("github.api_url", g.APIURL, urlCheck("api_url")); err != nil {
			return api.Settings{}, err
		}
		if err := str("github.web_url", g.WebURL, urlCheck("web_url")); err != nil {
			return api.Settings{}, err
		}
	}
	if f := in.Forgejo; f != nil {
		for name, ptr := range map[string]*string{"forgejo.org": f.Organization, "forgejo.oauth_client_id": f.ClientID,
			"forgejo.oauth_client_secret": f.ClientSecret, "forgejo.worker_login": f.WorkerUser, "forgejo.worker_token": f.WorkerToken} {
			if err := str(name, ptr, nil); err != nil {
				return api.Settings{}, err
			}
			required[name] = true
		}
		if err := str("forgejo.url", f.URL, urlCheck("Forgejo の URL")); err != nil {
			return api.Settings{}, err
		}
		required["forgejo.url"] = true
		if err := str("forgejo.public_url", f.PublicURL, urlCheck("Forgejo のブラウザからの URL")); err != nil {
			return api.Settings{}, err
		}
	}
	// 動くのに要る設定は、空にして消せない。
	for name := range required {
		if val, ok := v[name]; ok && val == "" {
			return api.Settings{}, problem(backend.CodeValidationFailed, "%s は消せない。入れ直すだけができる", name)
		}
	}
	if iv := in.Intervals; iv != nil {
		for name, ptr := range map[string]*float64{"interval.patrol": iv.PatrolSeconds, "interval.list": iv.ListSeconds,
			"interval.record": iv.RecordsSeconds, "interval.git_timeout": iv.GitTimeoutSeconds, "interval.wait_timeout": iv.WriteWaitSeconds} {
			if ptr == nil {
				continue
			}
			if *ptr <= 0 || *ptr > 24*3600 {
				return api.Settings{}, problem(backend.CodeValidationFailed, "%s は、0 より大きく 1 日までの秒数にする", name)
			}
			v[name] = time.Duration(*ptr * float64(time.Second)).String()
		}
		if c.GitForge == gitforgeGitHub {
			gt := c.Intervals.GitTimeout
			if n, ok := v["interval.git_timeout"]; ok {
				gt = n
			}
			if gt != "" && settings.Duration(gt, 0) >= gitTimeoutLimit {
				return api.Settings{}, problem(backend.CodeValidationFailed, "GitHub を使う構成では、git の操作の時間の上限を %s より短くする（installation token が切れないように）", gitTimeoutLimit)
			}
		}
	}
	if len(v) == 0 {
		return s.settingsOf(ctx)
	}
	// Forgejo の worker のつなぎ方が変わるなら、置く前に、読めて書けないことを確かめる。
	if c.GitForge == gitforgeForgejo {
		changed := false
		for _, n := range []string{"forgejo.url", "forgejo.org", "forgejo.worker_login", "forgejo.worker_token"} {
			if _, ok := v[n]; ok {
				changed = true
			}
		}
		if changed {
			pick := func(name, old string) string {
				if n, ok := v[name]; ok {
					return n
				}
				return old
			}
			cfg := forgejo.Config{URL: pick("forgejo.url", c.Forgejo.URL), Org: pick("forgejo.org", c.Forgejo.Org)}
			if err := forgejo.CheckWorkerToken(ctx, cfg, pick("forgejo.worker_login", c.Forgejo.WorkerLogin), pick("forgejo.worker_token", c.Forgejo.WorkerToken)); err != nil {
				return api.Settings{}, problem(backend.CodeValidationFailed, "worker のトークン：%v", err)
			}
		}
	}
	// サインインと権限の確かめに使う設定が変わるなら、変えたあとの設定でも、変えた本人が user のままかを確かめる。
	for _, n := range []string{"github.client_id", "github.client_secret", "github.api_url", "github.web_url",
		"forgejo.url", "forgejo.org", "forgejo.oauth_client_id", "forgejo.oauth_client_secret"} {
		if _, ok := v[n]; ok {
			c2 := c.Clone()
			c2.Apply(v)
			if _, err := asUser(ctx, c2, token); err != nil {
				return api.Settings{}, err
			}
			break
		}
	}
	// App の ID か秘密鍵が変わるなら、新しいもので、登録した repository を読めるかを確かめる。
	if _, idOK := v["github.app_id"]; idOK || v["github.app_private_key"] != "" {
		c2 := c.Clone()
		c2.Apply(v)
		if len(c2.Repositories) > 0 {
			r := c2.Repositories[0]
			if err := checkGitHubRepository(ctx, c2, c2.Repositories[1:], r); err != nil {
				return api.Settings{}, problem(backend.CodeValidationFailed, "新しい App の ID と秘密鍵で、%s を読めない：%s", r.GitForgeRef, backend.AsError(err).Detail)
			}
		}
	}
	if err := s.db.Put(ctx, s.keys, v, p.Login); err != nil {
		return api.Settings{}, problem(backend.CodeValidationFailed, "%v", err)
	}
	names := make([]string, 0, len(v))
	for n := range v {
		names = append(names, n)
	}
	sort.Strings(names)
	slog.Info("設定を変えた", "user", p.Login, "settings", strings.Join(names, ","))
	return s.settingsOf(ctx)
}

func (s *settingsService) ListRepositories(ctx context.Context, token string) ([]api.SetupRepository, error) {
	if _, err := s.user(ctx, token, false); err != nil {
		return nil, err
	}
	c, err := s.db.Load(ctx, s.keys)
	if err != nil {
		return nil, err
	}
	var out []api.SetupRepository
	for _, r := range c.Repositories {
		out = append(out, api.SetupRepository{Name: r.Name, GitForgeRef: r.GitForgeRef, Directory: r.Directory})
	}
	return out, nil
}

func (s *settingsService) RegisterRepository(ctx context.Context, token string, in api.SetupRepository) (api.SetupRepository, error) {
	p, err := s.user(ctx, token, true)
	if err != nil {
		return api.SetupRepository{}, err
	}
	if !ids.RepositoryNamePattern.MatchString(in.Name) {
		return api.SetupRepository{}, problem(backend.CodeValidationFailed, "repository の名前は、小文字の英数字とハイフンで、63 文字以下")
	}
	c, err := s.db.Load(ctx, s.keys)
	if err != nil {
		return api.SetupRepository{}, err
	}
	r := settings.Repository{Name: in.Name, GitForgeRef: strings.TrimSpace(in.GitForgeRef), Directory: strings.Trim(strings.TrimSpace(in.Directory), "/")}
	var others []settings.Repository
	for _, o := range c.Repositories {
		if o.Name == r.Name {
			if o.GitForgeRef == r.GitForgeRef && o.Directory == r.Directory {
				return in, nil
			}
			return api.SetupRepository{}, problem(backend.CodeValidationFailed, "%s は、%s%s で登録してある。登録した組は変えられない", o.Name, o.GitForgeRef, dirSuffix(o.Directory))
		}
		others = append(others, o)
	}
	switch c.GitForge {
	case gitforgeGitHub:
		if err := checkGitHubRepository(ctx, c, others, r); err != nil {
			return api.SetupRepository{}, err
		}
	case gitforgeForgejo:
		// Forgejo を使う構成のふつうの repository は、登録しない（利用者が作る）。登録するのは、ディレクトリの repository だけ。
		if r.Directory == "" {
			return api.SetupRepository{}, problem(backend.CodeValidationFailed, "Forgejo を使う構成で登録するのは、ディレクトリの repository だけ。ふつうの repository は、repository の一覧で作る")
		}
		if _, err := ports.ParseDirectories(repoList(append(others, r), true)); err != nil {
			return api.SetupRepository{}, problem(backend.CodeValidationFailed, "%v", err)
		}
	}
	// 登録できるのは、本人が Write を持つ git リポジトリだけ。なぜ：登録すると、その git リポジトリに Write を持つ人が user になるので、
	// 利用者の範囲を、本人の権限の外まで広げないため。
	c2 := c.Clone()
	c2.Repositories = append(c2.Repositories, r)
	auth, err := asUser(ctx, c2, token)
	if err != nil {
		return api.SetupRepository{}, err
	}
	if ok, err := auth.CanWrite(ports.WithFresh(ctx), token, r.Name); err != nil {
		return api.SetupRepository{}, err
	} else if !ok {
		return api.SetupRepository{}, problem(backend.CodeForbidden, "あなたは %s に Write がない。登録できるのは、自分が Write を持つ git リポジトリだけ", r.GitForgeRef)
	}
	if err := s.db.RegisterRepository(ctx, r, p.Login); errors.Is(err, settings.ErrRepositoryConflict) {
		return api.SetupRepository{}, problem(backend.CodeValidationFailed, "%v。外した名前は、前と同じ組でだけ登録し直せる", err)
	} else if err != nil {
		return api.SetupRepository{}, err
	}
	slog.Info("repository を登録した", "user", p.Login, "repository", r.Name, "gitforge_ref", r.GitForgeRef, "directory", r.Directory)
	return api.SetupRepository{Name: r.Name, GitForgeRef: r.GitForgeRef, Directory: r.Directory}, nil
}

func dirSuffix(d string) string {
	if d == "" {
		return ""
	}
	return ":" + d
}

func (s *settingsService) UnregisterRepository(ctx context.Context, token, name string) error {
	p, err := s.user(ctx, token, true)
	if err != nil {
		return err
	}
	c, err := s.db.Load(ctx, s.keys)
	if err != nil {
		return err
	}
	// GitHub を使う構成では、外したあとも、外す本人が、残りの repository のどれかに Write を持つことを求める。
	// なぜ：GitHub の user は、登録した repository の Write で決まるので、外すと、本人（最後の 1 つなら全員）が user でなくなり、
	// 設定の画面から戻せなくなるから（design/settings/users.md の「締め出されたとき」）。
	if c.GitForge == gitforgeGitHub {
		c2 := c.Clone()
		c2.Repositories = nil
		for _, r := range c.Repositories {
			if r.Name != name {
				c2.Repositories = append(c2.Repositories, r)
			}
		}
		if len(c2.Repositories) == 0 {
			return problem(backend.CodeValidationFailed, "最後の repository は外せない。外すと、だれも利用者でなくなる。先に、ほかの repository を登録する")
		}
		if len(c2.Repositories) < len(c.Repositories) {
			if _, err := asUser(ctx, c2, token); err != nil {
				return problem(backend.CodeValidationFailed, "外すと、あなたが利用者でなくなる（残りの repository のどれにも Write がない）")
			}
		}
	}
	if err := s.db.UnregisterRepository(ctx, name, p.Login); errors.Is(err, settings.ErrRepositoryNotFound) {
		return problem(backend.CodeNotFound, "repository %q は登録していない", name)
	} else if err != nil {
		return err
	}
	slog.Info("repository を外した", "user", p.Login, "repository", name)
	return nil
}

func keyOut(k settings.RecordKeyInfo) api.RecordKeyOut {
	return api.RecordKeyOut{KeyID: k.ID, PublicKey: k.PublicKey, CreatedAt: k.CreatedAt, Primary: k.Primary}
}

func (s *settingsService) ListRecordKeys(ctx context.Context, token string) ([]api.RecordKeyOut, error) {
	if _, err := s.user(ctx, token, false); err != nil {
		return nil, err
	}
	ks, err := s.db.ListRecordKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.RecordKeyOut, 0, len(ks))
	for _, k := range ks {
		out = append(out, keyOut(k))
	}
	return out, nil
}

func (s *settingsService) CreateRecordKey(ctx context.Context, token string) (api.RecordKeyOut, error) {
	p, err := s.user(ctx, token, true)
	if err != nil {
		return api.RecordKeyOut{}, err
	}
	k, err := s.db.CreateRecordKey(ctx, s.keys)
	if err != nil {
		return api.RecordKeyOut{}, fmt.Errorf("記録の鍵を作れない：%w", err)
	}
	slog.Info("記録の鍵を作った", "user", p.Login, "key_id", k.ID)
	return keyOut(k), nil
}
