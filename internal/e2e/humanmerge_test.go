package e2e

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// main は、だれがどこから merge しても、そのまま受け入れる（design/backend/invalid-main.md の「main はいつも正しい」）。
//   - apply request を、人が Forgejo の画面から merge しても、木に入り、コミットの一覧に、その apply request の反映と示す。
//   - approve のあとに request_changes が付いた apply request を、人が merge しても、受け入れる。review は見ない。
//   - コードを書く権限のない reviewer の approve は数えず、権限のある reviewer の approve し直しで反映される。
//
// approve のときに Logiforge が今の main で確かめないよう、向き先を main でないブランチにしてからレビューし、
// あとで main に戻す（Forgejo の画面で向き先を変えるのと同じ）。人の merge は、Logiforge を通さない。
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
	type validity struct {
		InvalidSince *string `json:"invalid_since"`
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
	var info validity
	if code, out := agent.cli("", "repo", "view", "q", "--json"); code != 0 || json.Unmarshal([]byte(out), &info) != nil || info.InvalidSince != nil {
		t.Fatalf("人の merge で、valid でなくなった：%d %s", code, out)
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

	// ---- コードを書く権限のない reviewer の approve は、数えない ----
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
			State    string   `json:"state"`
			Awaiting *string  `json:"awaiting"`
			Warnings []string `json:"warnings"`
		} `json:"apply_request"`
	}
	// 書く権限（コード）のない人の approve は、数える review にならない（design/adapters/pull-requests.md の「数える review」）。
	// そのため、レビュー待ちのままで、警告を返す。数えない approve しかないので、Logiforge は merge を断る（not_approved）。
	_, code, out = weak.review(fb.ID, "approve", 1)
	if code != 0 || json.Unmarshal([]byte(out), &forbidden) != nil {
		t.Fatalf("書く権限のない reviewer の approve：%d %s", code, out)
	}
	if code, out := weak.cli("", "ar", "merge", fb.ID, "--repo", "q", "--json"); code != 1 || !strings.Contains(out, "not_approved") {
		t.Fatalf("書く権限のない reviewer の merge：%d %s", code, out)
	}
	if forbidden.ApplyRequest.State != "open" ||
		forbidden.ApplyRequest.Awaiting == nil || *forbidden.ApplyRequest.Awaiting != "review" || !strings.Contains(strings.Join(forbidden.ApplyRequest.Warnings, " "), "数えない") {
		t.Fatalf("書く権限のない reviewer の approve を数えている：%s", out)
	}
	// merge の権限のある reviewer が approve し直せば、反映される。
	reviewer.session = "s-rev-hm-3"
	if got, code, out := reviewer.approveMerge(fb.ID, 1); code != 0 || got.State != "merged" {
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
	sha = humanMerge(ng, "squash")
	w.eventually("worker が request_changes のあとの人の merge を取り込む", 90*time.Second, func() bool { return ingested(sha) })
	info = validity{}
	if code, out := agent.cli("", "repo", "view", "q", "--json"); code != 0 || json.Unmarshal([]byte(out), &info) != nil || info.InvalidSince != nil {
		t.Fatalf("request_changes のあとの人の merge で、valid でなくなった：%d %s", code, out)
	}
	if id, _ := mainCommit(sha); id == nil || *id != ng.ID {
		t.Fatalf("request_changes のあとの merge が、%s の反映と示されない：%v", ng.ID, id)
	}
}

// pullNumber は、apply request の pull request の番号を返す。apply request の識別子は、pull request の番号である。
func (w *world) pullNumber(repo, arID string) int {
	w.t.Helper()
	n, err := strconv.Atoi(arID)
	if err != nil || n <= 0 {
		w.t.Fatalf("%s は pull request の番号でない", arID)
	}
	return n
}
