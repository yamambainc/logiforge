package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/settings"
)

// ディレクトリ方式（design/directory/）：プロジェクトのリポジトリの中の 2 つのディレクトリを、別々の repository として運用する。
//   - 用意は、利用者のトークンで、ディレクトリに用意のコミット（init-directory の署名）を置く。
//   - apply request の反映は、そのディレクトリの版になる。コードの merge は、版にならず、壊れた状態にもしない。
//   - ディレクトリの外も変える apply request は、作れない（outside_directory）。
//   - 人が、apply request でない PR でディレクトリを変えて merge すると、そのディレクトリの repository だけが壊れる。
func TestE2EDirectory(t *testing.T) {
	const project = "proj"
	w := setupFull(t, func(w *world, c *settings.Config) {
		ref := w.org + "/" + project
		c.Repositories = []settings.Repository{{Name: "cache", GitForgeRef: ref, Directory: "logiforge/cache"}, {Name: "index", GitForgeRef: ref, Directory: "logiforge/index"}}
	}, nil)
	ref := w.org + "/" + project
	// 管理者が、コードのあるプロジェクトのリポジトリを作り、利用者に Write を与える。
	w.forgejoCall(w.admin, "POST", "/orgs/"+w.org+"/repos", map[string]any{"name": project, "auto_init": true, "default_branch": "main", "private": true}, nil, 201)
	w.forgejoCall(w.admin, "PUT", fmt.Sprintf("/repos/%s/collaborators/%s", ref, w.login("alice")), map[string]string{"permission": "write"}, nil, 204)

	for _, name := range []string{"cache", "index"} {
		if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": name, "statement": name + " の問い"}); st != 201 && st != 202 {
			t.Fatalf("%s の作成：%d %s", name, st, b)
		}
		w.eventually(name+" のスナップショットツリーが出る", 60*time.Second, func() bool {
			st, _ := w.call("alice", "GET", "/repositories/"+name+"/tree", nil)
			return st == 200
		})
	}
	var info struct {
		Directory string `json:"directory"`
		Root      string `json:"root_hypothesis_id"`
	}
	_, b := w.call("alice", "GET", "/repositories/cache", nil)
	if json.Unmarshal(b, &info) != nil || info.Directory != "logiforge/cache" {
		t.Fatalf("Repository の directory：%s", b)
	}

	agent := w.user("agent", "s-agent-dir")
	agent.repo = "cache"
	agent.loginAndClone("cache")
	reviewer := w.user("reviewer", "s-rev-dir")
	reviewer.repo = "cache"
	if code, _ := reviewer.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}
	if got := agent.git("config", "--get", "logiforge.directory"); got != "logiforge/cache" {
		t.Fatalf("clone の logiforge.directory：%q", got)
	}

	// ---- apply request の反映は、cache の版になる ----
	agent.branch("bg")
	agent.must(nil, "node", "edit", info.Root, "--append-background", agent.file("bg.md", "ディレクトリの背景\n"))
	if !strings.Contains(agent.git("status", "--porcelain"), "logiforge/cache/tree/") {
		t.Fatalf("node edit が、ディレクトリの中に書いていない：%s", agent.git("status", "--porcelain"))
	}
	agent.commitPush("bg", "背景")
	ar := agent.create("bg")
	if got, code, out := reviewer.review(ar.ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("approve：%d %s", code, out)
	}
	var node struct {
		Node struct {
			Background string `json:"background"`
		} `json:"node"`
	}
	agent.must(&node, "node", "view", info.Root)
	if !strings.Contains(node.Node.Background, "ディレクトリの背景") {
		t.Fatalf("反映が木にない：%+v", node)
	}

	// ---- コードの merge は版にならず、壊さない ----
	w.humanPull(ref, "code-change", map[string]string{"src/main.go": "package main\n"})
	w.eventually("コードの merge のあと、壊れていない", 30*time.Second, func() bool {
		code, _ := agent.cli("", "repo", "view", "cache", "--json")
		return code == 0
	})
	var commits struct {
		Items []struct {
			Number int `json:"number"`
		} `json:"items"`
	}
	_, cb := w.call("alice", "GET", "/repositories/cache/commits", nil)
	if json.Unmarshal(cb, &commits) != nil || len(commits.Items) != 2 {
		t.Fatalf("cache の版は、用意と反映の 2 つ：%s", cb)
	}

	// ---- ディレクトリの外も変える apply request は作れない ----
	agent.branch("both")
	agent.must(nil, "node", "edit", info.Root, "--append-background", agent.file("bg2.md", "外も変える\n"))
	agent.write("README.md", "書き換え\n")
	agent.commitPush("both", "外も")
	if code, out := agent.cli("", "ar", "create", "--branch", "both", "--json"); code == 0 || !strings.Contains(out, "outside_directory") {
		t.Fatalf("外も変える apply request が作れた：%d %s", code, out)
	}

	// 壊れたあとは clone できない（git を読むコマンドは repository_broken）ので、直す人の clone を先に作っておく。
	fixer := w.user("agent", "s-agent-fix")
	fixer.repo = "index"
	fixer.loginAndClone("index")

	// ---- 人が、apply request でない PR で index を変えて merge すると、index だけが壊れる ----
	w.humanPull(ref, "edit-index", map[string]string{"logiforge/index/tree/note.md": "x\n"})
	w.eventually("index が壊れる", 120*time.Second, func() bool {
		code, _ := agent.cli("", "repo", "view", "index", "--json")
		return code == 1
	})
	if code, out := agent.cli("", "repo", "view", "cache", "--json"); code != 0 {
		t.Fatalf("cache まで壊れた：%s", out)
	}

	// ---- index を、repair revert で直す（ディレクトリの中だけを戻す） ----
	var rv struct {
		Branch string `json:"branch"`
	}
	fixer.must(&rv, "repair", "revert")
	if st := fixer.git("show", "--stat", "--format=", "HEAD"); !strings.Contains(st, "logiforge/index/tree/note.md") || strings.Contains(st, "README") || strings.Contains(st, "src/") {
		t.Fatalf("repair revert が、ディレクトリの中だけを戻していない：%s", st)
	}
	fixer.git("push", "--quiet", "origin", rv.Branch)
	var fix applyRequest
	fixer.must(&fix, "ar", "create", "--branch", rv.Branch, "--kind", "repair.revert")
	w.eventually("worker が直すブランチを取り込む", 30*time.Second, func() bool { return fixer.headReady(fix.ID, 1) })
	idxReviewer := w.user("reviewer", "s-rev-idx")
	idxReviewer.repo = "index"
	if code, _ := idxReviewer.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}
	if got, code, out := idxReviewer.review(fix.ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("戻す apply request の approve：%d %s", code, out)
	}
	w.eventually("index が直る", 60*time.Second, func() bool {
		code, _ := agent.cli("", "repo", "view", "index", "--json")
		return code == 0
	})
}

