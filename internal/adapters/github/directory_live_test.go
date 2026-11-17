package github

// 実際の GitHub での、ディレクトリ方式の結合テスト（design/directory/）。環境変数がなければ飛ばす。
//
//	GITHUB_TEST_DIR_REPO    プロジェクトのリポジトリ（owner/name）。main があり、コードのあるもの
//	GITHUB_TEST_TOKEN       利用者、opener、reviewer、人として使うトークン（repository の Write）

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

func TestGitHubLiveDirectory(t *testing.T) {
	project := os.Getenv("GITHUB_TEST_DIR_REPO")
	if project == "" || os.Getenv("GITHUB_TEST_TOKEN") == "" {
		t.Skip("GITHUB_TEST_DIR_REPO と GITHUB_TEST_TOKEN がない（実際の GitHub での結合テスト）")
	}
	t.Setenv("GITHUB_TEST_REPO", project+":logiforge/cache")
	l := liveEnv(t)
	l.cfg.Repositories = []ports.Repository{{Name: "q", GitForgeRef: project + ":logiforge/cache"}}
	var err error
	if l.a, l.auth, err = New(l.cfg, l.keys); err != nil {
		t.Fatal(err)
	}
	// 用意：ディレクトリがなければ、利用者のトークンで、用意のコミットを main に置く（init-directory の署名）。
	const id = "h-0000000000"
	files := []ports.File{{Path: "tree/" + id + "/hypothesis.md", Content: tree.MarshalHypothesis(tree.HypothesisContent{ID: id, Statement: "ディレクトリでも中身で決まるか"})}}
	switch err := l.a.SetupRepository(l.asUser, l.repo, files, "repository を用意した\n\nLogiforge-Init: 1"); {
	case err == nil:
		t.Log("確かめた：利用者のトークンで、プロジェクトのリポジトリの main に、ディレクトリの用意のコミットを置ける")
	case errors.Is(err, ports.ErrMainExists):
		t.Log("ディレクトリは用意済み")
	default:
		t.Fatalf("用意：%v", err)
	}
	dir := t.TempDir()
	l.must(dir, "clone", "--quiet", "https://github.com/"+project+".git", ".")
	stamp := fmt.Sprint(time.Now().Unix())

	// 1. apply request の反映（reviewer のトークンで merge）。ブランチは、ディレクトリの中だけを変える。
	branch := func(name string, change func()) string {
		l.must(dir, "fetch", "--quiet", "origin")
		l.must(dir, "switch", "--quiet", "-C", name, "origin/main")
		change()
		l.must(dir, "add", "-A")
		l.must(dir, "commit", "--quiet", "-m", name, "--trailer", "Logiforge-Session-Id: s-open")
		l.must(dir, "push", "--quiet", "--force", "origin", name)
		return l.must(dir, "rev-parse", "HEAD")
	}
	appendBg := func(line string) func() {
		return func() {
			p := filepath.Join(dir, "logiforge/cache/tree", id, "background.md")
			old, _ := os.ReadFile(p)
			if err := os.WriteFile(p, append(old, []byte(line+"\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	h := branch("lf-dir-ar-"+stamp, appendBg("反映 "+stamp))
	ar, err := l.a.CreateApplyRequest(l.asUser, l.repo, ports.NewApplyRequest{ID: "apr_dir" + stamp, Branch: "lf-dir-ar-" + stamp, FirstRevision: h, Opener: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	l.review(ar, ports.ReviewApprove, "s-rev")
	shaAR, err := l.a.Merge(l.asUser, l.repo, ar.ID, ar.HeadRevision, "反映 "+ar.ID, "Logiforge-Apply-Request: "+ar.ID)
	if err != nil {
		t.Fatalf("reviewer のトークンで merge できない：%v", err)
	}
	// 2. コードの PR（ディレクトリの外だけ）を、人が merge する。
	branch("lf-dir-code-"+stamp, func() {
		if err := os.WriteFile(filepath.Join(dir, "code-"+stamp+".txt"), []byte("code\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	code := pullreqOf(t, l, project, "lf-dir-code-"+stamp)
	shaCode := l.humanMerge(code, "merge")
	// 3. ディレクトリを変える、apply request でない PR を、人が squash merge する。
	branch("lf-dir-human-"+stamp, appendBg("人の変更 "+stamp))
	human := pullreqOf(t, l, project, "lf-dir-human-"+stamp)
	shaHuman := l.humanMerge(human, "squash")

	l.must(dir, "fetch", "--quiet", "origin")
	w, st, v := l.worker()
	l.sync(w)
	if got := l.verdict(v, st, dir, shaAR); !got.Authorized || got.ApplyRequest != ar.ID {
		t.Errorf("apply request の反映：%+v", got)
	}
	if _, err := store.GetCommit(context.Background(), st, "q", shaCode); err == nil {
		t.Error("コードの merge が、版になった")
	}
	if got := l.verdict(v, st, dir, shaHuman); got.Authorized {
		t.Errorf("apply request でない PR の merge が、反映になった：%+v", got)
	}
	c, _ := store.GetCommit(context.Background(), st, "q", shaHuman)
	if !hasWarning(c, snapshot.RuleNoApplyRequest) {
		t.Errorf("apply request でない PR の merge に、警告がない：%+v", c.Warnings)
	}
	t.Log("確かめた：ディレクトリの用意は init-directory の署名で、反映は中身で決まる。コードの merge は版にならず、apply request でない PR の merge は反映でない")
	for _, b := range []string{"lf-dir-ar-" + stamp, "lf-dir-code-" + stamp, "lf-dir-human-" + stamp} {
		_, _ = l.git(dir, "push", "--quiet", "origin", "--delete", b)
	}
}

// pullreqOf は、ブランチから pull request を作る（apply request の記録は書かない）。
func pullreqOf(t *testing.T, l *live, project, branch string) ports.ApplyRequest {
	t.Helper()
	p, err := api{&forge{cfg: l.cfg, c: newClient("", nil)}}.CreatePull(l.asUser, project, branch, branch, "")
	if err != nil {
		t.Fatal(err)
	}
	return ports.ApplyRequest{Number: p.Number, HeadRevision: p.HeadSHA, Branch: branch}
}
