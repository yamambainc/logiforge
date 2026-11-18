package forgejo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ErrWorkerTokenWrites は、worker のトークンで書けることである。worker に渡すトークンは、書けないものに限る
// （design/settings/secrets.md の「秘密ごとの鍵」）。
var ErrWorkerTokenWrites = errors.New("forgejo: worker のトークンで書ける")

// CheckWorkerToken は、worker のユーザーのトークンを置く前に、読めて、書けないことを確かめる
// （design/adapters/forgejo.md の「worker のトークンを確かめる」）。
//   - organization を読める。トークンは read:user を持たないので、GET /user は呼べない。
//   - 書くスコープの要る、害のない要求（本文が空）が 403 になる。Forgejo は、スコープの足りないトークンを、本文を確かめる前に 403 で断る。
//     403 なら書けない、404 はどちらともみなさない、それ以外（スコープを通った応答）なら書けるとみなす。
//   - worker のユーザーが、organization で read より強い team に入っていない。
func CheckWorkerToken(ctx context.Context, cfg Config, login, token string) error {
	c := &client{cfg: cfg}
	if err := c.do(ctx, token, http.MethodGet, "/orgs/"+url.PathEscape(cfg.Org), nil, nil); err != nil {
		return fmt.Errorf("forgejo: worker のトークンで、organization %s を読めない：%w", cfg.Org, err)
	}
	probes := []string{"/user/settings"}
	var repos []struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, token, http.MethodGet, "/orgs/"+url.PathEscape(cfg.Org)+"/repos?limit=1", nil, &repos); err != nil {
		return fmt.Errorf("forgejo: worker のトークンで、organization の repository を読めない：%w", err)
	}
	if len(repos) > 0 {
		// organization に repository がまだなければ、この要求は飛ばす。
		probes = append(probes, "/repos/"+url.PathEscape(cfg.Org)+"/"+url.PathEscape(repos[0].Name)+"/issues")
	}
	for _, p := range probes {
		method := http.MethodPost
		if p == "/user/settings" {
			method = http.MethodPatch
		}
		err := c.do(ctx, token, method, p, map[string]any{}, nil)
		switch s := statusOf(err); {
		case s == http.StatusForbidden, s == http.StatusNotFound:
		case err != nil && s == 0:
			return fmt.Errorf("forgejo: worker のトークンを確かめられない：%w", err)
		default:
			return fmt.Errorf("%w（%s %s が %d を返した。read:repository、read:organization、read:issue だけのトークンにする）", ErrWorkerTokenWrites, method, p, s)
		}
	}
	// worker のユーザーの team は、organization の team と、そのメンバーから引く（read:organization で読める）。
	type team struct {
		orgTeam
		ID int64 `json:"id"`
	}
	teams, err := list[team](ctx, c, token, "/orgs/"+url.PathEscape(cfg.Org)+"/teams")
	if err != nil {
		return fmt.Errorf("forgejo: organization の team を読めない：%w", err)
	}
	for _, t := range teams {
		writes := t.Permission != "read" && t.Permission != "none"
		for _, p := range t.UnitsMap {
			if p == "write" || p == "admin" {
				writes = true
			}
		}
		if !writes {
			continue
		}
		// Forgejo は、team のメンバーの一覧を、その team のメンバーにだけ見せる。403 なら、worker のユーザーはその team にいない。
		members, err := list[user](ctx, c, token, fmt.Sprintf("/teams/%d/members", t.ID))
		if statusOf(err) == http.StatusForbidden {
			continue
		}
		if err != nil {
			return fmt.Errorf("forgejo: team %s のメンバーを読めない：%w", t.Name, err)
		}
		for _, m := range members {
			if m.Login == login {
				return fmt.Errorf("%w（worker のユーザー %s が、書ける team %s にいる。読むだけの team にだけ入れる）", ErrWorkerTokenWrites, login, t.Name)
			}
		}
	}
	return nil
}
