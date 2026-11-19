package e2e

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/settings"
)

// TestWebUIAddRepository は、設定の画面で、ディレクトリの repository を足す流れをブラウザで通す（docs/webui/settings.md の「repository を足す」）。
// スナップショットツリーがもうあるディレクトリでは、最初の問いを飛ばす。新しいディレクトリでは、同じ画面で最初の問いを入れる。
// ブラウザの操作は webui/e2e/add.mjs が行う。Forgejo に新しく作る流れは、webui/e2e/run.mjs が通す。
func TestWebUIAddRepository(t *testing.T) {
	chrome, out := browserEnv(t)
	const project, old, fresh = "proj", "old", "fresh"
	w := setupFull(t, func(w *world, c *settings.Config) {
		c.Repositories = []settings.Repository{{Name: old, GitForgeRef: w.org + "/" + project, Directory: "logiforge/" + old}}
	}, nil)
	ref := w.org + "/" + project
	w.forgejoCall(w.admin, "POST", "/orgs/"+w.org+"/repos", map[string]any{"name": project, "auto_init": true, "default_branch": "main", "private": true}, nil, 201)
	w.forgejoCall(w.admin, "PUT", fmt.Sprintf("/repos/%s/collaborators/%s", ref, w.login("alice")), map[string]string{"permission": "write"}, nil, 204)
	// old のディレクトリを用意してから外す。ブラウザで登録し直すと、スナップショットツリーがもうある
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": old, "statement": "前からある問い"}); st != 201 && st != 202 {
		t.Fatalf("%s の作成：%d %s", old, st, b)
	}
	var list struct {
		Items []struct {
			Name     string `json:"name"`
			Prepared *bool  `json:"prepared"`
		} `json:"items"`
	}
	st, b := w.call("alice", "GET", "/settings/repositories", nil)
	if st != 200 || json.Unmarshal(b, &list) != nil || len(list.Items) != 1 || list.Items[0].Prepared == nil || !*list.Items[0].Prepared {
		t.Fatalf("登録した repository の一覧の prepared：%d %s", st, b)
	}
	if st, b := w.call("alice", "DELETE", "/settings/repositories/"+old, nil); st != 204 {
		t.Fatalf("%s を外す：%d %s", old, st, b)
	}

	login := w.login("alice")
	runBrowser(t, "e2e/add.mjs", "BASE="+w.api, "LOGIN="+login, "PASSWORD=pw-"+login+"-12345", "CHROME="+chrome, "OUT="+out,
		"PROJECT="+ref, "OLD="+old, "FRESH="+fresh)
	w.eventually(fresh+" のスナップショットツリーが出る", 60*time.Second, func() bool {
		st, b := w.call("alice", "GET", "/repositories/"+fresh, nil)
		var r struct {
			RootStatement *string `json:"root_statement"`
			Directory     string  `json:"directory"`
		}
		return st == 200 && json.Unmarshal(b, &r) == nil && r.RootStatement != nil && *r.RootStatement == "ディレクトリの問い" && r.Directory == "logiforge/"+fresh
	})
}
