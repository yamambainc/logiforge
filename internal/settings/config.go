package settings

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// keyClass は、設定の値をどの鍵で守るかである（design/settings/secrets.md の「秘密ごとの鍵」）。
type keyClass int

const (
	plain keyClass = iota
	backendOnly
	shared
)

// 構成（design/adapters/README.md の「組み合わせ」）。
const (
	GitForgeForgejo = "forgejo"
	GitForgeGitHub  = "github"
)

// field は、settings の表の 1 つの行である。
type field struct {
	name  string
	class keyClass
	get   func(c *Config) *string
}

// fields は、settings の表に置く設定の一覧である。repository の一覧と記録の鍵は、別の表に置く。
var fields = []field{
	{"gitforge", plain, func(c *Config) *string { return &c.GitForge }},
	{"webui_url", plain, func(c *Config) *string { return &c.WebUIURL }},
	{"forgejo.url", plain, func(c *Config) *string { return &c.Forgejo.URL }},
	{"forgejo.public_url", plain, func(c *Config) *string { return &c.Forgejo.PublicURL }},
	{"forgejo.org", plain, func(c *Config) *string { return &c.Forgejo.Org }},
	{"forgejo.worker_login", plain, func(c *Config) *string { return &c.Forgejo.WorkerLogin }},
	{"forgejo.worker_token", shared, func(c *Config) *string { return &c.Forgejo.WorkerToken }},
	{"forgejo.oauth_client_id", plain, func(c *Config) *string { return &c.Forgejo.OAuthClientID }},
	{"forgejo.oauth_client_secret", backendOnly, func(c *Config) *string { return &c.Forgejo.OAuthClientSecret }},
	{"github.api_url", plain, func(c *Config) *string { return &c.GitHub.APIURL }},
	{"github.web_url", plain, func(c *Config) *string { return &c.GitHub.WebURL }},
	{"github.app_id", plain, func(c *Config) *string { return &c.GitHub.AppID }},
	{"github.slug", plain, func(c *Config) *string { return &c.GitHub.Slug }},
	// App の秘密鍵は、バックエンドだけが読む。worker には、バックエンドが Read に絞って作った installation token だけを渡す
	// （design/adapters/github-app.md の「worker のトークン」）。
	{"github.app_private_key", backendOnly, func(c *Config) *string { return &c.GitHub.AppPrivateKey }},
	{"github.client_id", plain, func(c *Config) *string { return &c.GitHub.ClientID }},
	{"github.client_secret", backendOnly, func(c *Config) *string { return &c.GitHub.ClientSecret }},
	{"interval.patrol", plain, func(c *Config) *string { return &c.Intervals.Patrol }},
	{"interval.list", plain, func(c *Config) *string { return &c.Intervals.List }},
	{"interval.record", plain, func(c *Config) *string { return &c.Intervals.Record }},
	{"interval.git_timeout", plain, func(c *Config) *string { return &c.Intervals.GitTimeout }},
	{"interval.wait_timeout", plain, func(c *Config) *string { return &c.Intervals.WaitTimeout }},
}

// Config は、設定の DB の中身である。秘密は、読む側が持つ鍵で読めたものだけが入る（worker には、バックエンドだけの秘密は入らない）。
type Config struct {
	GitForge string `json:"gitforge"`
	// WebUIURL は、WebUI の URL である。サインインの redirect_uri は、これに /oauth/callback を足したもの。
	WebUIURL  string        `json:"webui_url,omitempty"`
	Forgejo   ForgejoConfig `json:"forgejo,omitzero"`
	GitHub    GitHubConfig  `json:"github,omitzero"`
	Intervals Intervals     `json:"intervals,omitzero"`
	// Repositories は、登録した repository（GitHub リポジトリの repository と、ディレクトリの repository）である。
	Repositories []Repository `json:"repositories,omitempty"`
	// RecordKeys は、記録の署名の鍵である。最初のものが、書くときの鍵。
	RecordKeys []RecordKey `json:"record_keys,omitempty"`
}

// ForgejoConfig は、Forgejo を使う構成の設定である。
type ForgejoConfig struct {
	URL               string `json:"url,omitempty"`
	PublicURL         string `json:"public_url,omitempty"`
	Org               string `json:"org,omitempty"`
	WorkerLogin       string `json:"worker_login,omitempty"`
	WorkerToken       string `json:"worker_token,omitempty"`
	OAuthClientID     string `json:"oauth_client_id,omitempty"`
	OAuthClientSecret string `json:"oauth_client_secret,omitempty"`
}

