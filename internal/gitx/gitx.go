// Package gitx は、git の CLI で、木の git リポジトリを読む（design/adapters/git.md）。
// 読むだけで、書く操作は持たない。worker と、CLI の check が使う。
//
// コミットを受ける関数は、コミットの SHA（40 か 64 文字の 16 進）だけを受け付ける。
// ブランチの名前は、LsRemote の結果で SHA に引いてから使う。オプションが引数に紛れ込むのを防ぐため。
package gitx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// Repo は、手元の clone である。
type Repo struct {
	// Dir は、clone のディレクトリ（bare でもよい）。
	Dir string
	// Env は、git に足す環境変数である。手元の git の設定は読まないので、
	// credential helper などは GIT_CONFIG_COUNT、GIT_CONFIG_KEY_n、GIT_CONFIG_VALUE_n で渡す。
	Env []string
}

// ErrNotFound は、コミットやブランチがないことを表す。
var ErrNotFound = errors.New("gitx: not found")

// ErrBadRevision は、コミットの SHA の形でない引数を表す。
var ErrBadRevision = errors.New("gitx: コミットの SHA でない")

var revPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

func checkRevs(revs ...string) error {
	for _, r := range revs {
		if !revPattern.MatchString(r) {
			return fmt.Errorf("%w: %q", ErrBadRevision, r)
		}
	}
	return nil
}

// isolatedEnv は、手元の git の設定から切り離した環境変数である。
var isolatedEnv = []string{
	"GIT_TERMINAL_PROMPT=0",
	"LC_ALL=C",
	"GIT_CONFIG_GLOBAL=/dev/null",
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_ALLOW_PROTOCOL=https:file",
	// パスを、glob ではなく、そのままのパスとして扱う。
	"GIT_LITERAL_PATHSPECS=1",
}

func (r *Repo) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = append(append(cmd.Environ(), isolatedEnv...), r.Env...)
	return cmd
}

func (r *Repo) run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := r.command(ctx, args...)
	cmd.Stdin = stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Init は、空の bare リポジトリを作る。すでにあれば何もしない。
func Init(ctx context.Context, dir string) (*Repo, error) {
	cmd := exec.CommandContext(ctx, "git", "init", "--bare", "--quiet", "--", dir)
	cmd.Env = append(cmd.Environ(), isolatedEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git init: %w: %s", err, out)
	}
	return &Repo{Dir: dir}, nil
}

// LsRemote は、リモートのブランチの先頭を、ブランチの名前から引く。
func (r *Repo) LsRemote(ctx context.Context, remote string) (map[string]string, error) {
	out, err := r.run(ctx, nil, "ls-remote", "--heads", "--", remote)
	if err != nil {
		return nil, err
	}
	heads := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		heads[strings.TrimPrefix(ref, "refs/heads/")] = sha
	}
	return heads, nil
}

// Fetch は、リモートのブランチを、手元の refs/remotes/origin/ に取ってくる。消えたブランチは消す。
//
// TODO(④ worker)：pull request の最後の版を読むため、refspec に refs/pull/*/head を足す。
func (r *Repo) Fetch(ctx context.Context, remote string) error {
	_, err := r.run(ctx, nil, "fetch", "--quiet", "--prune", "--no-tags", "--end-of-options", remote, "+refs/heads/*:refs/remotes/origin/*")
	return err
}

// Commit は、コミットの情報である。
type Commit struct {
	SHA         string
	Parents     []string
	CommittedAt time.Time
	Message     string
	Trailers    []Trailer
}

