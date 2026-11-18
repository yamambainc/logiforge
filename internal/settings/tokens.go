package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WorkerToken は、バックエンドが worker のために作った、Read に絞った installation token である
// （design/adapters/github-app.md の「worker のトークン」）。Repositories は、トークンを絞った GitHub リポジトリの名前（owner を除く）。
type WorkerToken struct {
	Owner        string
	Token        string
	ExpiresAt    time.Time
	Repositories []string
}

// PutWorkerToken は、owner の worker のトークンを置く。共有の鍵で暗号化する。
// revision は進めない。なぜ：トークンは 20 分ごとに作り直すので、そのたびに、バックエンドと worker を作り直させないため。
func (d *DB) PutWorkerToken(ctx context.Context, keys Keys, t WorkerToken) error {
	if d.readOnly {
		return errors.New("settings: 読むだけで開いた DB には書けない")
	}
	if keys.Shared == nil {
		return errors.New("settings: worker のトークンを書くには、共有の鍵が要る")
	}
	sealed := keys.Shared.seal("worker_token."+t.Owner, []byte(t.Token))
	_, err := d.db.ExecContext(ctx, `INSERT INTO worker_tokens (owner, token, expires_at, repositories) VALUES (?, ?, ?, ?)
		ON CONFLICT(owner) DO UPDATE SET token = excluded.token, expires_at = excluded.expires_at, repositories = excluded.repositories`,
		t.Owner, sealed, t.ExpiresAt.UTC().Format(time.RFC3339), strings.Join(t.Repositories, ","))
	return err
}

// DeleteWorkerTokens は、owners にない owner の worker のトークンを消す（repository を外したとき）。
func (d *DB) DeleteWorkerTokens(ctx context.Context, keep map[string]bool) error {
	rows, err := d.db.QueryContext(ctx, `SELECT owner FROM worker_tokens`)
	if err != nil {
		return err
	}
	var drop []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			rows.Close()
			return err
		}
		if !keep[o] {
			drop = append(drop, o)
		}
	}
	rows.Close()
	for _, o := range drop {
		if _, err := d.db.ExecContext(ctx, `DELETE FROM worker_tokens WHERE owner = ?`, o); err != nil {
			return err
		}
	}
	return nil
}

// WorkerTokenOf は、owner の worker のトークンを読む。なければ ok が false。
func (d *DB) WorkerTokenOf(ctx context.Context, keys Keys, owner string) (t WorkerToken, ok bool, err error) {
	var sealed, exp, repos string
	err = d.db.QueryRowContext(ctx, `SELECT token, expires_at, repositories FROM worker_tokens WHERE owner = ?`, owner).Scan(&sealed, &exp, &repos)
	if errors.Is(err, sql.ErrNoRows) || isNoTable(err) {
		return WorkerToken{}, false, nil
	}
	if err != nil {
		return WorkerToken{}, false, err
	}
	if keys.Shared == nil {
		return WorkerToken{}, false, errors.New("settings: worker のトークンを読むには、共有の鍵が要る")
	}
	plain, err := keys.Shared.open("worker_token."+owner, sealed)
	if err != nil {
		return WorkerToken{}, false, err
	}
	at, err := time.Parse(time.RFC3339, exp)
	if err != nil {
		return WorkerToken{}, false, fmt.Errorf("settings: worker のトークンの期限の形が正しくない：%w", err)
	}
	t = WorkerToken{Owner: owner, Token: string(plain), ExpiresAt: at}
	if repos != "" {
		t.Repositories = strings.Split(repos, ",")
	}
	return t, true, nil
}
