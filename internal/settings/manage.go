package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Changed は、設定を最後に変えた利用者と時刻である（design/settings/users.md の「記録」）。
type Changed struct {
	By string
	At string
}

// Changes は、settings の表の行ごとの、最後に変えた利用者と時刻と、設定の全体（settings、repositories）で最後のものを返す。
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
	for _, q := range []string{`SELECT updated_at, updated_by FROM repositories ORDER BY updated_at DESC LIMIT 1`} {
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

// Apply は、values（設定の名前と値。Put に渡すもの）を、c に当てる。書く前に、当てたあとの設定で確かめるために使う。
func (c *Config) Apply(values map[string]string) {
	for _, f := range fields {
		if v, ok := values[f.name]; ok {
			*f.get(c) = v
		}
	}
}

// Clone は、c の写しを返す。repository の一覧も写す。
func (c *Config) Clone() *Config {
	out := *c
	out.Repositories = append([]Repository(nil), c.Repositories...)
	return &out
}
