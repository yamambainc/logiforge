package e2e

import (
	"encoding/json"
	"net/url"
	"os"
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
		w.tokens["nobody"] = "not-a-token"
		if code, _ := w.call("nobody", "GET", "/repositories", nil); code != 401 {
			t.Fatalf("正しくないトークン：%d", code)
		}
	})

	// ---- 競合：同じノードを対象にする 2 つの apply request。2 つ目は stale_target で修正待ちになる ----
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

	// ---- WebUI のレビュー：利用者（alice）が、WebUI のサインインのトークンで approve し、本人のトークンで反映する ----
	t.Run("WebUI のレビュー", func(t *testing.T) {
		agent.branch("w")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("w.md", "w の背景\n"))
		agent.commitPush("w", "w")
		z := agent.create("w")
		path := "/repositories/q/apply-requests/" + z.ID
		code, b := w.call("alice", "GET", path, nil)
		var got struct {
			OpenedByCaller bool `json:"opened_by_caller"`
		}
		if _ = json.Unmarshal(b, &got); code != 200 || got.OpenedByCaller {
			t.Fatalf("alice の getApplyRequest：%d %s", code, b)
		}
		if code, b := w.call("alice", "POST", path+"/reviews", map[string]string{"kind": "comment", "commit": z.HeadCommit, "body": "見ました"}, "Logiforge-Session-Id", "webui-1"); code != 201 {
			t.Fatalf("alice の comment：%d %s", code, b)
		}
		code, b = w.call("alice", "POST", path+"/reviews", map[string]string{"kind": "approve", "commit": z.HeadCommit}, "Logiforge-Session-Id", "webui-1")
		if code != 201 || !strings.Contains(string(b), `"state":"merged"`) {
			t.Fatalf("alice の approve：%d %s", code, b)
		}
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
		if code != 1 || !strings.Contains(out, "commit_conflict") {
			t.Fatalf("古い版の approve：%d %s", code, out)
		}
		w.eventually("2 つ目の先頭のコミットが読む側に出る", 30*time.Second, func() bool { return agent.headReady(ar.ID, 2) })
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

	// ---- worker が止まっているとき：読むのは止まる前のスナップショットツリー、作成は unavailable。動かし直せば追いつく ----
	t.Run("worker の停止", func(t *testing.T) {
		var before struct {
			Commit string `json:"commit"`
		}
		agent.must(&before, "tree", "view")
		// 動いている worker のハートビートは新しい。worker-health は 0 で終わる。
		health := func() error {
			cmd := exec.Command(bin, "worker-health")
			cmd.Env = append(os.Environ(), w.workerEnv...)
			return cmd.Run()
		}
		if err := health(); err != nil {
			t.Fatalf("動いている worker の worker-health：%v", err)
		}
		w.stopWorker()
		// 止まると、見回りの間隔（1 秒）の 10 倍のあとに、0 以外で終わる。
		w.eventually("worker-health が止まったことを返す", 30*time.Second, func() bool { return health() != nil })
		agent.session = "s-a4"
		agent.branch("s")
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("s.md", "止まっている間\n"))
		agent.commitPush("s", "s")
		var after struct {
			Commit string `json:"commit"`
		}
		agent.must(&after, "tree", "view")
		if after.Commit != before.Commit {
			t.Fatalf("止まる前のスナップショットツリーが読めない：%s %s", before.Commit, after.Commit)
		}
		// apply request の作成は、worker を待たない。バックエンドが git を読んで検査するので、worker が止まっていても作れる。
		code, out := agent.cli("", "ar", "create", "--branch", "s", "--json")
		if code != 0 {
			t.Fatalf("worker が止まっているときの作成：%d %s", code, out)
		}
		var ar applyRequest
		_ = json.Unmarshal([]byte(out), &ar)
		if len(ar.Operations) != 1 || ar.Operations[0].Kind != "node.edit" {
			t.Fatalf("worker が止まっているときの作成の操作：%+v", ar)
		}
		// 版のスナップショットは worker が作るので、それまで、レビューは snapshot_pending になる。
		reviewer.session = "s-r4"
		code, out = reviewer.cli("", "ar", "review", ar.ID, "--comment", "--body", "見ました", "--commit", ar.HeadCommit, "--repo", "q", "--json")
		if code != 1 || !strings.Contains(out, "snapshot_pending") {
			t.Fatalf("worker が止まっているときのレビュー：%d %s", code, out)
		}
		w.restartWorker()
		w.eventually("worker が戻り、レビューできる", 30*time.Second, func() bool {
			code, _ := reviewer.cli("", "ar", "review", ar.ID, "--comment", "--body", "見ました", "--commit", ar.HeadCommit, "--repo", "q", "--json")
			return code == 0
		})
		agent.must(nil, "ar", "close", ar.ID, "--reason", "確かめ終えた")
	})

	// ---- main への直接の push：利用者とエージェントは、保護で拒まれる。保護が外れたときの push は、そのまま受け入れる。
	// 木の不変条件を破る push のあいだは、木を読む API が前の valid な木を返し、CLI は警告する。直すコミットで戻る
	// （design/backend/invalid-main.md） ----
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
			"enable_merge_whitelist": false}, nil, 201)
		// valid な直接の push は、そのまま受け入れる。反映の apply request は、ない。
		direct := agent.git("rev-parse", "HEAD")
		var tv struct {
			Commit       string  `json:"commit"`
			ValidCommit  *string `json:"valid_commit"`
			InvalidSince *string `json:"invalid_since"`
		}
		w.eventually("worker が直接の push を取り込む", 120*time.Second, func() bool {
			agent.must(&tv, "tree", "view")
			return tv.Commit == direct
		})
		if tv.InvalidSince != nil || tv.ValidCommit == nil || *tv.ValidCommit != direct {
			t.Fatalf("valid な直接の push：%+v", tv)
		}
		pushMain := func(what string) string {
			t.Helper()
			w.forgejoCall(w.admin, "DELETE", "/repos/"+ref+"/branch_protections/main", nil, nil, 204)
			cmd := exec.Command("git", "push", "--quiet", u.String()+"/"+ref+".git", "HEAD:main")
			cmd.Env, cmd.Dir = agent.env(), agent.dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s：%v %s", what, err, out)
			}
			w.forgejoCall(w.admin, "POST", "/repos/"+ref+"/branch_protections", map[string]any{"rule_name": "main", "enable_push": false,
				"enable_merge_whitelist": false}, nil, 201)
			return agent.git("rev-parse", "HEAD")
		}

		// 木の外のファイルを置く直接の push。最新のコミットは valid でない。
		agent.write("README.md", "木の外\n")
		agent.git("add", "-A")
		agent.git("commit", "--quiet", "-m", "木の外")
		invalid := pushMain("valid でない直接の push")
		var rv struct {
			ValidCommit  *string `json:"valid_commit"`
			InvalidSince *string `json:"invalid_since"`
		}
		w.eventually("最新のコミットが valid でないと分かる", 120*time.Second, func() bool {
			st, b := w.call("alice", "GET", "/repositories/q", nil)
			return st == 200 && json.Unmarshal(b, &rv) == nil && rv.InvalidSince != nil
		})
		if *rv.InvalidSince != invalid || rv.ValidCommit == nil || *rv.ValidCommit != direct {
			t.Fatalf("valid_commit と invalid_since：%+v（valid でないのは %s、前の valid は %s）", rv, invalid, direct)
		}
		// 木を読む API は、前の valid な木を返し、ヘッダー Logiforge-Latest-Invalid に、そのコミットを付ける。
		st, b, h := w.callHeader("alice", "GET", "/repositories/q/tree", nil)
		if json.Unmarshal(b, &tv); st != 200 || tv.Commit != direct || h.Get("Logiforge-Latest-Invalid") != direct {
			t.Fatalf("木を読む API：%d %s %q", st, b, h.Get("Logiforge-Latest-Invalid"))
		}
		// CLI は、結果を書いたうえで、標準エラーに警告を書く。止めない。
		for _, args := range [][]string{{"tree", "view", "--json"}, {"check", "--json"}} {
			code, _, errOut := agent.cliErr("", args...)
			if !strings.Contains(errOut, "valid でない") || !strings.Contains(errOut, direct) {
				t.Fatalf("%v が警告しない：%d %s", args, code, errOut)
			}
			if args[0] == "tree" && code != 0 {
				t.Fatalf("tree view の終了コード：%d", code)
			}
		}

		// 直すコミット（木の外のファイルを消す）で、最新のコミットが valid に戻る。
		agent.git("rm", "--quiet", "README.md")
		agent.git("commit", "--quiet", "-m", "直した")
		fixed := pushMain("直す直接の push")
		w.eventually("直したら、最新のコミットの木を返す", 120*time.Second, func() bool {
			st, b, h := w.callHeader("alice", "GET", "/repositories/q/tree", nil)
			return st == 200 && json.Unmarshal(b, &tv) == nil && tv.Commit == fixed && tv.InvalidSince == nil && h.Get("Logiforge-Latest-Invalid") == ""
		})
		if code, _, errOut := agent.cliErr("", "tree", "view", "--json"); code != 0 || strings.Contains(errOut, "valid でない") {
			t.Fatalf("直したあとの警告：%d %s", code, errOut)
		}
		agent.git("checkout", "--quiet", "main")
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

		agent.branch("c3")
		var vb scaffold
		agent.must(&vb, "verdict", "new", "--hypothesis", hB, "--value", "true", "--report", agent.file("rb.md", "A を流用\n"), "--cite", "subtree:"+hA)
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

		// 見直した検証結果で差し替えると、引用した時点が新しい反映のコミットになり、作業から外れる。
		agent.branch("c5")
		agent.must(nil, "verdict", "new", "--hypothesis", hB, "--value", "true", "--report", agent.file("rb2.md", "見直した\n"),
			"--cite", "subtree:"+hA, "--supersedes", oldVerdict)
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