// GitHubConfig は、GitHub を使う構成の設定である。
type GitHubConfig struct {
	APIURL        string `json:"api_url,omitempty"`
	WebURL        string `json:"web_url,omitempty"`
	AppID         string `json:"app_id,omitempty"`
	Slug          string `json:"slug,omitempty"`
	AppPrivateKey string `json:"app_private_key,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
}

// Intervals は、時間の長さの設定である（time.ParseDuration の形）。空なら既定。
type Intervals struct {
	Patrol      string `json:"patrol,omitempty"`
	List        string `json:"list,omitempty"`
	Record      string `json:"record,omitempty"`
	GitTimeout  string `json:"git_timeout,omitempty"`
	WaitTimeout string `json:"wait_timeout,omitempty"`
}

// Repository は、登録した repository である。Directory が空でなければ、ディレクトリの repository。
type Repository struct {
	Name        string `json:"name"`
	GitForgeRef string `json:"gitforge_ref"`
	Directory   string `json:"directory,omitempty"`
}

// RecordKey は、記録の署名の鍵である。Seed は Ed25519 の種（32 バイト）で、バックエンドの鍵で読めたときだけ入る。
// 読み込みの JSON では、Seed のかわりに Secret（任意の文字列。種はその SHA-256）でも渡せる。
type RecordKey struct {
	ID     string            `json:"id"`
	Seed   []byte            `json:"seed,omitempty"`
	Secret string            `json:"secret,omitempty"`
	Public ed25519.PublicKey `json:"-"`
}

// Keys は、DB を読み書きする鍵である。worker は Shared だけを持つ。
type Keys struct {
	Backend *Key
	Shared  *Key
}

func (k Keys) forClass(c keyClass) *Key {
	switch c {
	case backendOnly:
		return k.Backend
	case shared:
		return k.Shared
	}
	return nil
}

// Load は、設定を読む。読めない鍵の秘密（worker にとってのバックエンドだけの秘密）は、空のままにする。
// DB に表がまだなければ（バックエンドが一度も開いていない）、空の設定を返す。
func (d *DB) Load(ctx context.Context, keys Keys) (*Config, error) {
	c := &Config{}
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT name, value, encrypted FROM settings`)
	if err != nil {
		if isNoTable(err) {
			return c, nil
		}
		return nil, err
	}
	byName := map[string]field{}
	for _, f := range fields {
		byName[f.name] = f
	}
	for rows.Next() {
		var name, value string
		var enc bool
		if err := rows.Scan(&name, &value, &enc); err != nil {
			rows.Close()
			return nil, err
		}
		f, ok := byName[name]
		if !ok {
			continue
		}
		if enc {
			k := keys.forClass(f.class)
			if k == nil {
				continue
			}
			plain, err := k.open(name, value)
			if err != nil {
				rows.Close()
				return nil, err
			}
			value = string(plain)
		}
		*f.get(c) = value
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if c.Repositories, err = loadRepositories(ctx, tx); err != nil {
		return nil, err
	}
	if c.RecordKeys, err = loadRecordKeys(ctx, tx, keys.Backend); err != nil {
		return nil, err
	}
	return c, nil
}

