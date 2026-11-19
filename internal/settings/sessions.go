package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// WebUISession は、WebUI のサインインのセッションである（design/webui/session.md）。
// トークンは、バックエンドの鍵で暗号化して置く。セッション ID は置かず、その SHA-256（IDHash）だけを置く。
type WebUISession struct {
	IDHash string
	// ClientID は、セッションを作ったときのサインインのアプリの client_id。変わったら、そのセッションは使わない。
	ClientID      string
	AccessToken   string
	AccessExpires time.Time
	RefreshToken  string
	CreatedAt     time.Time
	UsedAt        time.Time
}

// SessionMaxIdle は、使われないまま置いておくセッションの長さである。これより前に使ったセッションは、書くときに消す。
const SessionMaxIdle = 180 * 24 * time.Hour

func sessionName(idHash, what string) string { return "webui_session." + idHash + "." + what }

// PutWebUISession は、セッションを置く（同じ IDHash があれば置き換える）。バックエンドの鍵で暗号化する。
// revision は進めない。なぜ：セッションは設定ではなく、worker は読まないから。
// あわせて、SessionMaxIdle より長く使われていないセッションを消す。
func (d *DB) PutWebUISession(ctx context.Context, keys Keys, s WebUISession) error {
	if d.readOnly {
		return errors.New("settings: 読むだけで開いた DB には書けない")
	}
	if keys.Backend == nil {
		return errors.New("settings: セッションを書くには、バックエンドの鍵が要る")
	}
	at := keys.Backend.seal(sessionName(s.IDHash, "access"), []byte(s.AccessToken))
	rt := keys.Backend.seal(sessionName(s.IDHash, "refresh"), []byte(s.RefreshToken))
	f := func(t time.Time) string { return t.UTC().Format(time.RFC3339) }
	if _, err := d.db.ExecContext(ctx, `INSERT INTO webui_sessions (id_hash, client_id, access_token, access_expires_at, refresh_token, created_at, used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id_hash) DO UPDATE SET client_id = excluded.client_id, access_token = excluded.access_token,
			access_expires_at = excluded.access_expires_at, refresh_token = excluded.refresh_token, used_at = excluded.used_at`,
		s.IDHash, s.ClientID, at, f(s.AccessExpires), rt, f(s.CreatedAt), f(s.UsedAt)); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM webui_sessions WHERE used_at < ?`, f(time.Now().Add(-SessionMaxIdle)))
	return err
}

// WebUISessionOf は、セッションを読む。なければ ok が false。
func (d *DB) WebUISessionOf(ctx context.Context, keys Keys, idHash string) (s WebUISession, ok bool, err error) {
	var at, aexp, rt, created, used string
	err = d.db.QueryRowContext(ctx, `SELECT client_id, access_token, access_expires_at, refresh_token, created_at, used_at FROM webui_sessions WHERE id_hash = ?`,
		idHash).Scan(&s.ClientID, &at, &aexp, &rt, &created, &used)
	if errors.Is(err, sql.ErrNoRows) || isNoTable(err) {
		return WebUISession{}, false, nil
	}
	if err != nil {
		return WebUISession{}, false, err
	}
	if keys.Backend == nil {
		return WebUISession{}, false, errors.New("settings: セッションを読むには、バックエンドの鍵が要る")
	}
	// 復号できなければ（鍵を入れ替えた、rekey と競合した）、知らないセッションとして扱い、消す。
	// なぜ：エラーを返し続けると、その端末は、cookie が切れるまで authorize にも行けないから。
	a, err := keys.Backend.open(sessionName(idHash, "access"), at)
	if err != nil {
		return WebUISession{}, false, d.DeleteWebUISession(ctx, idHash)
	}
	r, err := keys.Backend.open(sessionName(idHash, "refresh"), rt)
	if err != nil {
		return WebUISession{}, false, d.DeleteWebUISession(ctx, idHash)
	}
	s.IDHash, s.AccessToken, s.RefreshToken = idHash, string(a), string(r)
	for _, x := range []struct {
		v string
		t *time.Time
	}{{aexp, &s.AccessExpires}, {created, &s.CreatedAt}, {used, &s.UsedAt}} {
		if *x.t, err = time.Parse(time.RFC3339, x.v); err != nil {
			return WebUISession{}, false, fmt.Errorf("settings: セッションの時刻の形が正しくない：%w", err)
		}
	}
	return s, true, nil
}

// DeleteWebUISession は、セッションを消す。なければ何もしない。
func (d *DB) DeleteWebUISession(ctx context.Context, idHash string) error {
	if d.readOnly {
		return errors.New("settings: 読むだけで開いた DB には書けない")
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM webui_sessions WHERE id_hash = ?`, idHash)
	return err
}
