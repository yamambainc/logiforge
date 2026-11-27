package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestWebUIMobileApplyRequest は、スマホの幅（390x844）で apply request の画面を開いたとき、
// 説明の 1 行目（タイトル）が長くても、反映したあとの木が見えて、最後の行までスクロールできることを確かめる。
// タイトルは 1 行で省き、はみ出したときだけ開閉できることも（スマホと PC の幅で）確かめる。
// ブラウザの操作は webui/e2e/mobile-ar.mjs が行う。
func TestWebUIMobileApplyRequest(t *testing.T) {
	w, chrome := setupWebUI(t)
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "q", "statement": "スマホでレビューする"}); st != 201 {
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
	agent := w.user("agent", "s-agent-m")
	agent.loginAndClone("q")

	// 画面に収まらないだけの行（仮説 60 個）を足す apply request。説明の 1 行目は長い
	agent.branch("m1")
	args := []string{"story", "new", "--parent", repo.Root, "--title", "スマホで見る道筋", "--summary", "s"}
	for i := 1; i <= 60; i++ {
		args = append(args, "--hypothesis", fmt.Sprintf("仮説 %d：スマホの幅でも、この行まで読める", i))
	}
	agent.must(nil, args...)
	agent.commitPush("m1", "分解")
	ar := agent.create("m1")
	title := strings.Repeat("とても長いタイトルの apply request で、スマホの幅では何行にも折り返す。", 4)
	agent.must(nil, "ar", "edit", ar.ID, "--description", title+"\n\n本文。")
	// タイトルが 1 行に収まる apply request（▸ を出さない）
	agent.branch("m2")
	agent.must(nil, "story", "new", "--parent", repo.Root, "--title", "短い", "--summary", "s", "--hypothesis", "短い仮説")
	agent.commitPush("m2", "短い")
	short := agent.create("m2")
	w.eventually("反映したあとの木を読める", 60*time.Second, func() bool {
		st, _ := w.call("alice", "GET", "/repositories/q/tree/nodes?apply_request="+ar.ID, nil)
		return st == 200
	})

	out := os.Getenv("LOGIFORGE_E2E_OUT")
	if out == "" {
		out = t.TempDir()
	}
	login := w.login("alice")
	runBrowser(t, "e2e/mobile-ar.mjs", "BASE="+w.api, "LOGIN="+login, "PASSWORD=pw-"+login+"-12345", "CHROME="+chrome, "OUT="+out, "REPO=q", "AR="+ar.ID, "AR_SHORT="+short.ID)
}