func loadRepositories(ctx context.Context, tx *sql.Tx) ([]Repository, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name, gitforge_ref, directory FROM repositories WHERE removed = 0 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Repository
	for rows.Next() {
		var r Repository
		if err := rows.Scan(&r.Name, &r.GitForgeRef, &r.Directory); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadRecordKeys は、記録の署名の鍵を、書くときの鍵を先頭にして読む。backend がなければ、公開鍵だけを読む。
func loadRecordKeys(ctx context.Context, tx *sql.Tx, backend *Key) ([]RecordKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT key_id, public_key, private_key FROM record_keys ORDER BY is_primary DESC, created_at, key_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordKey
	for rows.Next() {
		var id, pub, priv string
		if err := rows.Scan(&id, &pub, &priv); err != nil {
			return nil, err
		}
		k := RecordKey{ID: id}
		if k.Public, err = base64.StdEncoding.DecodeString(pub); err != nil || len(k.Public) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("settings: 記録の鍵 %s の公開鍵の形が正しくない", id)
		}
		if backend != nil {
			if k.Seed, err = backend.open("record_key."+id, priv); err != nil {
				return nil, err
			}
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Save は、c を設定の DB に書く。渡した設定の全体で置き換える。空の値は、その設定を消す。by は、書いた主体（コマンドなら空でもよい）。
// `logiforge settings load` のためのものである。設定の API からは使わない。
// なぜ：API と画面は秘密を返さないので、読んだものを書き直すと、秘密が消えるから。API には、送った項目だけを置き換える書き方を別に作る。
// repository の一覧は、c にあるものを足し（同じ名前なら同じ組であることを確かめる）、c にないものは外す。
// 記録の鍵は、足すだけで、消さない（それまでの記録を確かめるため）。
func (d *DB) Save(ctx context.Context, keys Keys, c *Config, by string) error {
	if err := c.check(); err != nil {
		return err
	}
	return d.write(ctx, func(tx *sql.Tx) error {
		at := now()
		for _, f := range fields {
			v := *f.get(c)
			if v == "" {
				if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE name = ?`, f.name); err != nil {
					return err
				}
				continue
			}
			enc := false
			if f.class != plain {
				k := keys.forClass(f.class)
				if k == nil {
					return fmt.Errorf("settings: %s を書くには、%s が要る", f.name, className(f.class))
				}
				v, enc = k.seal(f.name, []byte(v)), true
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings (name, value, encrypted, updated_at, updated_by) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(name) DO UPDATE SET value = excluded.value, encrypted = excluded.encrypted, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
				f.name, v, enc, at, by); err != nil {
				return err
			}
		}
		if err := saveRepositories(ctx, tx, c.Repositories, at, by); err != nil {
			return err
		}
		return saveRecordKeys(ctx, tx, keys.Backend, c.RecordKeys, at)
	})
}

func className(c keyClass) string {
	if c == backendOnly {
		return "バックエンドの鍵"
	}
	return "共有の鍵"
}

// saveRepositories は、登録した repository を書く。登録したあとの名前と gitforge_ref とディレクトリの組は、変えられない。
// 外した名前を登録し直せるのは、同じ組のときだけである（design/api/endpoints/settings.md の「repository を外したとき」）。
func saveRepositories(ctx context.Context, tx *sql.Tx, rs []Repository, at, by string) error {
	want := map[string]Repository{}
	for _, r := range rs {
		want[r.Name] = r
	}
	rows, err := tx.QueryContext(ctx, `SELECT name, gitforge_ref, directory, removed FROM repositories`)
	if err != nil {
		return err
	}
	type row struct {
		Repository
		removed bool
	}
	have := map[string]row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Name, &r.GitForgeRef, &r.Directory, &r.removed); err != nil {
			rows.Close()
			return err
		}
		have[r.Name] = r
	}
	rows.Close()
	for name, w := range want {
		h, ok := have[name]
		if ok && (h.GitForgeRef != w.GitForgeRef || h.Directory != w.Directory) {
			return fmt.Errorf("settings: repository %s は %s%s で登録したので、%s%s には変えられない", name, h.GitForgeRef, dirSuffix(h.Directory), w.GitForgeRef, dirSuffix(w.Directory))
		}
		if ok && !h.removed {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO repositories (name, gitforge_ref, directory, removed, updated_at, updated_by) VALUES (?, ?, ?, 0, ?, ?)
			ON CONFLICT(name) DO UPDATE SET removed = 0, updated_at = excluded.updated_at, updated_by = excluded.updated_by`, name, w.GitForgeRef, w.Directory, at, by); err != nil {
			return err
		}
	}
	for name, h := range have {
		if _, ok := want[name]; ok || h.removed {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE repositories SET removed = 1, updated_at = ?, updated_by = ? WHERE name = ?`, at, by, name); err != nil {
			return err
		}
	}
	return nil
}

func dirSuffix(d string) string {
	if d == "" {
		return ""
	}
	return ":" + d
}

// minSecretLen は、記録の鍵の secret の短さの下限（バイト）である。
const minSecretLen = 32

// saveRecordKeys は、まだない記録の鍵を足す。先頭の鍵を、書くときの鍵にする。
func saveRecordKeys(ctx context.Context, tx *sql.Tx, backend *Key, ks []RecordKey, at string) error {
	if len(ks) == 0 {
		return nil
	}
	if backend == nil {
		return errors.New("settings: 記録の鍵を書くには、バックエンドの鍵が要る")
	}
	for i, k := range ks {
		seed := k.Seed
		if len(seed) == 0 && k.Secret != "" {
			// 短い文字列から作ると、弱い鍵になる。32 バイト以上に限る。
			if len(k.Secret) < minSecretLen {
				return fmt.Errorf("settings: 記録の鍵 %s の secret は、%d バイト以上の乱数にする", k.ID, minSecretLen)
			}
			h := sha256.Sum256([]byte(k.Secret))
			seed = h[:]
		}
		if len(seed) != ed25519.SeedSize {
			return fmt.Errorf("settings: 記録の鍵 %s には、32 バイトの seed か secret が要る", k.ID)
		}
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		var have string
		err := tx.QueryRowContext(ctx, `SELECT public_key FROM record_keys WHERE key_id = ?`, k.ID).Scan(&have)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, `INSERT INTO record_keys (key_id, public_key, private_key, created_at, is_primary) VALUES (?, ?, ?, ?, 0)`,
				k.ID, base64.StdEncoding.EncodeToString(pub), backend.seal("record_key."+k.ID, seed), at); err != nil {
				return err
			}
		case err != nil:
			return err
		case have != base64.StdEncoding.EncodeToString(pub):
			return fmt.Errorf("settings: 記録の鍵 %s は、ほかの鍵で登録してある。鍵の ID を変える", k.ID)
		}
		if i == 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE record_keys SET is_primary = (key_id = ?)`, k.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// check は、書く前に、値の形を確かめる。
func (c *Config) check() error {
	switch c.GitForge {
	case GitForgeForgejo, GitForgeGitHub:
	default:
		return fmt.Errorf("settings: gitforge は %s か %s：%q", GitForgeForgejo, GitForgeGitHub, c.GitForge)
	}
	for _, f := range []struct{ name, v string }{{"patrol", c.Intervals.Patrol}, {"list", c.Intervals.List}, {"record", c.Intervals.Record},
		{"git_timeout", c.Intervals.GitTimeout}, {"wait_timeout", c.Intervals.WaitTimeout}} {
		if f.v == "" {
			continue
		}
		if d, err := time.ParseDuration(f.v); err != nil || d <= 0 {
			return fmt.Errorf("settings: intervals.%s は正の長さ（例：10s）にする：%q", f.name, f.v)
		}
	}
	seen := map[string]bool{}
	for _, r := range c.Repositories {
		if r.Name == "" || r.GitForgeRef == "" || !strings.Contains(r.GitForgeRef, "/") {
			return fmt.Errorf("settings: repository には、名前と owner/name が要る：%+v", r)
		}
		if seen[r.Name] {
			return fmt.Errorf("settings: repository %s が 2 つある", r.Name)
		}
		seen[r.Name] = true
	}
	return nil
}

// ErrNotReady は、設定が足りない（初回のセットアップが終わっていない）ことである。
// 読みの失敗（ロックの待ちの上限、I/O の誤り）とは分ける。足りないときは、設定が書かれるまで待つ。読みの失敗は、やり直す。
var ErrNotReady = errors.New("初回のセットアップが終わっていない")

// Ready は、セットアップが終わり、動かせる設定がそろっているかである。そろっていなければ、ErrNotReady を包んで、足りないものを返す。
func (c *Config) Ready() error {
	var miss []string
	switch c.GitForge {
	case GitForgeForgejo:
		for n, v := range map[string]string{"forgejo.url": c.Forgejo.URL, "forgejo.org": c.Forgejo.Org, "forgejo.worker_login": c.Forgejo.WorkerLogin} {
			if v == "" {
				miss = append(miss, n)
			}
		}
	case GitForgeGitHub:
		if len(c.Repositories) == 0 {
			miss = append(miss, "repositories")
		}
	default:
		miss = append(miss, "gitforge")
	}
	if len(c.RecordKeys) == 0 {
		miss = append(miss, "record_keys")
	}
	if len(miss) == 0 {
		return nil
	}
	sort.Strings(miss)
	return fmt.Errorf("%w：設定の DB に、%s がない", ErrNotReady, strings.Join(miss, "、"))
}
