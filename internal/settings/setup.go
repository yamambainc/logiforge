package settings

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
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

// ErrSetupStarted は、初回のセットアップを、ほかのブラウザがもう始めたことである。
var ErrSetupStarted = errors.New("初回のセットアップは、ほかのブラウザで進んでいる")

// StartSetup は、初回のセットアップを始め、セットアップのトークンを返す。DB には、トークンの SHA-256 だけを置く。
// トークンを持つブラウザだけが、セットアップを続けられる（design/settings/setup.md の「だれがセットアップできるか」）。
// もう始まっていれば ErrSetupStarted、終わっていれば ErrSetupDone を返す。
func (d *DB) StartSetup(ctx context.Context) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	err := d.write(ctx, func(tx *sql.Tx) error {
		var hash, done string
		err := tx.QueryRowContext(ctx, `SELECT token_hash, done_at FROM setup WHERE id = 1`).Scan(&hash, &done)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		case done != "":
			return ErrSetupDone
		case hash != "":
			return ErrSetupStarted
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM record_keys`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupDone
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO setup (id, token_hash, created_at) VALUES (1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET token_hash = excluded.token_hash, created_at = excluded.created_at`, tokenHash(tok), now())
		return err
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

func tokenHash(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// SetupStarted は、初回のセットアップが、もう始まったかを返す。
func (d *DB) SetupStarted(ctx context.Context) (bool, error) {
	var hash string
	err := d.db.QueryRowContext(ctx, `SELECT token_hash FROM setup WHERE id = 1`).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) || isNoTable(err) {
		return false, nil
	}
	return hash != "", err
}

// CheckSetupToken は、tok が、セットアップを始めたブラウザのトークンかを返す。かかる時間は、tok によらない。
func (d *DB) CheckSetupToken(ctx context.Context, tok string) (bool, error) {
	var hash string
	err := d.db.QueryRowContext(ctx, `SELECT token_hash FROM setup WHERE id = 1`).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) || isNoTable(err) {
		return false, nil
	}
	if err != nil || hash == "" || tok == "" {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(tokenHash(tok))) == 1, nil
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
		// トークンは消す。セットアップが終わったら、使えなくするため。
		_, err = tx.ExecContext(ctx, `INSERT INTO setup (id, created_at, done_at) VALUES (1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET token_hash = '', done_at = excluded.done_at`, at, at)
		return err
	})
}