// Trailer は、コミットのメッセージの trailer である。
type Trailer struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ReadCommit は、コミットの情報を読む。
func (r *Repo) ReadCommit(ctx context.Context, rev string) (Commit, error) {
	if err := checkRevs(rev); err != nil {
		return Commit{}, err
	}
	out, err := r.run(ctx, nil, "show", "-s", "--format=%H%n%P%n%ct%n%B", "--end-of-options", rev, "--")
	if err != nil {
		if strings.Contains(err.Error(), "unknown revision") || strings.Contains(err.Error(), "bad object") {
			return Commit{}, ErrNotFound
		}
		return Commit{}, err
	}
	parts := strings.SplitN(string(out), "\n", 4)
	if len(parts) < 4 {
		return Commit{}, fmt.Errorf("git show: 形が正しくない")
	}
	ts, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Commit{}, err
	}
	c := Commit{SHA: parts[0], Parents: strings.Fields(parts[1]), CommittedAt: time.Unix(ts, 0).UTC(), Message: parts[3]}
	if c.Trailers, err = r.parseTrailers(ctx, c.Message); err != nil {
		return Commit{}, err
	}
	return c, nil
}

func (r *Repo) parseTrailers(ctx context.Context, msg string) ([]Trailer, error) {
	out, err := r.run(ctx, strings.NewReader(msg), "interpret-trailers", "--parse", "--no-divider")
	if err != nil {
		return nil, err
	}
	var ts []Trailer
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		ts = append(ts, Trailer{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
	}
	return ts, nil
}

// FirstParents は、rev から根までの first-parent の並びを、根から順に返す。
func (r *Repo) FirstParents(ctx context.Context, rev string) ([]string, error) {
	if err := checkRevs(rev); err != nil {
		return nil, err
	}
	out, err := r.run(ctx, nil, "rev-list", "--first-parent", "--reverse", "--end-of-options", rev, "--")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// MergeBase は、2 つのコミットの分かれた点を返す。
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	if err := checkRevs(a, b); err != nil {
		return "", err
	}
	out, err := r.run(ctx, nil, "merge-base", "--end-of-options", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ReadFiles は、コミットの木のファイルをすべて読む。
// 普通のファイルでないものと、maxSize を超えるものは、中身を読まない（design/backend/worker.md）。
func (r *Repo) ReadFiles(ctx context.Context, rev string, maxSize int64) (tree.Files, error) {
	return r.readFiles(ctx, rev, nil, maxSize)
}

// ReadPaths は、コミットの木の、指定したパスのファイルだけを読む。ないパスは返さない。
func (r *Repo) ReadPaths(ctx context.Context, rev string, paths []string, maxSize int64) (tree.Files, error) {
	if len(paths) == 0 {
		return tree.Files{}, nil
	}
	return r.readFiles(ctx, rev, paths, maxSize)
}

func (r *Repo) readFiles(ctx context.Context, rev string, paths []string, maxSize int64) (tree.Files, error) {
	if err := checkRevs(rev); err != nil {
		return nil, err
	}
	args := []string{"ls-tree", "-r", "-l", "-z", "--full-tree", "--end-of-options", rev}
	if paths != nil {
		args = append(append(args, "--"), paths...)
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	files := tree.Files{}
	var want []string
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		if !ok {
			return nil, fmt.Errorf("git ls-tree: 形が正しくない: %q", rec)
		}
		f := strings.Fields(meta) // mode type sha size
		if len(f) != 4 {
			return nil, fmt.Errorf("git ls-tree: 形が正しくない: %q", rec)
		}
		var size int64
		if f[3] != "-" {
			if size, err = strconv.ParseInt(f[3], 10, 64); err != nil {
				return nil, err
			}
		}
		files[p] = tree.File{Path: p, Mode: f[0], SHA: f[2], Size: size}
		if f[1] == "blob" && f[0] == tree.RegularMode && size <= maxSize {
			want = append(want, p)
		}
	}
	if err := r.catBlobs(ctx, files, want); err != nil {
		return nil, err
	}
	return files, nil
}

// catBlobs は、git cat-file --batch で、ファイルの中身を読む。出力を溜めず、1 つずつ切り分けて読む。
func (r *Repo) catBlobs(ctx context.Context, files tree.Files, paths []string) (err error) {
	if len(paths) == 0 {
		return nil
	}
	cmd := r.command(ctx, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		if werr := cmd.Wait(); err == nil && werr != nil {
			err = fmt.Errorf("git cat-file: %w: %s", werr, strings.TrimSpace(errb.String()))
		}
	}()
	shas := make([]string, len(paths)) // goroutine は files を読まない（下で書き換えるため）
	for i, p := range paths {
		shas[i] = files[p].SHA
	}
	go func() {
		w := bufio.NewWriter(stdin)
		for _, sha := range shas {
			if _, err := w.WriteString(sha + "\n"); err != nil {
				break
			}
		}
		_ = w.Flush()
		_ = stdin.Close()
	}()
	br := bufio.NewReader(stdout)
	defer func() { _, _ = io.Copy(io.Discard, br) }()
	for _, p := range paths {
		header, err := br.ReadString('\n')
		if err != nil {
			return fmt.Errorf("git cat-file: %w", err)
		}
		h := strings.Fields(header)
		if len(h) != 3 || h[1] != "blob" {
			return fmt.Errorf("git cat-file: 形が正しくない: %q", header)
		}
		n, err := strconv.Atoi(h[2])
		if err != nil {
			return err
		}
		buf := make([]byte, n+1) // 末尾の改行
		if _, err := io.ReadFull(br, buf); err != nil {
			return fmt.Errorf("git cat-file: %w", err)
		}
		f := files[p]
		f.Content = buf[:n]
		files[p] = f
	}
	return nil
}

// Change は、2 つのコミットの間で変わったファイルである。
type Change struct {
	// Status は A（加わった）、M（変わった）、D（消えた）、T（種類が変わった）。
	Status  string
	Path    string
	OldMode string
	NewMode string
	OldSHA  string
	NewSHA  string
}

// Diff は、a から b への、変わったファイルを返す。名前の変更は、消えたと加わったの組にする。
func (r *Repo) Diff(ctx context.Context, a, b string) ([]Change, error) {
	if err := checkRevs(a, b); err != nil {
		return nil, err
	}
	out, err := r.run(ctx, nil, "diff-tree", "-r", "-z", "--no-renames", "--no-commit-id", "--end-of-options", a, b)
	if err != nil {
		return nil, err
	}
	recs := strings.Split(string(out), "\x00")
	var cs []Change
	for i := 0; i+1 < len(recs); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(recs[i], ":"))
		if len(meta) != 5 {
			return nil, fmt.Errorf("git diff-tree: 形が正しくない: %q", recs[i])
		}
		cs = append(cs, Change{OldMode: meta[0], NewMode: meta[1], OldSHA: meta[2], NewSHA: meta[3], Status: meta[4], Path: recs[i+1]})
	}
	return cs, nil
}

// NextFiles は、parent の木のファイル（from のコミット）に、from から to への差分だけを読んで重ね、to の木のファイルを返す。
// 読むのは、加わったか変わったパスだけである。parent のファイルは、そのまま引き継ぐ。
// parent は、ひとつ前の main のスナップショットから Tree.Files で戻したものを渡す。
func (r *Repo) NextFiles(ctx context.Context, parent tree.Files, from, to string, maxSize int64) (tree.Files, error) {
	changes, err := r.Diff(ctx, from, to)
	if err != nil {
		return nil, err
	}
	next := tree.Files{}
	for p, f := range parent {
		next[p] = f
	}
	var read []string
	for _, c := range changes {
		delete(next, c.Path)
		if c.Status != "D" {
			read = append(read, c.Path)
		}
	}
	got, err := r.ReadPaths(ctx, to, read, maxSize)
	if err != nil {
		return nil, err
	}
	for p, f := range got {
		next[p] = f
	}
	return next, nil
}

// ChangedFiles は、base から head への差分と、head での、加わったか変わったファイルだけを返す。
// ブランチのスナップショットを、base の木の全体を読まずに作るために使う。
func (r *Repo) ChangedFiles(ctx context.Context, base, head string, maxSize int64) ([]Change, tree.Files, error) {
	changes, err := r.Diff(ctx, base, head)
	if err != nil {
		return nil, nil, err
	}
	var read []string
	for _, c := range changes {
		if c.Status != "D" {
			read = append(read, c.Path)
		}
	}
	files, err := r.ReadPaths(ctx, head, read, maxSize)
	if err != nil {
		return nil, nil, err
	}
	return changes, files, nil
}
