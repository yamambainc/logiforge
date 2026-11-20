package settings

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Rekey は、すべての秘密を、old の鍵で読み、next の鍵で暗号化し直す（`logiforge settings rekey`）。
// 1 つのトランザクションで行う。途中で止まっても、前のままである。
// すでに next の鍵で書いてある行は、そのままにする（同じコマンドを 2 度動かしてもよい）。
// 動かすあいだ、バックエンドと worker を止める（design/settings/secrets.md の「鍵を取り替える」）。
func (d *DB) Rekey(ctx context.Context, old, next Keys) (int, error) {
	if old.Backend == nil || old.Shared == nil || next.Backend == nil || next.Shared == nil {
		return 0, fmt.Errorf("settings: 鍵の取り替えには、古い鍵と新しい鍵が 2 つずつ要る")
	}
	n := 0
	err := d.write(ctx, func(tx *sql.Tx) error {
		re := func(class keyClass, name, sealed string) (string, bool, error) {
			id, err := keyIDOf(sealed)
			if err != nil {
				return "", false, err
			}
			if id == next.forClass(class).ID() {
				return sealed, false, nil
			}
			plain, err := old.forClass(class).open(name, sealed)
			if err != nil {
				return "", false, err
			}
			return next.forClass(class).seal(name, plain), true, nil
		}
		classOf := map[string]keyClass{}
		for _, f := range fields {
			classOf[f.name] = f.class
		}
		type upd struct{ table, key, col, val string }
		var ups []upd
		rows, err := tx.QueryContext(ctx, `SELECT name, value FROM settings WHERE encrypted = 1`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var name, v string
			if err := rows.Scan(&name, &v); err != nil {
				rows.Close()
				return err
			}
			c, ok := classOf[name]
			if !ok {
				continue
			}
			nv, changed, err := re(c, name, v)
			if err != nil {
				rows.Close()
				return err
			}
			if changed {
				ups = append(ups, upd{"settings", name, "value", nv})
			}
		}
		rows.Close()
		for _, q := range []struct {
			sel, table, col, prefix string
			class                   keyClass
		}{
			{`SELECT key_id, private_key FROM record_keys`, "record_keys", "private_key", "record_key.", backendOnly},
			{`SELECT owner, token FROM worker_tokens`, "worker_tokens", "token", "worker_token.", shared},
		} {
			rows, err := tx.QueryContext(ctx, q.sel)
			if err != nil {
				return err
			}
			for rows.Next() {
				var k, v string
				if err := rows.Scan(&k, &v); err != nil {
					rows.Close()
					return err
				}
				nv, changed, err := re(q.class, q.prefix+k, v)
				if err != nil {
					rows.Close()
					return err
				}
				if changed {
					ups = append(ups, upd{q.table, k, q.col, nv})
				}
			}
			rows.Close()
		}
		// WebUI のセッションは、暗号化し直さずに消す。利用者は、次に開いたときに 1 度だけサインインし直す（design/webui/session.md）。
		if _, err := tx.ExecContext(ctx, `DELETE FROM webui_sessions`); err != nil && !isNoTable(err) {
			return err
		}
		keyCol := map[string]string{"settings": "name", "record_keys": "key_id", "worker_tokens": "owner"}
		for _, u := range ups {
			if _, err := tx.ExecContext(ctx, `UPDATE `+u.table+` SET `+u.col+` = ? WHERE `+keyCol[u.table]+` = ?`, u.val, u.key); err != nil {
				return err
			}
		}
		n = len(ups)
		return nil
	})
	return n, err
}

// Duration は、時間の長さの設定を読む。空なら def。
func Duration(v string, def time.Duration) time.Duration {
	if strings.TrimSpace(v) == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
