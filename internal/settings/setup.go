package settings

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrSetupDone は、初回のセットアップが、もう終わっていることである。
var ErrSetupDone = errors.New("初回のセットアップは、もう終わっている")

// SetupDone は、初回のセットアップが終わったかを返す（design/settings/setup.md）。
// 記録の鍵がすでにあるとき（`settings load` で設定を入れたとき）も、終わったとみなす。
func (d *DB) SetupDone(ctx context.Context) (bool, error) {
	var done string
	err := d.db.QueryRowContext(ctx, `SELECT done_at FROM setup WHERE id = 1`).Scan(&done)
	switch {
	case isNoTable(err):
		return false, nil
	case err == nil && done != "":
		return true, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	var n int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_keys`).Scan(&n); err != nil {
		if isNoTable(err) {
			return false, nil
		}
		return false, err
	}
	return n > 0, nil
}

// NewSetupCode は、セットアップのコードを作り直し、その SHA-256 だけを置く。前のコードは使えなくなる。
// コードは、128 ビットの乱数を base32 にしたものである（design/settings/setup.md の「セットアップのコード」）。
// セットアップが終わっていれば、ErrSetupDone を返す。
func (d *DB) NewSetupCode(ctx context.Context) (string, error) {
	if done, err := d.SetupDone(ctx); err != nil {
		return "", err
	} else if done {
		return "", ErrSetupDone
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	err := d.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO setup (id, code_hash, created_at) VALUES (1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET code_hash = excluded.code_hash, created_at = excluded.created_at`, codeHash(code), now())
		return err
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

func codeHash(code string) string {
	h := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(h[:])
}

// CheckSetupCode は、code が、今のセットアップのコードかを返す。かかる時間は、code によらない。
func (d *DB) CheckSetupCode(ctx context.Context, code string) (bool, error) {
	var have string
	err := d.db.QueryRowContext(ctx, `SELECT code_hash FROM setup WHERE id = 1`).Scan(&have)
	if errors.Is(err, sql.ErrNoRows) || isNoTable(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if have == "" {
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(have), []byte(codeHash(code))) == 1, nil
}

// Put は、names の設定だけを置き換える。ほかの設定は変えない。空の値は、その設定を消す。by は、書いた主体。
// 設定の API とセットアップの API は、Save（全体の置き換え）でなく、これを使う。なぜ：画面は秘密を返さないので、
// 送られなかった秘密を消さないため。
func (d *DB) Put(ctx context.Context, keys Keys, values map[string]string, by string) error {
	byName := map[string]field{}
	for _, f := range fields {
		byName[f.name] = f
	}
	for name := range values {
		if _, ok := byName[name]; !ok {
			return fmt.Errorf("settings: %s という設定はない", name)
		}
	}
	if g, ok := values["gitforge"]; ok && g != GitForgeForgejo && g != GitForgeGitHub {
		return fmt.Errorf("settings: gitforge は %s か %s：%q", GitForgeForgejo, GitForgeGitHub, g)
	}
	return d.write(ctx, func(tx *sql.Tx) error {
		at := now()
		for name, v := range values {
			f := byName[name]
			if v == "" {
				if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE name = ?`, name); err != nil {
					return err
				}
				continue
			}
			enc := false
			if f.class != plain {
				k := keys.forClass(f.class)
				if k == nil {
					return fmt.Errorf("settings: %s を書くには、%s が要る", name, className(f.class))
				}
				v, enc = k.seal(name, []byte(v)), true
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings (name, value, encrypted, updated_at, updated_by) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(name) DO UPDATE SET value = excluded.value, encrypted = excluded.encrypted, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
				name, v, enc, at, by); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddRepository は、repository を 1 つ登録する。同じ名前がすでにあれば、同じ組のときだけ受け付ける（外したものは登録し直す）。
func (d *DB) AddRepository(ctx context.Context, r Repository, by string) error {
	if r.Name == "" || r.GitForgeRef == "" || !strings.Contains(r.GitForgeRef, "/") {
		return fmt.Errorf("settings: repository には、名前と owner/name が要る：%+v", r)
	}
	return d.write(ctx, func(tx *sql.Tx) error {
		rs, err := loadRepositories(ctx, tx)
		if err != nil {
			return err
		}
		for _, h := range rs {
			if h.Name == r.Name {
				rs = nil
				break
			}
		}
		return saveRepositories(ctx, tx, append(rs, r), now(), by)
	})
}

// SetFirstRepository は、セットアップの最初の repository を r だけにする。前にセットアップで登録したものは外す。
// なぜ：セットアップの repository の段を、名前を変えてやり直せるようにするため。
func (d *DB) SetFirstRepository(ctx context.Context, r Repository, by string) error {
	if r.Name == "" || r.GitForgeRef == "" || !strings.Contains(r.GitForgeRef, "/") {
		return fmt.Errorf("settings: repository には、名前と owner/name が要る：%+v", r)
	}
	return d.write(ctx, func(tx *sql.Tx) error {
		return saveRepositories(ctx, tx, []Repository{r}, now(), by)
	})
}

// FinishSetup は、記録の署名の鍵を 1 つ作り、セットアップを終える。1 つのトランザクションで行う。
// すでに終わっていれば、ErrSetupDone を返す。
func (d *DB) FinishSetup(ctx context.Context, keys Keys, by string) error {
	if keys.Backend == nil {
		return errors.New("settings: セットアップを終えるには、バックエンドの鍵が要る")
	}
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	id := "rk-" + time.Now().UTC().Format("20060102")
	return d.write(ctx, func(tx *sql.Tx) error {
		var done string
		err := tx.QueryRowContext(ctx, `SELECT done_at FROM setup WHERE id = 1`).Scan(&done)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_keys`).Scan(&n); err != nil {
			return err
		}
		if done != "" || n > 0 {
			return ErrSetupDone
		}
		at := now()
		if err := saveRecordKeys(ctx, tx, keys.Backend, []RecordKey{{ID: id, Seed: seed}}, at); err != nil {
			return err
		}
		// コードは消す。セットアップが終わったら、使えなくするため。
		_, err = tx.ExecContext(ctx, `INSERT INTO setup (id, code_hash, created_at, done_at) VALUES (1, '', ?, ?)
			ON CONFLICT(id) DO UPDATE SET code_hash = '', done_at = excluded.done_at`, at, at)
		return err
	})
}
