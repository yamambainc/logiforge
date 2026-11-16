package gitx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
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
	mb, err := clone.MergeBase(ctx, heads["main"], heads["topic"])
	if err != nil || mb != base {
		t.Fatalf("merge-base: %v %v", mb, err)
	}
}

func TestRejectsNonSHA(t *testing.T) {
	ctx := context.Background()
	w := newWork(t)
	w.write("a", "1")
	w.commit("1")
	r := &Repo{Dir: w.dir}
	for _, rev := range []string{"main", "--output=/tmp/x", "HEAD~1", strings.Repeat("g", 40)} {
		if _, err := r.ReadFiles(ctx, rev, 10); !errors.Is(err, ErrBadRevision) {
			t.Fatalf("%q: %v", rev, err)
		}
		if _, err := r.Diff(ctx, rev, rev); !errors.Is(err, ErrBadRevision) {
			t.Fatalf("%q: %v", rev, err)
		}
	}
}

// main のスナップショットから戻したファイルに、変わったパスだけを読んで重ねると、全体を読んだのと同じになる。
func TestNextFilesFromSnapshot(t *testing.T) {
	ctx := context.Background()
	w := newWork(t)
	w.write("tree/h-0000000000/hypothesis.md", "---\nid: h-0000000000\nstatement: 根\n---\n")
	w.write("tree/h-0000000000/background.md", "一\n")
	art := "data"
	sha := tree.SHA256Hex([]byte(art))
	w.write(tree.ArtifactPath(sha), art)
	c1 := w.commit("1")
	w.write("tree/h-0000000000/background.md", "一\n二\n")
	art2 := "data2"
	sha2 := tree.SHA256Hex([]byte(art2))
	w.write(tree.ArtifactPath(sha2), art2)
	if err := os.Remove(filepath.Join(w.dir, tree.ArtifactPath(sha))); err != nil {
		t.Fatal(err)
	}
	c2 := w.commit("2")

	r := &Repo{Dir: w.dir}
	f1, err := r.ReadFiles(ctx, c1, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	m1, err := snapshot.BuildMain(f1, snapshot.CommitInfo{Revision: c1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := m1.SnapshotTree.Files()
	if err != nil {
		t.Fatal(err)
	}
	next, err := r.NextFiles(ctx, parent, c1, c2, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	full, err := r.ReadFiles(ctx, c2, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.ChangedPaths(full, next); len(got) != 0 {
		t.Fatalf("違うパス: %v", got)
	}
	// 加わった artifact だけ中身を読む。
	if f := next[tree.ArtifactPath(sha2)]; f.Content == nil {
		t.Fatal("加わった artifact を読んでいない")
	}
	m2, err := snapshot.BuildMain(next, snapshot.CommitInfo{Revision: c2}, m1)
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.SnapshotTree.Artifacts) != 1 || m2.SnapshotTree.Artifacts[0].SHA256 != sha2 {
		t.Fatalf("artifacts: %+v", m2.SnapshotTree.Artifacts)
	}

	changes, changed, err := r.ChangedFiles(ctx, c1, c2, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 || len(changed) != 2 {
		t.Fatalf("changes: %+v files: %d", changes, len(changed))
	}
}

// 変わったパスが多くても読める（パスをコマンドの引数に並べない）。大きいものとシンボリックリンクは中身を読まない。
func TestChangedFilesManyPaths(t *testing.T) {
	ctx := context.Background()
	w := newWork(t)
	w.write("a", "1")
	base := w.commit("1")
	for i := 0; i < 3000; i++ {
		w.write(fmt.Sprintf("tree/d%04d/a-long-file-name-to-make-the-argument-list-long.md", i), "x")
	}
	w.write("big", strings.Repeat("y", 100))
	if err := os.Symlink("a", filepath.Join(w.dir, "link")); err != nil {
		t.Fatal(err)
	}
	head := w.commit("2")
	r := &Repo{Dir: w.dir}
	changes, files, err := r.ChangedFiles(ctx, base, head, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3002 || len(files) != 3002 {
		t.Fatalf("changes %d files %d", len(changes), len(files))
	}
	if f := files["big"]; f.Content != nil || f.Size != 100 {
		t.Fatalf("big: %+v", f)
	}
	if f := files["link"]; f.Content != nil || f.Mode != "120000" {
		t.Fatalf("link: %+v", f)
	}
	if f := files["tree/d0001/a-long-file-name-to-make-the-argument-list-long.md"]; string(f.Content) != "x" || f.Size != 1 {
		t.Fatalf("file: %+v", f)
	}
}

// git の操作が時間の上限を超えたら、ErrTimeout を包んだ誤りを返す。
func TestTimeout(t *testing.T) {
	r := &Repo{Dir: t.TempDir(), Timeout: time.Nanosecond}
	if _, err := r.LsRemote(context.Background(), t.TempDir()); !errors.Is(err, ErrTimeout) {
		t.Fatalf("got %v", err)
	}
}
