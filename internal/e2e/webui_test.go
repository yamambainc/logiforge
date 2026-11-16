package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWebUISignIn は、ブラウザで WebUI を開き、Forgejo の OAuth2 でサインインして、repository の一覧から
// スナップショットツリーのビュワー（アウトライン、図、詳細のパネル、検索）までを通す。
// LOGIFORGE_E2E_CHROME（Chrome のパス）と、ビルドした webui/dist/index.html がなければ飛ばす。
// ブラウザの操作は webui/e2e/run.mjs（puppeteer-core）が行う。スクリーンショットは LOGIFORGE_E2E_OUT に書く。
func TestWebUISignIn(t *testing.T) {
	chrome := os.Getenv("LOGIFORGE_E2E_CHROME")
	if chrome == "" {
		t.Skip("LOGIFORGE_E2E_CHROME がない")
	}
	if _, err := os.Stat("../../webui/dist/index.html"); err != nil {
		t.Skip("webui/dist がない（webui/ で npm run build）")
	}
	w := setupWith(t, func(w *world, serveURL string) []string {
		// WebUI のサインインに使う OAuth2 のアプリケーション（confidential client）を、admin で登録する
		var app struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		w.forgejoCall(w.admin, "POST", "/user/applications/oauth2", map[string]any{
			"name": "webui-" + w.org, "redirect_uris": []string{serveURL + "/oauth/callback"}, "confidential_client": true,
		}, &app, 201)
		return []string{"LOGIFORGE_OAUTH_CLIENT_ID=" + app.ClientID, "LOGIFORGE_OAUTH_CLIENT_SECRET=" + app.ClientSecret,
			"LOGIFORGE_OAUTH_REDIRECT_URI=" + serveURL + "/oauth/callback"}
	})
	// 利用者が repository を作り、スナップショットツリーが出るまで待つ
	if st, b := w.call("alice", "POST", "/repositories", map[string]any{"name": "q", "statement": "新しい導線で、継続率が上がる"}); st != 201 {
		t.Fatalf("repository の作成：%d %s", st, b)
	}
	w.eventually("スナップショットツリーが出る", 60*time.Second, func() bool {
		st, _ := w.call("alice", "GET", "/repositories/q/tree", nil)
		return st == 200
	})
	// エージェントが、ストーリー（子の仮説 2 つ）を足し、片方に検証結果（artifact 付き）を出して、反映する
	var repo0 struct {
		Root string `json:"root_hypothesis_id"`
	}
	_, rb := w.call("alice", "GET", "/repositories/q", nil)
	_ = json.Unmarshal(rb, &repo0)
	agent := w.user("agent", "s-agent-1")
	agent.loginAndClone("q")
	reviewer := w.user("reviewer", "s-reviewer")
	if code, _ := reviewer.cli(w.tokens["reviewer"]+"\n", "auth", "login", "--with-token"); code != 0 {
		t.Fatal("reviewer の login")
	}
	agent.branch("b1")
	var sc scaffold
	agent.must(&sc, "story", "new", "--parent", repo0.Root, "--title", "招待の導線", "--summary", "招待で入った人は、成果物まで早く着く",
		"--hypothesis", "招待経由で、成果物までの時間が半分になる", "--hypothesis", "招待した人の継続率も下がらない")
	hA := sc.IDs["hypothesis_ids"].([]any)[0].(string)
	hB := sc.IDs["hypothesis_ids"].([]any)[1].(string) // 検証結果のない葉
	var sc2 scaffold
	agent.must(&sc2, "story", "new", "--parent", repo0.Root, "--title", "広告の導線", "--summary", "広告で入った人も、同じ速さで着く",
		"--hypothesis", "広告から来た人も、成果物までの時間が短い")
	inactiveStory := sc2.IDs["story_id"].(string)
	agent.commitPush("b1", "分解")
	if got, code, out := reviewer.review(agent.create("b1").ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("分解の approve：%d %s", code, out)
	}
	agent.branch("b2")
	agent.must(nil, "verdict", "new", "--hypothesis", hA, "--value", "true", "--report", agent.file("r1.md", "# 検証\n\n比較実験で、成果物までの時間が半分になった。\n"), "--artifact", agent.file("data.csv", "x,y\n1,2\n"))
	agent.must(nil, "story", "inactivate", inactiveStory, "--reason", "広告は出さない")
	agent.commitPush("b2", "検証")
	if got, code, out := reviewer.review(agent.create("b2").ID, "approve", 1); code != 0 || got.State != "merged" {
		t.Fatalf("検証の approve：%d %s", code, out)
	}
	w.eventually("検証結果が読む側に出る", 60*time.Second, func() bool {
		st, b := w.call("alice", "GET", "/repositories/q/nodes/"+hA, nil)
		return st == 200 && strings.Contains(string(b), `"verdict":{`)
	})

	// 重ねて見る apply request。main 向き（open のまま）：検証結果のない葉に、検証結果（偽）を出す
	agent.branch("b3")
	agent.must(nil, "verdict", "new", "--hypothesis", hB, "--value", "false", "--report", agent.file("r3.md", "# 検証\n\n継続率が下がった。\n"))
	agent.commitPush("b3", "検証 2")
	arMain := agent.create("b3")
	// 向き先が develop（積み上げ）：develop で背景情報を足し、その上の b4 でも足す
	agent.branch("develop")
	agent.must(nil, "node", "edit", hA, "--append-background", agent.file("bg-dev.md", "下の背景\n"))
	agent.commitPush("develop", "下")
	agent.git("checkout", "--quiet", "-B", "b4", "develop")
	agent.must(nil, "node", "edit", hA, "--append-background", agent.file("bg-b4.md", "上の背景\n"))
	agent.commitPush("b4", "上")
	arDev := agent.create("b4")
	w.retarget("q", arDev.ID, "develop")
	w.eventually("2 つの apply request を重ねて読める", 60*time.Second, func() bool {
		s1, _ := w.call("alice", "GET", "/repositories/q/tree/nodes?apply_request="+arMain.ID, nil)
		s2, b := w.call("alice", "GET", "/repositories/q/tree/nodes?apply_request="+arDev.ID, nil)
		return s1 == 200 && s2 == 200 && strings.Contains(string(b), "上の背景")
	})

	// root_statement を返す
	_, b := w.call("alice", "GET", "/repositories/q", nil)
	var repo struct {
		RootStatement *string `json:"root_statement"`
	}
	_ = json.Unmarshal(b, &repo)
	if repo.RootStatement == nil || *repo.RootStatement != "新しい導線で、継続率が上がる" {
		t.Fatalf("root_statement：%s", b)
	}
	// WebUI を配っている（CSP 付き）
	res, err := http.Get(w.api + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("WebUI の CSP がない：%v", res.Header)
	}

	// organization に入っていない Forgejo のユーザー（サインインはできるが、Logiforge の主体ではない）
	outsider := w.org + "outsider"
	w.forgejoCall(w.admin, "POST", "/admin/users", map[string]any{"username": outsider, "email": outsider + "@example.com", "password": "pw-" + outsider + "-12345", "must_change_password": false}, nil, 201)

	out := os.Getenv("LOGIFORGE_E2E_OUT")
	if out == "" {
		out = t.TempDir()
	}
	login := w.login("alice")
	cmd := exec.Command("node", "e2e/run.mjs")
	cmd.Dir = filepath.Join("..", "..", "webui")
	cmd.Env = append(os.Environ(), "BASE="+w.api, "LOGIN="+login, "PASSWORD=pw-"+login+"-12345", "CHROME="+chrome, "OUT="+out, "REPO=q",
		"OUTSIDER_LOGIN="+outsider, "NODE_A="+hA, "NODE_B="+hB, "AR_MAIN="+arMain.ID, "AR_DEV="+arDev.ID, "OUTSIDER_PASSWORD=pw-"+outsider+"-12345")
	b, err = cmd.CombinedOutput()
	t.Logf("run.mjs:\n%s", b)
	if err != nil {
		t.Fatalf("ブラウザの E2E が失敗した：%v（スクリーンショットは %s）", err, out)
	}
}
