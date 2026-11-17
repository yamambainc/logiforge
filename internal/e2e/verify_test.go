package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 反映かは、だれがどこから merge したかでなく、中身で決める（design/backend/verify.md）。
//   - 承認された apply request を、人が Forgejo の画面から merge しても、正しい反映になる。
//   - approve のあとに request_changes が付いた apply request を、人が merge すると、壊れた状態になる。
//   - merge の権限のない reviewer の approve は、反映の失敗 merge_forbidden になり、権限のある reviewer の approve し直しで反映される。
//
// approve したときに Logiforge が反映しないよう、向き先を main でないブランチにしてからレビューし、
// あとで main に戻す（Forgejo の画面で向き先を変えるのと同じ）。
func TestE2EHumanMerge(t *testing.T) {
	w := setup(t)
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "q", "statement": "だれが merge しても中身で決める"}); st != 201 {
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
	agent := w.user("agent", "s-agent-hm")
	agent.loginAndClone("q")
	reviewer := w.user("reviewer", "s-rev-hm")
	if code, _ := reviewer.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}
	ref := w.org + "/q"
	agent.branch("side")
	agent.git("push", "--quiet", "origin", "side")

	// prepare は、根の仮説の背景情報に 1 行足す apply request を作り、向き先を side にする。
	prepare := func(branch, line string) applyRequest {
		t.Helper()
		agent.branch(branch)
		agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file(branch+".md", line+"\n"))
		agent.commitPush(branch, line)
		ar := agent.create(branch)
		w.retarget("q", ar.ID, "side")
		w.eventually(ar.ID+" の向き先が side になる", 30*time.Second, func() bool {
			var v struct {
				Target string `json:"target"`
			}
			agent.must(&v, "ar", "view", ar.ID, "--repo", "q")
			return v.Target == "side"
		})
		return ar
	}
	// humanMerge は、Forgejo の API で、repository を作った利用者（管理者）として merge する。Logiforge を通さない。
	humanMerge := func(ar applyRequest, do string) string {
		t.Helper()
		w.retarget("q", ar.ID, "main")
		number := w.pullNumber("q", ar.ID)
		w.forgejoCall(w.tokens["alice"], "POST", fmt.Sprintf("/repos/%s/pulls/%d/merge", ref, number),
			map[string]any{"Do": do, "head_commit_id": ar.HeadCommit, "MergeTitleField": "画面から merge"}, nil, 200)
		var b struct {
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}
		w.forgejoCall(w.admin, "GET", "/repos/"+ref+"/branches/main", nil, &b, 200)
		return b.Commit.ID
	}
	type brokenInfo struct {
		Broken *struct {
			FirstCommit string `json:"first_commit"`
		} `json:"broken"`
	}
	// mainCommit は、worker が取り込んだ main のコミット sha の、反映した apply request を返す。まだ取り込んでいなければ false。
	mainCommit := func(sha string) (*string, bool) {
		var list struct {
			Items []struct {
				Commit       string  `json:"commit"`
				ApplyRequest *string `json:"apply_request_id"`
			} `json:"items"`
		}
		st, b := w.call("alice", "GET", "/repositories/q/commits", nil)
		if st != 200 || json.Unmarshal(b, &list) != nil {
			return nil, false
		}
		for _, c := range list.Items {
			if c.Commit == sha {
				return c.ApplyRequest, true
			}
		}
		return nil, false
	}
	ingested := func(sha string) bool { _, ok := mainCommit(sha); return ok }

	// ---- 承認された apply request を、人が merge のコミットで merge する ----
	ok := prepare("approved", "承認された変更")
	got, code, out := reviewer.review(ok.ID, "approve", 1)
	if code != 0 || got.State != "open" {
		t.Fatalf("向き先が side のときの approve：%d %s", code, out)
	}
	sha := humanMerge(ok, "merge")
	w.eventually("worker が人の merge を取り込む", 90*time.Second, func() bool { return ingested(sha) })
	var info brokenInfo
	if code, out := agent.cli("", "repo", "view", "q", "--json"); code != 0 || json.Unmarshal([]byte(out), &info) != nil || info.Broken != nil {
		t.Fatalf("承認された apply request の人の merge で、壊れた：%d %s", code, out)
	}
	var node struct {
		Node struct {
			Background string `json:"background"`
		} `json:"node"`
	}
	agent.must(&node, "node", "view", repo.Root)
	if !strings.Contains(node.Node.Background, "承認された変更") {
		t.Fatalf("人の merge の変更が、木にない：%+v", node)
	}
	var merged applyRequest
	agent.must(&merged, "ar", "view", ok.ID, "--repo", "q")
	if merged.State != "merged" {
		t.Fatalf("人の merge のあとの状態：%+v", merged)
	}
	if id, _ := mainCommit(sha); id == nil || *id != ok.ID {
		t.Fatalf("人の merge のコミットが、%s の反映にならない：%v", ok.ID, id)
	}

	// ---- merge の権限のない reviewer の approve：GitForge が merge を断り、反映の失敗 merge_forbidden として記録する ----
	// コードを読むだけで、pull request と issue を書ける team の、エージェント（client）。
	var nm struct {
		ID int64 `json:"id"`
	}
	w.forgejoCall(w.admin, "POST", "/orgs/"+w.org+"/teams", map[string]any{"name": "no-merge", "permission": "write", "includes_all_repositories": true,
		"units_map": map[string]string{"repo.code": "read", "repo.pulls": "write", "repo.issues": "write"}}, &nm, 201)
	login := w.login("nomerge")
	w.forgejoCall(w.admin, "POST", "/admin/users", map[string]any{"username": login, "email": login + "@example.com", "password": "pw-" + login + "-12345", "must_change_password": false}, nil, 201)
	w.forgejoCall(w.admin, "PUT", fmt.Sprintf("/teams/%d/members/%s", nm.ID, login), nil, nil, 204)
	w.tokens["nomerge"] = w.token(login, []string{"all"})
	weak := w.user("nomerge", "s-weak")
	if code, _ := weak.cli(w.tokens["nomerge"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("merge の権限のない reviewer の login")
	}
	agent.branch("forbidden")
	agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("forbidden.md", "権限のない reviewer\n"))
	agent.commitPush("forbidden", "権限のない reviewer")
	fb := agent.create("forbidden")
	var forbidden struct {
		ApplyRequest struct {
			State         string  `json:"state"`
			Awaiting      *string `json:"awaiting"`
			MergeFailures []struct {
				Reason string `json:"reason"`
			} `json:"merge_failures"`
		} `json:"apply_request"`
	}
	_, code, out = weak.review(fb.ID, "approve", 1)
	if code != 0 || json.Unmarshal([]byte(out), &forbidden) != nil {
		t.Fatalf("merge の権限のない reviewer の approve：%d %s", code, out)
	}
	fs := forbidden.ApplyRequest.MergeFailures
	if forbidden.ApplyRequest.State != "open" || len(fs) == 0 || fs[len(fs)-1].Reason != "merge_forbidden" ||
		forbidden.ApplyRequest.Awaiting == nil || *forbidden.ApplyRequest.Awaiting != "review" {
		t.Fatalf("merge_forbidden として記録しない：%s", out)
	}
	// merge の権限のある reviewer が approve し直せば、反映される。
	reviewer.session = "s-rev-hm-3"
	if got, code, out := reviewer.review(fb.ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("権限のある reviewer の approve：%d %s", code, out)
	}
	reviewer.session = "s-rev-hm"

	// ---- approve のあとに request_changes が付いた apply request を、人が squash merge する ----
	ng := prepare("changes-requested", "直すよう求められた変更")
	if _, code, out := reviewer.review(ng.ID, "approve", 1); code != 0 {
		t.Fatalf("approve：%d %s", code, out)
	}
	reviewer.session = "s-rev-hm-2"
	if _, code, out := reviewer.review(ng.ID, "request-changes", 1); code != 0 {
		t.Fatalf("request_changes：%d %s", code, out)
	}
	// worker が記録を控えに足すのを待ってから、merge する（控えにない記録は、消されると判定に使えない）。
	time.Sleep(3 * time.Second)
	sha = humanMerge(ng, "squash")
	w.eventually("request_changes のあとの人の merge で壊れる", 120*time.Second, func() bool {
		code, out := agent.cli("", "repo", "view", "q", "--json")
		info = brokenInfo{}
		return code == 1 && json.Unmarshal([]byte(out), &info) == nil && info.Broken != nil
	})
	if info.Broken.FirstCommit != sha {
		t.Fatalf("壊れた最初のコミット：%s（merge したのは %s）", info.Broken.FirstCommit, sha)
	}
	if id, _ := mainCommit(sha); id != nil {
		t.Fatalf("request_changes のあとの merge が、%s の反映になった", *id)
	}
}

// pullNumber は、apply request の pull request の番号を返す。
func (w *world) pullNumber(repo, arID string) int {
	w.t.Helper()
	var pulls []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	w.forgejoCall(w.admin, "GET", "/repos/"+w.org+"/"+repo+"/pulls?state=all&limit=50", nil, &pulls, 200)
	for _, p := range pulls {
		if strings.Contains(p.Title, arID) {
			return p.Number
		}
	}
	w.t.Fatalf("%s の pull request がない", arID)
	return 0
}
