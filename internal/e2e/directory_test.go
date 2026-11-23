package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/settings"
)

// ディレクトリ方式（design/directory/）：プロジェクトのリポジトリの中の 2 つのディレクトリを、別々の repository として運用する。
//   - 用意は、利用者のトークンで、ディレクトリに用意のコミットを置く。
//   - apply request の反映は、そのディレクトリの版になる。コードの merge は、版にならない。
//   - ディレクトリの外も変える apply request は、作れない（outside_directory）。GitForge で直接作った pull request では、
//     apply request の outside_paths に外のパスが出る。
//   - 人が、PR でディレクトリの木を不変条件に反する形にすると、そのディレクトリの repository だけが valid でなくなる。直す PR で戻る。
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
	// 外を変えていない apply request の outside_paths は空。url は、GitForge の pull request の画面。
	w.outsidePaths("cache", ar.ID, "")
	w.outsideBrowser("cache", ar.ID, "ディレクトリの外の変更：なし", "")
	if got, code, out := reviewer.approveMerge(ar.ID, 1); code != 0 || got.State != "merged" {
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

	// ---- コードの merge は版にならない ----
	w.humanPull(ref, "code-change", map[string]string{"src/main.go": "package main\n"})
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
	// GitForge で直接作った pull request は、apply request として読め、外のパスが出る。
	var pull struct {
		Number int `json:"number"`
	}
	w.forgejoCall(w.tokens["agent"], "POST", "/repos/"+ref+"/pulls", map[string]string{"head": "both", "base": "main", "title": "外も"}, &pull, 201)
	w.outsidePaths("cache", fmt.Sprint(pull.Number), "README.md")
	w.outsideBrowser("cache", fmt.Sprint(pull.Number), "ディレクトリの外の変更：1 件", "README.md")
	w.forgejoCall(w.tokens["agent"], "PATCH", fmt.Sprintf("/repos/%s/pulls/%d", ref, pull.Number), map[string]string{"state": "closed"}, nil, 201)

	// ---- 人が、apply request でない PR で index の木を不変条件に反する形にすると、index だけが valid でなくなる ----
	type validity struct {
		ValidCommit  *string `json:"valid_commit"`
		InvalidSince *string `json:"invalid_since"`
	}
	view := func(name string) validity {
		var v validity
		if st, b := w.call("alice", "GET", "/repositories/"+name, nil); st != 200 || json.Unmarshal(b, &v) != nil {
			t.Fatalf("%s：%d %s", name, st, b)
		}
		return v
	}
	before := view("index")
	bad := w.humanPull(ref, "edit-index", map[string]string{"logiforge/index/README.md": "木の外\n"})
	w.eventually("index が valid でなくなる", 120*time.Second, func() bool { return view("index").InvalidSince != nil })
	if v := view("index"); *v.InvalidSince != bad || v.ValidCommit == nil || before.ValidCommit == nil || *v.ValidCommit != *before.ValidCommit {
		t.Fatalf("index：%+v（前は %+v、valid でないのは %s）", v, before, bad)
	}
	if v := view("cache"); v.InvalidSince != nil {
		t.Fatalf("cache まで valid でなくなった：%+v", v)
	}

	// ---- 直す PR（木の外のファイルを消す）で、index が valid に戻る ----
	fixed := w.humanPull(ref, "fix-index", map[string]string{"logiforge/index/README.md": ""})
	w.eventually("index が valid に戻る", 120*time.Second, func() bool {
		v := view("index")
		return v.InvalidSince == nil && v.ValidCommit != nil && *v.ValidCommit == fixed
	})
}

// outsidePaths は、ディレクトリの repository の apply request を読み、outside_paths（, で繋いだもの）と url を確かめる。
// 先頭のコミットを worker が取り込むまでは null なので、出るまで待つ。
func (w *world) outsidePaths(repo, id, want string) {
	w.t.Helper()
	var got struct {
		URL          string    `json:"url"`
		OutsidePaths *[]string `json:"outside_paths"`
	}
	var b []byte
	w.eventually(repo+" の apply request "+id+" の outside_paths が出る", 60*time.Second, func() bool {
		var st int
		st, b = w.call("alice", "GET", "/repositories/"+repo+"/apply-requests/"+id, nil)
		got.OutsidePaths = nil
		return st == 200 && json.Unmarshal(b, &got) == nil && got.OutsidePaths != nil
	})
	if strings.Join(*got.OutsidePaths, ",") != want || !strings.HasPrefix(got.URL, "http") || !strings.HasSuffix(got.URL, "/pulls/"+id) {
		w.t.Fatalf("apply request %s：outside_paths は %q のはず：%s", id, want, b)
	}
}

// outsideBrowser は、WebUI の apply request の画面で、pull request へのリンクと、ディレクトリの外の変更を確かめる。
// Chrome と webui/dist がなければ、何もしない（API の確かめは outsidePaths で済んでいる）。
func (w *world) outsideBrowser(repo, id, line, paths string) {
	w.t.Helper()
	chrome := os.Getenv("LOGIFORGE_E2E_CHROME")
	if _, err := os.Stat("../../webui/dist/index.html"); chrome == "" || err != nil {
		return
	}
	out := os.Getenv("LOGIFORGE_E2E_OUT")
	if out == "" {
		out = w.t.TempDir()
	}
	login := w.login("alice")
	runBrowser(w.t, "e2e/outside.mjs", "BASE="+w.api, "LOGIN="+login, "PASSWORD=pw-"+login+"-12345", "CHROME="+chrome, "OUT="+out,
		"REPO="+repo, "AR="+id, "EXPECT="+line, "PATHS="+paths)
}

// humanPull は、管理者として、main からブランチを作ってファイルを置き（中身が空なら消し）、pull request を作って merge する
// （人が GitForge で行うのと同じ）。
func (w *world) humanPull(ref, branch string, files map[string]string) string {
	w.t.Helper()
	var ops []map[string]string
	for p, c := range files {
		if c == "" {
			var cur struct {
				SHA string `json:"sha"`
			}
			w.forgejoCall(w.admin, "GET", "/repos/"+ref+"/contents/"+p+"?ref=main", nil, &cur, 200)
			ops = append(ops, map[string]string{"operation": "delete", "path": p, "sha": cur.SHA})
			continue
		}
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
