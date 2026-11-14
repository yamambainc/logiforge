package e2e

import (
	"encoding/json"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestE2EEdges は、認可、競合、レビューのあとの push、ブランチの削除、worker の停止、main への直接の push を通す。
func TestE2EEdges(t *testing.T) {
	w := setup(t)
	code, body := w.call("alice", "POST", "/repositories", map[string]string{"name": "q", "statement": "問い"})
	if code != 201 {
		t.Fatalf("repository の作成：%d %s", code, body)
	}
	var repo struct {
		Root string `json:"root_hypothesis_id"`
	}
	_ = json.Unmarshal(body, &repo)
	agent := w.user("agent", "s-a1")
	agent.loginAndClone("q")
	reviewer := w.user("reviewer", "s-r")
	if code, _ := reviewer.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}

	// ---- 認可 ----
	t.Run("認可", func(t *testing.T) {
		if code, _ := w.call("alice", "POST", "/repositories/q/apply-requests", map[string]string{"branch": "x"}, "Logiforge-Session-Id", "s"); code != 403 {
			t.Fatalf("user の apply request の作成：%d", code)
		}
		if code, _ := w.call("agent", "POST", "/repositories/q/human-requests/hrq_X/answer", map[string]string{"body": "b"}); code != 403 {
			t.Fatalf("client の回答：%d", code)
		}
		if code, _ := w.call("reader", "GET", "/repositories/q", nil); code != 401 {
			t.Fatalf("worker のユーザー：%d", code)
		}
		if code, _ := w.call("agent", "POST", "/repositories/q/apply-requests", map[string]string{"branch": "x"}); code != 400 {
			t.Fatalf("セッションなし：%d", code)
		}
		w.tokens["nobody"] = "not-a-token"
		if code, _ := w.call("nobody", "GET", "/repositories", nil); code != 401 {
			t.Fatalf("正しくないトークン：%d", code)
		}
	})

	// ---- 競合：同じノードを対象にする 2 つの提案。2 つ目は stale_target で修正待ちになる ----
	t.Run("stale_target", func(t *testing.T) {
		agent.branch("x")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("x.md", "x の背景\n"))
		agent.commitPush("x", "x")
		x := agent.create("x")
		agent.branch("y")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("y.md", "y の背景\n"))
		agent.commitPush("y", "y")
		y := agent.create("y")
		if got, code, _ := reviewer.review(x.ID, "approve", 1); code != 0 || got.State != "merged" {
			t.Fatalf("x：%+v", got)
		}
		got, code, out := reviewer.review(y.ID, "approve", 1)
		if code != 0 || got.State != "open" || got.Awaiting != "fix" || !strings.Contains(out, "stale_target") {
			t.Fatalf("y：%d %s", code, out)
		}
		t.Log("conflict（重ねられない）は、同じファイルの変化が対象ノードの変化として先に stale_target で見つかるので、この流れでは起きない。git の衝突の道は、internal/backend の単体テストで確かめた")
	})

	// ---- approve の直前に push された：version_conflict。新しい版をレビューし直せば、反映できる ----
	t.Run("approve の直前の push", func(t *testing.T) {
		agent.session = "s-a2"
		agent.branch("v")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("v1.md", "版 1\n"))
		agent.commitPush("v", "版 1")
		ar := agent.create("v")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("v2.md", "版 2\n"))
		agent.commitPush("v", "版 2")
		_, code, out := reviewer.review(ar.ID, "approve", 1)
		if code != 1 || !strings.Contains(out, "version_conflict") {
			t.Fatalf("古い版の approve：%d %s", code, out)
		}
		w.eventually("版 2 が読む側に出る", 30*time.Second, func() bool { return agent.versionReady(ar.ID, 2) })
		if got, code, out := reviewer.review(ar.ID, "approve", 2); code != 0 || got.State != "merged" {
			t.Fatalf("新しい版の approve：%d %s", code, out)
		}
	})

	// ---- ブランチを消すと、apply request は閉じる ----
	t.Run("ブランチの削除", func(t *testing.T) {
		agent.session = "s-a3"
		agent.branch("d")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("d.md", "消す\n"))
		agent.commitPush("d", "d")
		ar := agent.create("d")
		agent.git("push", "--quiet", "origin", "--delete", "d")
		w.eventually("apply request が閉じる", 30*time.Second, func() bool {
			var got applyRequest
			agent.must(&got, "ar", "view", ar.ID)
			return got.State == "closed"
		})
	})

	// ---- worker が止まっているとき：読むのは止まる前の木、作成は unavailable。動かし直せば追いつく ----
	t.Run("worker の停止", func(t *testing.T) {
		var before struct {
			Revision string `json:"revision"`
		}
		agent.must(&before, "tree", "view")
		w.stopWorker()
		agent.session = "s-a4"
		agent.branch("s")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("s.md", "止まっている間\n"))
		agent.commitPush("s", "s")
		var after struct {
			Revision string `json:"revision"`
		}
		agent.must(&after, "tree", "view")
		if after.Revision != before.Revision {
			t.Fatalf("止まる前の木が読めない：%s %s", before.Revision, after.Revision)
		}
		code, out := agent.cli("", "ar", "create", "--branch", "s", "--json")
		if code != 1 || !strings.Contains(out, "unavailable") || !strings.Contains(out, "idempotency_key") {
			t.Fatalf("worker が止まっているときの作成：%d %s", code, out)
		}
		var prob struct {
			Key string `json:"idempotency_key"`
		}
		_ = json.Unmarshal([]byte(out), &prob)
		w.restartWorker()
		// 失敗したときに残ったキーで送り直す。
		w.eventually("worker が戻る", 30*time.Second, func() bool {
			code, _ := agent.cli("", "ar", "create", "--branch", "s", "--idempotency-key", prob.Key, "--json")
			return code == 0
		})
	})

	// ---- main への直接の push：利用者とエージェントは、保護で拒まれる。保護が外れたときの push は、警告になる ----
	t.Run("main への直接の push", func(t *testing.T) {
		agent.git("fetch", "--quiet", "origin")
		agent.git("checkout", "--quiet", "-B", "direct", "origin/main")
		agent.write("tree/"+repo.Root+"/background.md", "直接\n")
		agent.git("commit", "--quiet", "-am", "直接")
		cmd := exec.Command("git", "push", "--quiet", "origin", "HEAD:main")
		cmd.Env, cmd.Dir = agent.env(), agent.dir
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("エージェントが main に push できた：%s", out)
		}
		alice := w.user("alice", "s-alice")
		u, _ := url.Parse(w.forgejo)
		u.User = url.UserPassword(w.login("alice"), w.tokens["alice"])
		cmd = exec.Command("git", "push", "--quiet", u.String()+"/"+w.org+"/q.git", "HEAD:main")
		cmd.Env, cmd.Dir = alice.env(), agent.dir
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("利用者が main に push できた：%s", out)
		}

		ref := w.org + "/q"
		w.forgejoCall(w.admin, "DELETE", "/repos/"+ref+"/branch_protections/main", nil, nil, 204)
		u.User = url.UserPassword("root", w.admin)
		cmd = exec.Command("git", "push", "--quiet", u.String()+"/"+ref+".git", "HEAD:main")
		cmd.Env, cmd.Dir = agent.env(), agent.dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("保護を外したあとの管理者の push：%v %s", err, out)
		}
		w.forgejoCall(w.admin, "POST", "/repos/"+ref+"/branch_protections", map[string]any{"rule_name": "main", "enable_push": false,
			"enable_merge_whitelist": true, "merge_whitelist_usernames": []string{w.login("writer")}}, nil, 201)
		w.eventually("反映の記録がないコミットの警告が出る", 30*time.Second, func() bool {
			var tr struct {
				Warnings []string `json:"warnings"`
			}
			agent.must(&tr, "tree", "view")
			return len(tr.Warnings) > 0 && strings.Contains(strings.Join(tr.Warnings, "\n"), "反映の記録がない")
		})
	})

	// ---- 引用元の変化、差し替え、inactive 化 ----
	t.Run("引用元の変化と差し替えと inactive 化", func(t *testing.T) {
		agent.session = "s-a6"
		merge := func(branch string) applyRequest {
			t.Helper()
			ar := agent.create(branch)
			got, code, out := reviewer.review(ar.ID, "approve", 1)
			if code != 0 || got.State != "merged" {
				t.Fatalf("%s の approve：%d %s", branch, code, out)
			}
			return got
		}
		mainRevision := func() string {
			var tr struct {
				Revision string `json:"revision"`
			}
			agent.must(&tr, "tree", "view")
			return tr.Revision
		}
		agent.branch("c1")
		var sc scaffold
		agent.must(&sc, "story", "new", "--parent", repo.Root, "--title", "道筋", "--summary", "s", "--hypothesis", "A", "--hypothesis", "B")
		story := sc.IDs["story_id"].(string)
		hyps := sc.IDs["hypothesis_ids"].([]any)
		hA, hB := hyps[0].(string), hyps[1].(string)
		agent.commitPush("c1", "分解")
		merge("c1")

		agent.branch("c2")
		agent.must(nil, "verdict", "new", "--hypothesis", hA, "--value", "true", "--report", agent.file("ra.md", "A\n"))
		agent.commitPush("c2", "A")
		merge("c2")
		rA := mainRevision()

		agent.branch("c3")
		var vb scaffold
		agent.must(&vb, "verdict", "new", "--hypothesis", hB, "--value", "true", "--report", agent.file("rb.md", "A を流用\n"), "--cite", "subtree:"+hA+"@"+rA)
		agent.commitPush("c3", "B は A を引用")
		merge("c3")
		oldVerdict := vb.IDs["verdict_id"].(string)
		citationChanged := func() bool {
			var page struct {
				Items []map[string]string `json:"items"`
			}
			agent.must(&page, "work", "list", "--kind", "citation_changed")
			for _, it := range page.Items {
				if it["subject_id"] == oldVerdict && it["node_id"] == hB {
					return true
				}
			}
			return false
		}
		if citationChanged() {
			t.Fatal("引用元が変わる前に、citation_changed が出た")
		}

		agent.branch("c4")
		agent.must(nil, "node", "edit", hA, "--append-background", agent.file("ba.md", "A の背景を足した\n"))
		agent.commitPush("c4", "A を変える")
		merge("c4")
		if !citationChanged() {
			t.Fatal("引用元が変わったのに、citation_changed が出ない")
		}

		// 見直して、引用を今のリビジョンにした検証結果で差し替えると、作業から外れる。
		agent.branch("c5")
		agent.must(nil, "verdict", "new", "--hypothesis", hB, "--value", "true", "--report", agent.file("rb2.md", "見直した\n"),
			"--cite", "subtree:"+hA+"@"+mainRevision(), "--supersedes", oldVerdict)
		agent.commitPush("c5", "B を見直す")
		merge("c5")
		if citationChanged() {
			t.Fatal("差し替えたのに、citation_changed が残る")
		}

		agent.branch("c6")
		agent.must(nil, "story", "inactivate", story, "--reason", "別の道筋にする")
		agent.commitPush("c6", "inactive")
		merge("c6")
		var nodes struct {
			Nodes []map[string]any `json:"nodes"`
		}
		agent.must(&nodes, "tree", "nodes")
		for _, n := range nodes.Nodes {
			if n["id"] == story && (n["state"] != "inactive" || n["reason"] == nil) {
				t.Fatalf("inactive にならない：%+v", n)
			}
			if n["id"] == hA && n["inactive"] != true {
				t.Fatalf("子の仮説が inactive にならない：%+v", n)
			}
		}
	})
}
