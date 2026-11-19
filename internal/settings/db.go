package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // SQLite（cgo を使わない。リリースのバイナリは CGO_ENABLED=0 で作る）
)

// schemaVersion は、DB の中の表の形の版である。値の形を変えるときは上げ、migrate で移す（design/settings/database.md の「表」）。
const schemaVersion = 1

// ErrNoDB は、読むだけで開こうとした設定の DB のファイルが、まだないことである。
var ErrNoDB = errors.New("設定の DB のファイルがない")

// DB は、設定の DB である。
type DB struct {
	db       *sql.DB
	readOnly bool
}

// Open は、設定の DB を開く。readOnly なら、読むだけで開く（worker）。
//
// ジャーナルは rollback journal（DELETE）にする。WAL にしない。なぜ：WAL では、読むだけの接続も -shm のファイルに書くので、
// worker に書く権限を渡すことになるから（design/settings/database.md の「DB の種類」）。
func Open(path string, readOnly bool) (*DB, error) {
	if readOnly {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w：%s", ErrNoDB, path)
		}
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	if readOnly {
		q.Set("mode", "ro")
	} else {
		q.Add("_pragma", "journal_mode(DELETE)")
		q.Add("_pragma", "synchronous(FULL)")
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: url.PathEscape(path), RawQuery: q.Encode()}).String())
	if err != nil {
		return nil, err
	}
	// 1 つの接続だけを使う。なぜ：SQLite の書き込みは 1 つずつで、接続を増やしても速くならず、ロックの待ちが増えるだけだから。
	db.SetMaxOpenConns(1)
	d := &DB{db: db, readOnly: readOnly}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("設定の DB %s を開けない：%w", path, err)
	}
	if !readOnly {
		if err := d.migrate(context.Background()); err != nil {
			db.Close()
			return nil, err
		}
	}
	return d, nil
}

// Close は、DB を閉じる。
func (d *DB) Close() error { return d.db.Close() }

func (d *DB) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (name TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS settings (name TEXT PRIMARY KEY, value TEXT NOT NULL, encrypted INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL, updated_by TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS repositories (name TEXT PRIMARY KEY, gitforge_ref TEXT NOT NULL, directory TEXT NOT NULL DEFAULT '',
			removed INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS record_keys (key_id TEXT PRIMARY KEY, public_key TEXT NOT NULL, private_key TEXT NOT NULL,
			created_at TEXT NOT NULL, is_primary INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS worker_tokens (owner TEXT PRIMARY KEY, token TEXT NOT NULL, expires_at TEXT NOT NULL, repositories TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS setup (id INTEGER PRIMARY KEY CHECK (id = 1), created_at TEXT NOT NULL, done_at TEXT NOT NULL DEFAULT '')`,
	}
	return d.write(ctx, func(tx *sql.Tx) error {
		for _, s := range stmts {
			if _, err := tx.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("設定の DB の表を作れない：%w", err)
			}
		}
		var v string
		err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE name = 'schema_version'`).Scan(&v)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			_, err = tx.ExecContext(ctx, `INSERT INTO meta (name, value) VALUES ('schema_version', ?), ('revision', '0')`, strconv.Itoa(schemaVersion))
			return err
		case err != nil:
			return err
		}
		if n, _ := strconv.Atoi(v); n > schemaVersion {
			return fmt.Errorf("設定の DB の版（%s）が、このバイナリ（%d）より新しい。新しいバイナリで動かす", v, schemaVersion)
		}
		return nil
	})
}

// write は、1 つのトランザクションで書き、revision を 1 つ進める。
// revision は、バックエンドと worker が、設定が変わったかを知るのに使う（design/settings/README.md の「設定を読むとき」）。
func (d *DB) write(ctx context.Context, f func(tx *sql.Tx) error) error {
	if d.readOnly {
		return errors.New("settings: 読むだけで開いた DB には書けない")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE name = 'revision'`); err != nil {
		return err
	}
	return tx.Commit()
}

// Revision は、設定の版である。書くたびに増える。DB がまだ作られていなければ 0。
func (d *DB) Revision(ctx context.Context) (int64, error) {
	var v string
	err := d.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE name = 'revision'`).Scan(&v)
	if err != nil {
		if isNoTable(err) || errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// isNoTable は、表がまだない（バックエンドが一度も DB を作っていない）ことである。worker は、読むだけで開くので作れない。
func isNoTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
