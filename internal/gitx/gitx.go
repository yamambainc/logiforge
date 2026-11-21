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

	"github.com/yamambainc/logiforge/internal/tree"
)

// Repo は、手元の clone である。
type Repo struct {
	// Dir は、clone のディレクトリ（bare でもよい）。
	Dir string
	// Env は、git に足す環境変数である。手元の git の設定は読まないので、
	// credential helper などは GIT_CONFIG_COUNT、GIT_CONFIG_KEY_n、GIT_CONFIG_VALUE_n で渡す。
	// 手元から切り離す環境変数より後ろに足すので、同じ名前なら Env が勝つ（例：テストで GIT_ALLOW_PROTOCOL に http を足す）。
	Env []string
	// Prefix は、ディレクトリの repository のパス（design/directory/README.md）。空でなければ、ファイルと差分は、
	// このディレクトリの中のものだけを、パスからディレクトリを除いて返す。外の変化は、DiffAll で別に返す。
	Prefix string
	// Timeout は、git の操作 1 回の時間の上限である。0 なら上限を付けない。
	// 超えたら、git を止めて、ErrTimeout を包んだ誤りを返す（design/backend/worker/patrol.md の「すること」）。
	Timeout time.Duration
}

// waitDelay は、git を止めたあと、出力のパイプが閉じるのを待つ時間の上限である。
const waitDelay = 5 * time.Second

// ErrTimeout は、git の操作が、時間の上限を超えたことを表す。
var ErrTimeout = errors.New("gitx: 時間の上限を超えた")

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

// command は、git を動かすコマンドを作る。Timeout があれば、その時間で止まる ctx を使う。終わったら、返した関数を呼ぶ。
func (r *Repo) command(ctx context.Context, args ...string) (*exec.Cmd, context.Context, context.CancelFunc) {
	cancel := context.CancelFunc(func() {})
	if r.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.Dir}, args...)...)
	cmd.Env = append(append(cmd.Environ(), isolatedEnv...), r.Env...)
	// 止めたあと、子のプロセスがパイプを持ったまま残っても、待ちを打ち切る。
	cmd.WaitDelay = waitDelay
	killGroup(cmd)
	return cmd, ctx, cancel
}

// failed は、git の誤りを包む。時間の上限を超えたときは、ErrTimeout も包む。
func failed(ctx context.Context, args []string, err error, stderr string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("git %s: %w: %w", strings.Join(args, " "), ErrTimeout, err)
	}
	return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr))
}

func (r *Repo) run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	cmd, cctx, cancel := r.command(ctx, args...)
	defer cancel()
	cmd.Stdin = stdin
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, failed(cctx, args, err, errb.String())
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

