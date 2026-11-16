package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 向き先が main でない apply request（積み上げた pull request）は、向き先のブランチの今の先頭に、衝突を判定せずに重ねて読める（issue #36）。
// 下の develop と、その上の b2 が、同じファイル（根の仮説の背景情報）を変えても、衝突にならず、b2 の内容が出る。
func TestStackedApplyRequest(t *testing.T) {
	w := setup(t)
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "q", "statement": "積み上げても読める"}); st != 201 {
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
	agent := w.user("agent", "s-agent-st")
	agent.loginAndClone("q")

	// 下：develop。上：b2（develop から）。どちらも根の仮説の背景情報を書く。
	agent.branch("develop")
	agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("bg1.md", "下の背景\n"))
	agent.commitPush("develop", "下")
	dev := strings.TrimSpace(agent.git("rev-parse", "HEAD"))
	agent.git("checkout", "--quiet", "-B", "b2", "develop")
	agent.must(nil, "node", "edit", repo.Root, "--append-background", agent.file("bg2.md", "上の背景\n"))
	agent.commitPush("b2", "上")
	ar := agent.create("b2")

	w.retarget("q", ar.ID, "develop")

	var nodes struct {
		Commit string `json:"commit"`
		Nodes  []struct {
			ID         string `json:"id"`
			Background string `json:"background"`
		} `json:"nodes"`
	}
	w.eventually("向き先のブランチに重ねて読める", 60*time.Second, func() bool {
		st, b := w.call("alice", "GET", "/repositories/q/tree/nodes?apply_request="+ar.ID, nil)
		return st == 200 && json.Unmarshal(b, &nodes) == nil && nodes.Commit == dev
	})
	if nodes.Nodes[0].ID != repo.Root || !strings.Contains(nodes.Nodes[0].Background, "上の背景") {
		t.Fatalf("上の apply request の内容が出ない：%+v", nodes.Nodes[0])
	}
	// 向き先のブランチのコミットも、commit に指定して読める。
	if st, b := w.call("alice", "GET", "/repositories/q/nodes/"+repo.Root+"?commit="+dev, nil); st != 200 || !strings.Contains(string(b), "下の背景") {
		t.Fatalf("ブランチのコミット：%d %s", st, b)
	}
	// レビューの材料は作らない（Logiforge は検査しない）。
	if st, b := w.call("alice", "GET", "/repositories/q/apply-requests/"+ar.ID+"/review-material", nil); st != 422 || !strings.Contains(string(b), "overlay_unsupported") {
		t.Fatalf("レビューの材料：%d %s", st, b)
	}
	// main のコミットの一覧（listCommits）は、新しい順。
	var commits struct {
		Items []struct {
			Number int    `json:"number"`
			Commit string `json:"commit"`
		} `json:"items"`
	}
	if st, b := w.call("alice", "GET", "/repositories/q/commits", nil); st != 200 || json.Unmarshal(b, &commits) != nil || len(commits.Items) == 0 || commits.Items[0].Number != len(commits.Items) {
		t.Fatalf("listCommits：%d %s", st, b)
	}
}

// retarget は、apply request の pull request の向き先を変える（GitForge の画面で、積み上げた pull request にするのと同じ）。
func (w *world) retarget(repo, arID, base string) {
	w.t.Helper()
	ref := w.org + "/" + repo
	var pulls []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	}
	w.forgejoCall(w.admin, "GET", "/repos/"+ref+"/pulls?state=open&limit=50", nil, &pulls, 200)
	for _, p := range pulls {
		if strings.Contains(p.Title, arID) {
			w.forgejoCall(w.admin, http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%d", ref, p.Number), map[string]any{"base": base}, nil, 201)
			return
		}
	}
	w.t.Fatalf("%s の pull request がない：%+v", arID, pulls)
}
