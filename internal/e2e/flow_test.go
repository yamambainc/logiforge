package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type applyRequest struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	HeadCommit string `json:"head_commit"`
	Awaiting   string `json:"awaiting"`
	Operations []struct {
		Kind string `json:"kind"`
	} `json:"operations"`
	Warnings []string `json:"warnings"`
}

type checkOut struct {
	OK         bool `json:"ok"`
	Operations []struct {
		Kind string `json:"kind"`
	} `json:"operations"`
	Rejected []struct {
		Rule string `json:"rule"`
	} `json:"rejected"`
}

type scaffold struct {
	IDs map[string]any `json:"ids"`
}

func (u *user) file(name, content string) string {
	p := filepath.Join(u.home, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		u.w.t.Fatal(err)
	}
	return p
}

// branch は、origin/main から新しいブランチを作る。
func (u *user) branch(name string) {
	u.git("fetch", "--quiet", "origin")
	u.git("checkout", "--quiet", "-B", name, "origin/main")
}

// commitPush は、ファイルをコミットし（logiforge commit でセッションの trailer を付ける）、push する。
func (u *user) commitPush(branch, msg string) {
	u.git("add", "-A")
	if code, _ := u.cli("", "commit", "-m", msg); code != 0 {
		u.w.t.Fatal("logiforge commit")
	}
	u.git("push", "--quiet", "--force", "origin", branch)
}

// create は、check と ar create をし、2 つが同じ操作を復元したことを確かめる。
func (u *user) create(branch string) applyRequest {
	u.w.t.Helper()
	var c checkOut
	u.must(&c, "check")
	var ar applyRequest
	start := time.Now()
	u.must(&ar, "ar", "create", "--branch", branch)
	u.w.t.Logf("ar create %s：%v", branch, time.Since(start).Round(time.Millisecond))
	if !c.OK || len(c.Operations) != len(ar.Operations) {
		u.w.t.Fatalf("check と ar create の操作が違う：%+v %+v", c, ar)
	}
	for i := range c.Operations {
		if c.Operations[i].Kind != ar.Operations[i].Kind {
			u.w.t.Fatalf("check と ar create の操作が違う：%+v %+v", c.Operations, ar.Operations)
		}
	}
	// 作成の直後に読むと、worker がまだブランチを取り込んでいなければ、レビューの材料は snapshot_pending になる。
	// 取り込んでいれば、操作の一覧が返る（design/api/writes.md の「書いた直後の読み」）。どちらかであることを確かめる。
	if code, out := u.cli("", "ar", "material", ar.ID, "--repo", u.repoName(), "--json"); code == 0 {
		if !strings.Contains(out, `"operations"`) {
			u.w.t.Fatalf("作成の直後のレビューの材料に、操作の一覧がない：%s", out)
		}
	} else if !strings.Contains(out, "snapshot_pending") {
		u.w.t.Fatalf("作成の直後のレビューの材料：%d %s", code, out)
	}
	// バックエンドは worker を起こさない。先頭のコミットのスナップショットは、worker が次の見回りで作る（design/api/writes.md）。
	// そのあとのレビューと作業の一覧が、それを読めるように待つ。
	u.w.eventually("worker が "+branch+" の先頭を取り込む", 30*time.Second, func() bool { return u.headReady(ar.ID, 1) })
	return ar
}

// headCommits は、apply request に push された先頭のコミットの一覧（古い順）を読む。
func (u *user) headCommits(id string) []struct {
	Commit string `json:"commit"`
	Base   string `json:"base_commit"`
} {
	var ar struct {
		HeadCommits []struct {
			Commit string `json:"commit"`
			Base   string `json:"base_commit"`
		} `json:"head_commits"`
	}
	u.must(&ar, "ar", "view", id, "--repo", u.repoName())
	return ar.HeadCommits
}

// headReady は、apply request に push された先頭のコミットが n 個になり、worker が最後のもののスナップショットを作り終えた（base がある）かを返す。
func (u *user) headReady(id string, n int) bool {
	hs := u.headCommits(id)
	return len(hs) == n && hs[n-1].Base != ""
}

// review は、apply request に push された n 番目（1 から）の先頭のコミットを、--commit の SHA で指してレビューする。
func (u *user) review(id, kind string, n int) (applyRequest, int, string) {
	u.w.t.Helper()
	hs := u.headCommits(id)
	if len(hs) < n {
		u.w.t.Fatalf("%s に、%d 番目の先頭のコミットがない：%+v", id, n, hs)
	}
	args := []string{"ar", "review", id, "--" + kind, "--commit", hs[n-1].Commit, "--repo", u.repoName(), "--json"}
	if kind == "approve" {
		args = append(args, "--prompt-revision", "prompt-1")
	}
	code, out := u.cli("", args...)
	var res struct {
		ApplyRequest applyRequest `json:"apply_request"`
	}
	_ = json.Unmarshal([]byte(out), &res)
	return res.ApplyRequest, code, out
}

