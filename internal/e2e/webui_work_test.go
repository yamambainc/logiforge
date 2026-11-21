package e2e

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestWebUIWorkCounts は、ダッシュボードの作業の件数を押して開いた先の数が、件数と同じになることを、
// どの種類も 1 件以上あるスナップショットツリーで確かめる（design/webui/screens.md の「ダッシュボード」）。
// 回答済みの依頼は、回答を引用した apply request が open になると、件数からも開いた先からも外れることも確かめる。
// ブラウザの操作は webui/e2e/work.mjs が行う。
func TestWebUIWorkCounts(t *testing.T) {
	w, chrome := setupWebUI(t)
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "q", "statement": "作業を数える"}); st != 201 {
		t.Fatalf("repository の作成：%d %s", st, b)
	}
	w.eventually("スナップショットツリーが出る", 60*time.Second, func() bool {
		st, _ := w.call("alice", "GET", "/repositories/q/tree", nil)
		return st == 200
	})
	var repo struct {
		Root string `json:"root_hypothesis_id"`
	}
	_, rb := w.call("alice", "GET", "/repositories/q", nil)
	_ = json.Unmarshal(rb, &repo)
	agent := w.user("agent", "s-agent-w")
	agent.loginAndClone("q")
	reviewer := w.user("reviewer", "s-reviewer-w")
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

	// 仮説 A〜D のストーリー
	agent.branch("c1")
	var sc scaffold
	agent.must(&sc, "story", "new", "--parent", repo.Root, "--title", "道筋", "--summary", "s",
		"--hypothesis", "A", "--hypothesis", "B", "--hypothesis", "C", "--hypothesis", "D")
	hyps := sc.IDs["hypothesis_ids"].([]any)
	hA, hB, hC, hD := hyps[0].(string), hyps[1].(string), hyps[2].(string), hyps[3].(string)
	agent.commitPush("c1", "分解")
	merge("c1")

	// 引用元が変化した検証結果：B が A の部分木を引用し、そのあと A が変わる
	agent.branch("c2")
	agent.must(nil, "verdict", "new", "--hypothesis", hA, "--value", "true", "--report", agent.file("ra.md", "A\n"))
	agent.commitPush("c2", "A")
	merge("c2")
	agent.branch("c3")
	agent.must(nil, "verdict", "new", "--hypothesis", hB, "--value", "true", "--report", agent.file("rb.md", "A を流用\n"), "--cite", "subtree:"+hA)
	agent.commitPush("c3", "B は A を引用")
	merge("c3")
	agent.branch("c4")
	agent.must(nil, "node", "edit", hA, "--append-background", agent.file("ba.md", "A の背景を足した\n"))
	agent.commitPush("c4", "A を変える")
	merge("c4")

	// 修正待ち：C の検証結果の apply request に、修正を求めるレビュー
	agent.branch("c5")
	agent.must(nil, "verdict", "new", "--hypothesis", hC, "--value", "false", "--report", agent.file("rc.md", "C\n"))
	agent.commitPush("c5", "C")
	arC := agent.create("c5")
	if got, code, out := reviewer.review(arC.ID, "request-changes", 1); code != 0 || got.Awaiting != "fix" {
		t.Fatalf("request_changes：%d %s", code, out)
	}
	// レビューは、だれが（reviewer）と、いつ（created_at）を返す（Forgejo のコメントから読む）
	var arView struct {
		Reviews []struct {
			Kind      string  `json:"kind"`
			Reviewer  string  `json:"reviewer"`
			CreatedAt *string `json:"created_at"`
		} `json:"reviews"`
	}
	_, ab := w.call("alice", "GET", "/repositories/q/apply-requests/"+arC.ID, nil)
	_ = json.Unmarshal(ab, &arView)
	if len(arView.Reviews) != 1 || arView.Reviews[0].Reviewer == "" || arView.Reviews[0].CreatedAt == nil {
		t.Fatalf("レビューの書いた人と時刻：%s", ab)
	}

	// 回答済みの依頼：D への PVR に、利用者が回答する
	var hr struct {
		ID string `json:"id"`
	}
	agent.must(&hr, "human-request", "create", "--node", hD, "--kind", "pvr", "--body", agent.file("pvr.md", "D を測る\n"))
	if code, body := w.call("alice", "POST", "/repositories/q/human-requests/"+hr.ID+"/answer", map[string]any{"body": "測った",
		"attachments": []map[string]string{{"kind": "file", "name": "d.csv", "content_base64": base64.StdEncoding.EncodeToString([]byte("d,1\n"))}}}); code != 201 {
		t.Fatalf("回答：%d %s", code, body)
	}

	out := os.Getenv("LOGIFORGE_E2E_OUT")
	if out == "" {
		out = t.TempDir()
	}
	login := w.login("alice")
	env := []string{"BASE=" + w.api, "LOGIN=" + login, "PASSWORD=pw-" + login + "-12345", "CHROME=" + chrome, "OUT=" + out, "REPO=q"}
	runBrowser(t, "e2e/work.mjs", append(env, "EXPECT=fix_waiting>=1,citation_changed>=1,human_request_answered>=1,review_waiting=0")...)

	// 回答を引用した apply request を open にすると、回答済みの依頼は作業から外れる
	agent.branch("c6")
	agent.must(nil, "verdict", "new", "--hypothesis", hD, "--value", "true", "--report", agent.file("rd.md", "回答のとおり\n"), "--cite", "answer:"+hr.ID, "--repo", "q")
	agent.commitPush("c6", "D を検証")
	agent.create("c6")
	runBrowser(t, "e2e/work.mjs", append(env, "EXPECT=human_request_answered=0,review_waiting>=1", "PHASE=2")...)
}
