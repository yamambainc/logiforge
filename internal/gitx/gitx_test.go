package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// work は、テスト用の作業ツリーつきのリポジトリである。
type work struct {
	t   *testing.T
	dir string
}

func newWork(t *testing.T) *work {
	t.Helper()
	w := &work{t: t, dir: t.TempDir()}
	w.git("init", "--quiet", "--initial-branch=main")
	return w
}

func (w *work) git(args ...string) string {
	w.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", w.dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_AUTHOR_DATE=2026-11-13T00:00:00Z", "GIT_COMMITTER_DATE=2026-11-13T00:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (w *work) write(p, content string) {
	w.t.Helper()
	full := filepath.Join(w.dir, p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *work) commit(msg string) string {
	w.t.Helper()
	w.git("add", "-A")
	w.git("commit", "--quiet", "-m", msg)
	return w.git("rev-parse", "HEAD")
}

func TestReadAndDiff(t *testing.T) {
	ctx := context.Background()
	w := newWork(t)
	w.write("tree/h-0000000000/hypothesis.md", "a\n")
	w.write("big.bin", strings.Repeat("x", 100))
	first := w.commit("最初")
	w.write("tree/h-0000000000/background.md", "背景\n")
	if err := os.Symlink("hypothesis.md", filepath.Join(w.dir, "tree/h-0000000000/link")); err != nil {
		t.Fatal(err)
	}
	second := w.commit("二つ目\n\nLogiforge-Apply-Request: apr_1\nLogiforge-Session-Id: s1\n")

	r := &Repo{Dir: w.dir}
	files, err := r.ReadFiles(ctx, second, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(files["tree/h-0000000000/background.md"].Content); got != "背景\n" {
		t.Fatalf("background: %q", got)
	}
	if f := files["big.bin"]; f.Content != nil || f.Size != 100 {
		t.Fatalf("大きいファイルを読んだ: %+v", f)
	}
	if f := files["tree/h-0000000000/link"]; f.Mode != "120000" || f.Content != nil {
		t.Fatalf("シンボリックリンク: %+v", f)
	}
	if f := files["tree/h-0000000000/hypothesis.md"]; f.SHA == "" {
		t.Fatal("blob の SHA がない")
	}

	c, err := r.ReadCommit(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Parents) != 1 || c.Parents[0] != first || len(c.Trailers) != 2 || c.Trailers[0] != (Trailer{"Logiforge-Apply-Request", "apr_1"}) {
		t.Fatalf("commit: %+v", c)
	}

	cs, err := r.Diff(ctx, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Status != "A" || cs[0].Path != "tree/h-0000000000/background.md" {
		t.Fatalf("diff: %+v", cs)
	}

	fp, err := r.FirstParents(ctx, second)
	if err != nil || len(fp) != 2 || fp[0] != first {
		t.Fatalf("first parents: %v %v", fp, err)
	}

	only, err := r.ReadPaths(ctx, second, []string{"tree/h-0000000000/background.md", "nothing"}, 50)
	if err != nil || len(only) != 1 {
		t.Fatalf("read paths: %v %v", only, err)
	}

	if _, err := r.ReadCommit(ctx, strings.Repeat("0", 40)); err != ErrNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestFetchAndMergeBase(t *testing.T) {
	ctx := context.Background()
	w := newWork(t)
	w.write("a", "1")
	base := w.commit("1")
	w.git("checkout", "--quiet", "-b", "topic")
	w.write("b", "2")
	topic := w.commit("2")
	w.git("checkout", "--quiet", "main")
	w.write("c", "3")
	w.commit("3")

	clone, err := Init(ctx, filepath.Join(t.TempDir(), "clone.git"))
	if err != nil {
		t.Fatal(err)
	}
	heads, err := clone.LsRemote(ctx, w.dir)
	if err != nil || heads["topic"] != topic || len(heads) != 2 {
		t.Fatalf("ls-remote: %v %v", heads, err)
	}
	if err := clone.Fetch(ctx, w.dir); err != nil {
		t.Fatal(err)
	}
	mb, err := clone.MergeBase(ctx, "refs/remotes/origin/main", "refs/remotes/origin/topic")
	if err != nil || mb != base {
		t.Fatalf("merge-base: %v %v", mb, err)
	}
}
