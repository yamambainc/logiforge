package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/settings"
)

// 別リポジトリ方式から、ディレクトリの repository に移す（design/directory/migrate.md）。
//   - 元の git リポジトリが archive でなければ、移せない。
//   - 移すと、移した先は、元の今のスナップショットツリーと同じになり、migrated_from を持つ。元は migrated_to を持ち、書けない。
func TestE2EMigrate(t *testing.T) {
	const project = "proj"
	w := setupFull(t, func(w *world, c *settings.Config) {
		c.Repositories = []settings.Repository{{Name: "moved", GitForgeRef: w.org + "/" + project, Directory: "logiforge/moved"}}
	}, nil)
	ref := w.org + "/" + project
	w.forgejoCall(w.admin, "POST", "/orgs/"+w.org+"/repos", map[string]any{"name": project, "auto_init": true, "default_branch": "main", "private": true}, nil, 201)
	w.forgejoCall(w.admin, "PUT", fmt.Sprintf("/repos/%s/collaborators/%s", ref, w.login("alice")), map[string]string{"permission": "write"}, nil, 204)

	// 元の repository（別リポジトリ方式）に、背景情報を 1 つ反映しておく。
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "old", "statement": "移す前の問い"}); st != 201 && st != 202 {
		t.Fatalf("old の作成：%d %s", st, b)
	}
	w.eventually("old のスナップショットツリーが出る", 60*time.Second, func() bool {
		st, _ := w.call("alice", "GET", "/repositories/old/tree", nil)
		return st == 200
	})
	var old struct {
		Root string `json:"root_hypothesis_id"`
	}
	_, b := w.call("alice", "GET", "/repositories/old", nil)
	_ = json.Unmarshal(b, &old)
	agent := w.user("agent", "s-agent-mig")
	agent.repo = "old"
	agent.loginAndClone("old")
	reviewer := w.user("reviewer", "s-rev-mig")
	reviewer.repo = "old"
	if code, _ := reviewer.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}
	merge := func(branch string) {
		t.Helper()
		ar := agent.create(branch)
		if got, code, out := reviewer.approveMerge(ar.ID, 1); code != 0 || got.State != "merged" {
			t.Fatalf("%s の approve：%d %s", branch, code, out)
		}
	}
	agent.branch("bg")
	agent.must(nil, "node", "edit", old.Root, "--append-background", agent.file("bg.md", "移す前の背景\n"))
	agent.commitPush("bg", "背景")
	merge("bg")
	// 元の repository で、B の検証結果が、A の部分木を、元の版のコミットで引用する。
	agent.branch("c1")
	var sc scaffold
	agent.must(&sc, "story", "new", "--parent", old.Root, "--title", "道筋", "--summary", "s", "--hypothesis", "A", "--hypothesis", "B")
	hyps := sc.IDs["hypothesis_ids"].([]any)
	hA, hB := hyps[0].(string), hyps[1].(string)
	agent.commitPush("c1", "分解")
	merge("c1")
	agent.branch("c2")
	agent.must(nil, "verdict", "new", "--hypothesis", hA, "--value", "true", "--report", agent.file("ra.md", "A\n"))
	agent.commitPush("c2", "A")
	merge("c2")
	agent.branch("c3")
	var vb scaffold
	agent.must(&vb, "verdict", "new", "--hypothesis", hB, "--value", "true", "--report", agent.file("rb.md", "A を流用\n"), "--cite", "subtree:"+hA)
	agent.commitPush("c3", "B は A を引用")
	merge("c3")
	citedVerdict := vb.IDs["verdict_id"].(string)

	// archive でなければ、移せない。
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "moved", "migrate_from": "old"}); st != 422 || !strings.Contains(string(b), "archive") {
		t.Fatalf("archive でないのに移せた：%d %s", st, b)
	}
	// 管理者が archive にしてから、移す。
	w.forgejoCall(w.admin, "PATCH", "/repos/"+w.org+"/old", map[string]any{"archived": true}, nil, 200)
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "moved", "migrate_from": "old"}); st != 201 && st != 202 {
		t.Fatalf("移す：%d %s", st, b)
	}
	var moved struct {
		Directory    string `json:"directory"`
		Root         string `json:"root_hypothesis_id"`
		MigratedFrom *struct {
			Repository string `json:"repository"`
			Commit     string `json:"commit"`
		} `json:"migrated_from"`
	}
	w.eventually("移した先が読める", 60*time.Second, func() bool {
		st, b := w.call("alice", "GET", "/repositories/moved", nil)
		return st == 200 && json.Unmarshal(b, &moved) == nil && moved.MigratedFrom != nil
	})
	if moved.Root != old.Root || moved.MigratedFrom.Repository != "old" || moved.Directory != "logiforge/moved" {
		t.Fatalf("移した先：%+v", moved)
	}
	_, nb := w.call("alice", "GET", "/repositories/moved/nodes/"+old.Root, nil)
	if !strings.Contains(string(nb), "移す前の背景") {
		t.Fatalf("移した先の木に、元の背景がない：%s", nb)
	}
	// 元は migrated_to を持ち、書けない。
	w.eventually("元に migrated_to が付く", 30*time.Second, func() bool {
		_, b := w.call("alice", "GET", "/repositories/old", nil)
		return strings.Contains(string(b), `"migrated_to":"moved"`)
	})
	if st, b := w.call("agent", "POST", "/repositories/old/human-requests", map[string]any{"kind": "pvr", "node_id": old.Root, "body": "x"}, "Logiforge-Session-Id", "s"); st != 409 || !strings.Contains(string(b), "not_open") {
		t.Fatalf("移した元に書けた：%d %s", st, b)
	}

	// 移した検証結果の引用は、引用した時点が版の 1 番になるので、移しただけでは、引用元が変わったことにならない。
	mover := w.user("agent", "s-agent-mig2")
	mover.repo = "moved"
	mover.loginAndClone("moved")
	moverRev := w.user("reviewer", "s-rev-mig2")
	moverRev.repo = "moved"
	if code, _ := moverRev.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}
	changed := func() bool {
		var page struct {
			Items []map[string]string `json:"items"`
		}
		mover.must(&page, "work", "list", "--kind", "citation_changed")
		for _, it := range page.Items {
			if it["subject_id"] == citedVerdict {
				return true
			}
		}
		return false
	}
	if changed() {
		t.Fatal("移しただけで、引用元が変わったことになった")
	}
	// 移した先で、引用した部分木（A）が変わると、変わったことになる。
	mover.branch("a-bg")
	mover.must(nil, "node", "edit", hA, "--append-background", mover.file("abg.md", "移したあとの A の背景\n"))
	mover.commitPush("a-bg", "A の背景")
	mar := mover.create("a-bg")
	if got, code, out := moverRev.approveMerge(mar.ID, 1); code != 0 || got.State != "merged" {
		t.Fatalf("移した先の approve：%d %s", code, out)
	}
	w.eventually("移した先で A が変わると、引用元が変わったことになる", 30*time.Second, changed)
}