// humanPull は、管理者として、main からブランチを作ってファイルを置き、pull request を作って merge する（人が GitForge で行うのと同じ）。
func (w *world) humanPull(ref, branch string, files map[string]string) string {
	w.t.Helper()
	var ops []map[string]string
	for p, c := range files {
		ops = append(ops, map[string]string{"operation": "create", "path": p, "content": base64.StdEncoding.EncodeToString([]byte(c))})
	}
	w.forgejoCall(w.admin, "POST", "/repos/"+ref+"/contents", map[string]any{"branch": "main", "new_branch": branch, "message": branch, "files": ops}, nil, 201)
	var pr struct {
		Number int `json:"number"`
	}
	w.forgejoCall(w.admin, "POST", "/repos/"+ref+"/pulls", map[string]any{"head": branch, "base": "main", "title": branch}, &pr, 201)
	deadline := time.Now().Add(30 * time.Second)
	for {
		if st := w.forgejoCall(w.admin, "POST", fmt.Sprintf("/repos/%s/pulls/%d/merge", ref, pr.Number), map[string]any{"Do": "merge"}, nil); st == 200 {
			break
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("%s の merge ができない", branch)
		}
		time.Sleep(500 * time.Millisecond)
	}
	var b struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	w.forgejoCall(w.admin, "GET", "/repos/"+ref+"/branches/main", nil, &b, 200)
	return b.Commit.ID
}