// IsRepo は、r.Dir が git の bare repository として読めるかを返す。
// 読めないと決まるのは、git が動いて「not a git repository」で終わったときだけである。
// git を動かせなかった、時間の上限を超えた、などのときは、error を返す。なぜ：正しい複製を、読めないと誤って消さないため。
func (r *Repo) IsRepo(ctx context.Context) (bool, error) {
	args := []string{"--git-dir=.", "rev-parse", "--is-bare-repository"}
	cmd, cctx, cancel := r.command(ctx, args...)
	defer cancel()
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err == nil {
		return strings.TrimSpace(out.String()) == "true", nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && cctx.Err() == nil && strings.Contains(errb.String(), "not a git repository") {
		return false, nil
	}
	return false, failed(cctx, args, err, errb.String())
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

// Fetch は、リモートのブランチを手元の refs/remotes/origin/ に、pull request の最後の先頭のコミットを refs/remotes/pull/ に取ってくる。
// 消えたブランチは消す。pull request の先頭のコミットは、ブランチが消えたあとも、反映の重なりを確かめるのに使う（design/backend/worker/patrol.md の「コミットの警告」）。
func (r *Repo) Fetch(ctx context.Context, remote string) error {
	_, err := r.run(ctx, nil, "fetch", "--quiet", "--prune", "--no-tags", "--end-of-options", remote,
		"+refs/heads/*:refs/remotes/origin/*", "+refs/pull/*/head:refs/remotes/pull/*")
	return err
}

// ErrNoRef は、fetch したいブランチが、リモートにないことを表す。
var ErrNoRef = errors.New("gitx: リモートにそのブランチがない")

// FetchBranches は、リモートの、名前を指定したブランチだけを fetch する。どれかがリモートになければ ErrNoRef。
// 名前は、refspec で意味を持つ文字（: * など）を含まないものを渡す。
func (r *Repo) FetchBranches(ctx context.Context, remote string, names ...string) error {
	args := []string{"fetch", "--quiet", "--no-tags", "--end-of-options", remote}
	for _, n := range names {
		args = append(args, "+refs/heads/"+n+":refs/remotes/origin/"+n)
	}
	_, err := r.run(ctx, nil, args...)
	if err != nil && strings.Contains(err.Error(), "couldn't find remote ref") {
		return fmt.Errorf("%w: %v", ErrNoRef, err)
	}
	return err
}

// LocalHeads は、前に取ってきたブランチの先頭を、ブランチの名前から引く。
func (r *Repo) LocalHeads(ctx context.Context) (map[string]string, error) {
	out, err := r.run(ctx, nil, "for-each-ref", "--format=%(objectname) %(refname)", "refs/remotes/origin/")
	if err != nil {
		return nil, err
	}
	heads := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		sha, ref, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		heads[strings.TrimPrefix(ref, "refs/remotes/origin/")] = sha
	}
	return heads, nil
}

// HasCommit は、手元にそのコミットがあるかを返す。
func (r *Repo) HasCommit(ctx context.Context, rev string) (bool, error) {
	if err := checkRevs(rev); err != nil {
		return false, err
	}
	_, err := r.run(ctx, nil, "cat-file", "-e", "--end-of-options", rev+"^{commit}")
	return err == nil, nil
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
// 普通のファイルでないものと、maxSize を超えるものは、中身を読まない（design/backend/worker/patrol.md の「すること」）。
func (r *Repo) ReadFiles(ctx context.Context, rev string, maxSize int64) (tree.Files, error) {
	return r.readFiles(ctx, rev, maxSize)
}

func (r *Repo) readFiles(ctx context.Context, rev string, maxSize int64) (tree.Files, error) {
	if err := checkRevs(rev); err != nil {
		return nil, err
	}
	out, err := r.run(ctx, nil, "ls-tree", "-r", "-l", "-z", "--full-tree", "--end-of-options", rev)
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
		p, inside := r.inside(p)
		if !inside {
			continue
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
	cmd, cctx, cancel := r.command(ctx, "cat-file", "--batch")
	defer cancel()
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
			err = failed(cctx, []string{"cat-file", "--batch"}, werr, errb.String())
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
// a が空なら、空の木から b への差分（b のすべてのファイルが加わった）を返す。最初のコミットに使う。
func (r *Repo) Diff(ctx context.Context, a, b string) ([]Change, error) {
	cs, _, err := r.DiffAll(ctx, a, b)
	return cs, err
}

// inside は、パスが Prefix のディレクトリの中かと、ディレクトリを除いたパスを返す。Prefix が空なら、どのパスも中である。
func (r *Repo) inside(p string) (string, bool) {
	if r.Prefix == "" {
		return p, true
	}
	rest, ok := strings.CutPrefix(p, r.Prefix+"/")
	return rest, ok && rest != ""
}

// DiffAll は、Diff と同じく差分を返し、Prefix のディレクトリの外で変わったパスも返す。Prefix が空なら、外はない。
func (r *Repo) DiffAll(ctx context.Context, a, b string) ([]Change, []string, error) {
	args := []string{"diff-tree", "-r", "-z", "--no-renames", "--no-commit-id"}
	if a == "" {
		if err := checkRevs(b); err != nil {
			return nil, nil, err
		}
		args = append(args, "--root", "--end-of-options", b)
	} else {
		if err := checkRevs(a, b); err != nil {
			return nil, nil, err
		}
		args = append(args, "--end-of-options", a, b)
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return nil, nil, err
	}
	recs := strings.Split(string(out), "\x00")
	var cs []Change
	var outside []string
	for i := 0; i+1 < len(recs); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(recs[i], ":"))
		if len(meta) != 5 {
			return nil, nil, fmt.Errorf("git diff-tree: 形が正しくない: %q", recs[i])
		}
		p, in := r.inside(recs[i+1])
		if !in {
			outside = append(outside, recs[i+1])
			continue
		}
		cs = append(cs, Change{OldMode: meta[0], NewMode: meta[1], OldSHA: meta[2], NewSHA: meta[3], Status: meta[4], Path: p})
	}
	return cs, outside, nil
}

// NextFiles は、parent の木のファイル（from のコミット）に、from から to への差分だけを読んで重ね、to の木のファイルを返す。
// 読むのは、加わったか変わったパスだけである。parent のファイルは、そのまま引き継ぐ。
// parent は、ひとつ前の main のスナップショットから Tree.Files で戻したものを渡す。
func (r *Repo) NextFiles(ctx context.Context, parent tree.Files, from, to string, maxSize int64) (tree.Files, error) {
	changes, err := r.Diff(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return r.ApplyChanges(ctx, parent, changes, maxSize)
}

// ApplyChanges は、parent の木のファイルに、Diff で求めた差分を重ねる。加わったか変わったパスだけを読む。
// 差分をほかにも使うときに、Diff を 2 度動かさずに済むようにするため。
func (r *Repo) ApplyChanges(ctx context.Context, parent tree.Files, changes []Change, maxSize int64) (tree.Files, error) {
	next := tree.Files{}
	for p, f := range parent {
		next[p] = f
	}
	for _, c := range changes {
		delete(next, c.Path)
	}
	got, err := r.changedFiles(ctx, changes, maxSize)
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
	files, err := r.changedFiles(ctx, changes, maxSize)
	if err != nil {
		return nil, nil, err
	}
	return changes, files, nil
}

// changedFiles は、差分の、加わったか変わったファイルを読む。モードと blob の SHA は差分から取り、
// 大きさは cat-file --batch-check で求める。パスをコマンドの引数に並べないので、数が多くても引数の長さの上限に当たらない。
func (r *Repo) changedFiles(ctx context.Context, changes []Change, maxSize int64) (tree.Files, error) {
	files := tree.Files{}
	var blobs []string
	for _, c := range changes {
		if c.Status == "D" {
			continue
		}
		files[c.Path] = tree.File{Path: c.Path, Mode: c.NewMode, SHA: c.NewSHA}
		if c.NewMode == tree.RegularMode {
			blobs = append(blobs, c.Path)
		}
	}
	if len(blobs) == 0 {
		return files, nil
	}
	var in bytes.Buffer
	for _, p := range blobs {
		in.WriteString(files[p].SHA + "\n")
	}
	out, err := r.run(ctx, &in, "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(blobs) {
		return nil, fmt.Errorf("git cat-file --batch-check: %d 行で、%d 行でない", len(lines), len(blobs))
	}
	var read []string
	for i, p := range blobs {
		h := strings.Fields(lines[i])
		if len(h) != 3 || h[1] != "blob" {
			return nil, fmt.Errorf("git cat-file --batch-check: 形が正しくない: %q", lines[i])
		}
		size, err := strconv.ParseInt(h[2], 10, 64)
		if err != nil {
			return nil, err
		}
		f := files[p]
		f.Size = size
		files[p] = f
		if size <= maxSize {
			read = append(read, p)
		}
	}
	if err := r.catBlobs(ctx, files, read); err != nil {
		return nil, err
	}
	return files, nil
}

// LastChange は、rev の first-parent の並びのうち、Prefix のディレクトリの中を変えた、いちばん新しいコミット（rev を含む）を返す。
// なければ空。ディレクトリの repository で、コミットを、その時点の版に引くのに使う（design/directory/history.md の「版」）。
func (r *Repo) LastChange(ctx context.Context, rev string) (string, error) {
	if err := checkRevs(rev); err != nil {
		return "", err
	}
	if r.Prefix == "" {
		return rev, nil
	}
	out, err := r.run(ctx, nil, "log", "--first-parent", "-1", "--format=%H", "--end-of-options", rev, "--", r.Prefix)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// IsAncestor は、a が b の祖先（a と b が同じでもよい）かを返す。
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	if err := checkRevs(a, b); err != nil {
		return false, err
	}
	_, err := r.run(ctx, nil, "merge-base", "--is-ancestor", "--end-of-options", a, b)
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}
