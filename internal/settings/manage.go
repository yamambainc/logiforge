package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Changed は、設定を最後に変えた利用者と時刻である（design/settings/users.md の「記録」）。
type Changed struct {
	By string
	At string
}

// Changes は、settings の表の行ごとの、最後に変えた利用者と時刻と、設定の全体（settings、repositories、record_keys）で最後のものを返す。
func (d *DB) Changes(ctx context.Context) (map[string]Changed, Changed, error) {
	out := map[string]Changed{}
	var last Changed
	keep := func(c Changed) {
		// 時刻は秒までなので、同じ秒なら、利用者の分かるほうを選ぶ。
		if c.At > last.At || c.At == last.At && last.By == "" {
			last = c
		}
	}
	rows, err := d.db.QueryContext(ctx, `SELECT name, updated_at, updated_by FROM settings`)
	if err != nil {
		return nil, last, err
	}
	for rows.Next() {
		var n string
		var c Changed
		if err := rows.Scan(&n, &c.At, &c.By); err != nil {
			rows.Close()
			return nil, last, err
		}
		out[n] = c
		keep(c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, last, err
	}
	for _, q := range []string{`SELECT updated_at, updated_by FROM repositories ORDER BY updated_at DESC LIMIT 1`,
		`SELECT created_at, '' FROM record_keys ORDER BY created_at DESC LIMIT 1`} {
		var c Changed
		if err := d.db.QueryRowContext(ctx, q).Scan(&c.At, &c.By); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, last, err
		}
		keep(c)
	}
	return out, last, nil
}

// ErrRepositoryConflict は、登録済みか外した名前を、別の gitforge_ref かディレクトリで登録しようとしたことである
// （design/api/endpoints/settings.md の「repository を外したとき」）。
var ErrRepositoryConflict = errors.New("settings: この名前は、別の git リポジトリかディレクトリで登録してある")

// ErrRepositoryNotFound は、登録していない repository である。
var ErrRepositoryNotFound = errors.New("settings: 登録していない repository")

// RegisterRepository は、repository を 1 つ登録する。ほかの登録は変えない。
// 同じ名前が同じ組で登録してあれば、何もしない。外したものなら、登録し直す。別の組なら、ErrRepositoryConflict を返す。
func (d *DB) RegisterRepository(ctx context.Context, r Repository, by string) error {
	if r.Name == "" || r.GitForgeRef == "" || !strings.Contains(r.GitForgeRef, "/") {
		return fmt.Errorf("settings: repository には、名前と owner/name が要る：%+v", r)
	}
	return d.write(ctx, func(tx *sql.Tx) error {
		var ref, dir string
		var removed bool
		err := tx.QueryRowContext(ctx, `SELECT gitforge_ref, directory, removed FROM repositories WHERE name = ?`, r.Name).Scan(&ref, &dir, &removed)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			_, err = tx.ExecContext(ctx, `INSERT INTO repositories (name, gitforge_ref, directory, removed, updated_at, updated_by) VALUES (?, ?, ?, 0, ?, ?)`,
				r.Name, r.GitForgeRef, r.Directory, now(), by)
			return err
		case err != nil:
			return err
		case ref != r.GitForgeRef || dir != r.Directory:
			return fmt.Errorf("%w（%s は %s%s で登録した）", ErrRepositoryConflict, r.Name, ref, dirSuffix(dir))
		case !removed:
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE repositories SET removed = 0, updated_at = ?, updated_by = ? WHERE name = ?`, now(), by, r.Name)
		return err
	})
}

// UnregisterRepository は、repository を外す。名前と組は残す（同じ組でだけ、登録し直せるように）。
func (d *DB) UnregisterRepository(ctx context.Context, name, by string) error {
	return d.write(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE repositories SET removed = 1, updated_at = ?, updated_by = ? WHERE name = ? AND removed = 0`, now(), by, name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrRepositoryNotFound
		}
		return nil
	})
}

// RecordKeyInfo は、記録の署名の鍵の、秘密でないものである。
type RecordKeyInfo struct {
	ID        string
	PublicKey string // base64
	CreatedAt string
	Primary   bool
}

// ListRecordKeys は、記録の署名の鍵を、書くときの鍵を先頭にして返す。秘密鍵は読まない。
func (d *DB) ListRecordKeys(ctx context.Context) ([]RecordKeyInfo, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT key_id, public_key, created_at, is_primary FROM record_keys ORDER BY is_primary DESC, created_at, key_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordKeyInfo
	for rows.Next() {
		var k RecordKeyInfo
		if err := rows.Scan(&k.ID, &k.PublicKey, &k.CreatedAt, &k.Primary); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// CreateRecordKey は、新しい記録の署名の鍵を作り、書くときの鍵にする。古い鍵は消さない（それまでの記録を確かめるため）。
func (d *DB) CreateRecordKey(ctx context.Context, keys Keys) (RecordKeyInfo, error) {
	if keys.Backend == nil {
		return RecordKeyInfo{}, errors.New("settings: 記録の鍵を作るには、バックエンドの鍵が要る")
	}
	seed := make([]byte, 32)
	suffix := make([]byte, 3)
	if _, err := rand.Read(seed); err != nil {
		return RecordKeyInfo{}, err
	}
	if _, err := rand.Read(suffix); err != nil {
		return RecordKeyInfo{}, err
	}
	id := "rk-" + time.Now().UTC().Format("20060102") + "-" + hex.EncodeToString(suffix)
	if err := d.write(ctx, func(tx *sql.Tx) error {
		return saveRecordKeys(ctx, tx, keys.Backend, []RecordKey{{ID: id, Seed: seed}}, now())
	}); err != nil {
		return RecordKeyInfo{}, err
	}
	ks, err := d.ListRecordKeys(ctx)
	if err != nil {
		return RecordKeyInfo{}, err
	}
	for _, k := range ks {
		if k.ID == id {
			return k, nil
		}
	}
	return RecordKeyInfo{}, fmt.Errorf("settings: 作った記録の鍵 %s を読めない", id)
}

// Apply は、values（設定の名前と値。Put に渡すもの）を、c に当てる。書く前に、当てたあとの設定で確かめるために使う。
func (c *Config) Apply(values map[string]string) {
	for _, f := range fields {
		if v, ok := values[f.name]; ok {
			*f.get(c) = v
		}
	}
}

// Clone は、c の写しを返す。repository の一覧と記録の鍵も写す。
func (c *Config) Clone() *Config {
	out := *c
	out.Repositories = append([]Repository(nil), c.Repositories...)
	out.RecordKeys = append([]RecordKey(nil), c.RecordKeys...)
	return &out
}