func TestE2E(t *testing.T) {
	w := setup(t)

	// ---- 利用者が repository を作る（user だけ） ----
	if code, body := w.call("agent", "POST", "/repositories", map[string]string{"name": "q", "statement": "問い"}); code != 403 {
		t.Fatalf("client が repository を作れた：%d %s", code, body)
	}
	start := time.Now()
	code, body := w.call("alice", "POST", "/repositories", map[string]string{"name": "q", "statement": "問い"})
	if code != 201 {
		t.Fatalf("repository の作成：%d %s", code, body)
	}
	t.Logf("repository の作成（用意と、worker が読む側に出すまで）：%v", time.Since(start).Round(time.Millisecond))
	var repo struct {
		Root string `json:"root_hypothesis_id"`
	}
	_ = json.Unmarshal(body, &repo)
	if repo.Root == "" {
		t.Fatalf("repository の応答に根がない（読む側に出ていない）：%s", body)
	}
	// 送り直しは、成功する。
	if code, body := w.call("alice", "POST", "/repositories", map[string]string{"name": "q", "statement": "問い"}); code != 201 {
		t.Fatalf("送り直し：%d %s", code, body)
	}

	agent := w.user("agent", "s-agent-1")
	agent.loginAndClone("q")
	reviewer := w.user("reviewer", "s-reviewer")
	if code, _ := reviewer.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}

	// ---- 分解：ストーリーと子仮説を作り、反映する ----
	agent.branch("b1")
	var sc scaffold
	agent.must(&sc, "story", "new", "--parent", repo.Root, "--title", "道筋", "--summary", "2 つに分ける", "--hypothesis", "A が成り立つ", "--hypothesis", "B が成り立つ")
	hyps := sc.IDs["hypothesis_ids"].([]any)
	hA, hB := hyps[0].(string), hyps[1].(string)
	agent.commitPush("b1", "分解")
	ar1 := agent.create("b1")
	var mat struct {
		Targets []map[string]any `json:"targets"`
	}
	reviewer.must(&mat, "ar", "material", ar1.ID, "--repo", "q")
	if len(mat.Targets) != 1 || mat.Targets[0]["id"] != repo.Root {
		t.Fatalf("レビューの材料：%+v", mat)
	}
	// 反映したあとの木（WebUI の apply request の画面）：新しい仮説 A と B が、反映の前から見える。
	if code, body := w.call("alice", "GET", "/repositories/q/tree/nodes?apply_request="+ar1.ID, nil); code != 200 ||
		!strings.Contains(string(body), hA) || !strings.Contains(string(body), hB) {
		t.Fatalf("反映したあとの木：%d %s", code, body)
	}
	if got, code, out := reviewer.review(ar1.ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("approve：%d %s", code, out)
	}

	// ---- 検証：request_changes のあと、新しい版を push して、approve する ----
	agent.branch("b2")
	agent.must(nil, "verdict", "new", "--hypothesis", hA, "--value", "true", "--report", agent.file("r1.md", "# A の検証\n\n初版\n"), "--artifact", agent.file("data.csv", "x,y\n1,2\n"))
	agent.commitPush("b2", "A を検証")
	ar2 := agent.create("b2")
	if got, code, _ := reviewer.review(ar2.ID, "request-changes", 1); code != 0 || got.Awaiting != "fix" {
		t.Fatalf("request_changes：%+v", got)
	}
	agent.session = "s-agent-2"
	reports, _ := filepath.Glob(filepath.Join(agent.dir, "tree", "*", "*", hA, "verdicts", "*.md"))
	if len(reports) != 1 {
		t.Fatalf("検証結果のファイル：%v", reports)
	}
	old, _ := os.ReadFile(reports[0])
	_ = os.WriteFile(reports[0], []byte(strings.Replace(string(old), "初版", "直した版", 1)), 0o644)
	agent.commitPush("b2", "A の検証を直した")
	w.eventually("2 つ目の先頭のコミットが読む側に出る", 30*time.Second, func() bool { return agent.headReady(ar2.ID, 2) })
	if got, code, out := reviewer.review(ar2.ID, "approve", 2); code != 0 || got.State != "merged" {
		t.Fatalf("版 2 の approve：%d %s", code, out)
	}
	var node struct {
		Node    map[string]any `json:"node"`
		Verdict map[string]any `json:"verdict"`
	}
	agent.must(&node, "node", "view", hA)
	if node.Node["value"] != true || !strings.Contains(fmt.Sprint(node.Verdict["report_body"]), "直した版") {
		t.Fatalf("A：%+v", node)
	}

	// ---- 人への依頼：起票、回答、回答の引用、作業から外れる ----
	agent.session = "s-agent-3"
	var hr struct {
		ID string `json:"id"`
	}
	agent.must(&hr, "human-request", "create", "--node", hB, "--kind", "pvr", "--body", agent.file("pvr.md", "B を測る\n"))
	photo := []byte("measured-photo")
	if code, body := w.call("alice", "POST", "/repositories/q/human-requests/"+hr.ID+"/answer", map[string]any{"body": "測った。B は成り立つ",
		"attachments": []map[string]string{{"kind": "file", "name": "photo.jpg", "content_base64": base64.StdEncoding.EncodeToString(photo)}}}); code != 201 {
		t.Fatalf("回答：%d %s", code, body)
	}
	hasWork := func(kind, subject string) bool {
		var raw struct {
			Items []map[string]string `json:"items"`
		}
		agent.must(&raw, "work", "list")
		for _, it := range raw.Items {
			if it["kind"] == kind && it["subject_id"] == subject {
				return true
			}
		}
		return false
	}
	if !hasWork("human_request_answered", hr.ID) {
		t.Fatal("回答済みの依頼が、作業にない")
	}
	agent.branch("b3")
	agent.must(nil, "verdict", "new", "--hypothesis", hB, "--value", "true", "--report", agent.file("r2.md", "回答のとおり\n"), "--cite", "answer:"+hr.ID, "--repo", "q")
	agent.commitPush("b3", "B を検証")
	ar3 := agent.create("b3")
	if hasWork("human_request_answered", hr.ID) {
		t.Fatal("回答を引用した提案があるのに、作業から外れない")
	}
	if got, code, out := reviewer.review(ar3.ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("B の approve：%d %s", code, out)
	}
	var comp struct {
		RootValue any  `json:"root_value"`
		Done      bool `json:"done"`
	}
	agent.must(&comp, "tree", "completion", "--repo", "q")
	if comp.RootValue != true || !comp.Done {
		t.Fatalf("作業の終わり：%+v", comp)
	}

	// ---- レビューの鎖が切れたら、close して、同じブランチから作り直す ----
	// worker が控えに足したレビューは、書き換えられても控えのものを使うので、鎖は切れない。
	// 鎖が切れるのは、控えに足す前に書き換えられたときである。worker を止めて、その場合を作る。
	agent.session = "s-agent-4"
	agent.branch("b4")
	agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("bg.md", "背景を足す\n"))
	agent.commitPush("b4", "背景")
	ar4 := agent.create("b4")
	w.stopWorker()
	for i := 0; i < 2; i++ {
		if _, code, _ := reviewer.review(ar4.ID, "comment", 1); code != 0 {
			t.Fatal("comment")
		}
	}
	w.tamperFirstReview("q", ar4.ID)
	w.restartWorker()
	if _, code, out := reviewer.review(ar4.ID, "comment", 1); code != 1 || !strings.Contains(out, "not_open") || !strings.Contains(out, "作り直す") {
		t.Fatalf("鎖が切れたときのレビュー：%d %s", code, out)
	}
	agent.must(nil, "ar", "close", ar4.ID, "--reason", "鎖が切れたので作り直す")
	var ar5 applyRequest
	agent.must(&ar5, "ar", "create", "--branch", "b4")
	if ar5.ID == ar4.ID || ar5.State != "open" {
		t.Fatalf("作り直しで、閉じた apply request が返った：%+v", ar5)
	}
	if got, code, out := reviewer.review(ar5.ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("作り直したものの approve：%d %s", code, out)
	}

	// ---- 読めない変化は、check もバックエンドも、同じ規則で拒む ----
	agent.branch("b5")
	rootFile, _ := filepath.Glob(filepath.Join(agent.dir, "tree", repo.Root, "hypothesis.md"))
	_ = os.WriteFile(rootFile[0], []byte("---\nid: "+repo.Root+"\nstatement: 書き換えた問い\n---\n"), 0o644)
	agent.commitPush("b5", "書き換え")
	code, out := agent.cli("", "check", "--json")
	if code != 1 || !strings.Contains(out, "change_not_allowed") {
		t.Fatalf("check：%d %s", code, out)
	}
	code, out = agent.cli("", "ar", "create", "--branch", "b5", "--json")
	if code != 1 || !strings.Contains(out, "validation_failed") || !strings.Contains(out, "change_not_allowed") {
		t.Fatalf("ar create：%d %s", code, out)
	}

	// ---- 書き出しと掃除 ----
	code, out = agent.cli("", "tree", "export", "--format", "markdown")
	if code != 0 || !strings.Contains(out, "## 仮説 "+hA) {
		t.Fatalf("export：%d", code)
	}
	var cleaned struct {
		Deleted []string `json:"deleted"`
	}
	agent.git("checkout", "--quiet", "main")
	agent.must(&cleaned, "ar", "cleanup")
	if len(cleaned.Deleted) < 4 {
		t.Fatalf("cleanup：%+v", cleaned)
	}
}

// tamperFirstReview は、管理者として、apply request の pull request の最初のレビューの記録を書き換える（署名が合わなくなる）。
func (w *world) tamperFirstReview(repo, arID string) {
	w.t.Helper()
	ref := w.org + "/" + repo
	var comments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	w.forgejoCall(w.admin, "GET", fmt.Sprintf("/repos/%s/issues/%d/comments", ref, w.pullNumber(repo, arID)), nil, &comments, 200)
	for _, c := range comments {
		if strings.Contains(c.Body, "Logiforge-Record: review") {
			w.forgejoCall(w.admin, http.MethodPatch, fmt.Sprintf("/repos/%s/issues/comments/%d", ref, c.ID), map[string]string{"body": c.Body + "x"}, nil, 200)
			return
		}
	}
	w.t.Fatalf("%s のレビューの記録がない", arID)
}
