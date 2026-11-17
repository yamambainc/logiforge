package e2e

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

// TestE2ELargeTree は、main のコミットが多い repository で、worker の初めての取り込みと、
// push の直後の ar create（バックエンドの作業用の git の複製の、初めての clone を含む）と、2 回目の ar create（fetch だけ）が、
// どれだけかかるかを測る。
// 時間がかかるので、E2E_LARGE_COMMITS（main のコミットの数）があるときだけ動かす。
func TestE2ELargeTree(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("E2E_LARGE_COMMITS"))
	if n <= 0 {
		t.Skip("E2E_LARGE_COMMITS がない（例：E2E_LARGE_COMMITS=300）")
	}
	w := setup(t)
	if code, body := w.call("alice", "POST", "/repositories", map[string]string{"name": "big", "statement": "大きな問い"}); code != 201 {
		t.Fatalf("repository の作成：%d %s", code, body)
	}
	ref := w.org + "/big"
	// 管理者として main の守りを外し、反映のコミットを、まとめて直接 push する（反映の手順を n 回通すと遅いため）。
	w.forgejoCall(w.admin, "DELETE", "/repos/"+ref+"/branch_protections/main", nil, nil, 204)
	admin := w.user("root", "s-gen")
	u, _ := url.Parse(w.forgejo)
	u.User = url.UserPassword("root", w.admin)
	admin.dir = admin.home
	admin.git("clone", "--quiet", u.String()+"/"+ref+".git", "big")
	admin.dir = filepath.Join(admin.home, "big")
	roots, _ := filepath.Glob(filepath.Join(admin.dir, "tree", "h-*"))
	leaves := []string{strings.TrimPrefix(roots[0], admin.dir+"/")}
	nodes := 1
	for i := 0; i < n; i++ {
		parent := leaves[0]
		leaves = leaves[1:]
		sid := fmt.Sprintf("s-%010d", i)
		sdir := parent + "/" + sid
		admin.write(sdir+"/story.md", string(tree.MarshalStory(tree.StoryContent{ID: sid, Title: fmt.Sprintf("道筋 %d", i), Summary: strings.Repeat("概要", 20)})))
		for j := 0; j < 3; j++ {
			hid := fmt.Sprintf("h-%08d%02d", i, j)
			hdir := sdir + "/" + hid
			admin.write(hdir+"/hypothesis.md", string(tree.MarshalHypothesis(tree.HypothesisContent{ID: hid, Statement: fmt.Sprintf("仮説 %d-%d", i, j)})))
			admin.write(hdir+"/background.md", strings.Repeat("背景の文章。", 50)+"\n")
			leaves = append(leaves, hdir)
		}
		nodes += 4
		admin.git("add", "-A")
		admin.git("commit", "--quiet", "-m", fmt.Sprintf("反映 %d\n\nLogiforge-Apply-Request: apr_gen%d\nLogiforge-Session-Id: s-gen", i, i))
	}
	head := admin.git("rev-parse", "HEAD")
	// ブランチも、同時に push する（worker の初めての取り込みと、ar create が重なる場合）。
	admin.git("checkout", "--quiet", "-b", "big-edit")
	admin.write(leaves[0]+"/background.md", strings.Repeat("背景の文章。", 50)+"\n足した文章\n")
	admin.git("commit", "--quiet", "-am", "背景を足す\n\nLogiforge-Session-Id: s-gen")
	admin.git("push", "--quiet", "origin", "main", "big-edit")
	pushed := time.Now()
	w.forgejoCall(w.admin, "POST", "/repos/"+ref+"/branch_protections", map[string]any{"rule_name": "main", "enable_push": false,
		"enable_merge_whitelist": false}, nil, 201)

	agent := w.user("agent", "s-agent")
	if code, _ := agent.cli(w.tokens["agent"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("login")
	}
	agent.dir = admin.dir // ar create は、手元と remote の先頭を比べるので、push した clone で呼ぶ
	agent.git("config", "logiforge.repository", "big")
	// ar create は、worker を待たない。バックエンドが、作業用の git の複製から読んで検査する。
	// 初めての要求では、バックエンドが repository を clone する。2 回目からは fetch だけである。
	create := func(branch string) (applyRequest, time.Duration) {
		start := time.Now()
		code, out := agent.cli("", "ar", "create", "--branch", branch, "--json")
		if code != 0 {
			t.Fatalf("ar create %s：%d %s", branch, code, out)
		}
		var ar applyRequest
		_ = json.Unmarshal([]byte(out), &ar)
		if len(ar.Operations) != 1 || ar.Operations[0].Kind != "node.edit" {
			t.Fatalf("ar create %s：%+v", branch, ar)
		}
		return ar, time.Since(start)
	}
	_, first := create("big-edit")
	admin.git("checkout", "--quiet", "-b", "big-edit2", head)
	admin.write(leaves[1]+"/background.md", strings.Repeat("背景の文章。", 50)+"\n別に足した文章\n")
	admin.git("commit", "--quiet", "-am", "背景を足す 2\n\nLogiforge-Session-Id: s-gen")
	admin.git("push", "--quiet", "origin", "big-edit2")
	_, second := create("big-edit2")
	latest := filepath.Join(w.storeDir, filepath.FromSlash(store.LatestKey("big")))
	w.eventually("worker が main に追いつく", 10*time.Minute, func() bool {
		b, _ := os.ReadFile(latest)
		return strings.TrimSpace(string(b)) == head
	})
	ingest := time.Since(pushed)
	t.Logf("大きめの木：main のコミット %d、ノード %d", n+1, nodes)
	t.Logf("push の直後の ar create（バックエンドの初めての clone を含む。worker の取り込みと重なる）：%v", first.Round(time.Millisecond))
	t.Logf("2 回目の ar create（fetch だけ）：%v", second.Round(time.Millisecond))
	t.Logf("push から、worker が main に追いつくまで：%v", ingest.Round(time.Millisecond))

}
