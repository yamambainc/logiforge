// Package worker は、git を読んでスナップショットを作り続ける worker である（design/backend/worker.md）。
// git にも GitForge にも書かない。置き場に書くのは、worker だけである。
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/gitx"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// Source は、worker が読む repository の一覧と、その git の URL である。GitForge ごとに違う。
type Source interface {
	ListRepositories(ctx context.Context) ([]ports.Repository, error)
	// GitForgeRef は、repository の名前から gitforge_ref を求める。GitForge に問い合わせない。
	GitForgeRef(ctx context.Context, name string) (string, error)
	// GitURL は、gitforge_ref から、git で読む URL を返す。
	GitURL(gitForgeRef string) string
}

// Config は、worker の設定である。
type Config struct {
	// Dir は、repository ごとの clone を置く、永続するディレクトリ。
	Dir string
	// GitEnv は、git に渡す環境変数（読むだけの資格情報の credential helper など）。
	GitEnv []string
	// Interval は、巡回の間隔。0 なら 1 分。
	Interval time.Duration
	// MaxFileSize は、中身を読むファイルの大きさの上限。0 なら artifact の上限（10 MiB）。
	MaxFileSize int64
	// ReadBranchWait は、「ブランチを読む」の中で、main の取り込みを待つ時間の上限。0 なら 60 秒。
	ReadBranchWait time.Duration
	Now            func() time.Time
	Log            *slog.Logger
}

// Worker は、worker である。repository ごとに、1 つずつ順に処理する。
type Worker struct {
	cfg   Config
	src   Source
	store store.Writer

	mu    sync.Mutex
	locks map[string]chan struct{} // repository ごとのロック（容量 1。入れたら取った、抜いたら放した）
	wake  chan string
	// runCtx は、Run の ctx である。裏の取り込みは、これで止まる。Run の前は context.Background()。
	runCtx context.Context
}

// New は、worker を作る。
func New(cfg Config, src Source, st store.Writer) *Worker {
	if cfg.Interval == 0 {
		cfg.Interval = time.Minute
	}
	if cfg.ReadBranchWait == 0 {
		cfg.ReadBranchWait = 60 * time.Second
	}
	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = tree.MaxArtifactSize
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Worker{cfg: cfg, src: src, store: st, locks: map[string]chan struct{}{}, wake: make(chan string, 64), runCtx: context.Background()}
}

func (w *Worker) repoLock(repo string) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	l, ok := w.locks[repo]
	if !ok {
		l = make(chan struct{}, 1)
		w.locks[repo] = l
	}
	return l
}

// lock は、repository のロックを取るまで待つ。戻り値は、放す関数である。
func (w *Worker) lock(repo string) func() {
	l := w.repoLock(repo)
	l <- struct{}{}
	return func() { <-l }
}

// tryLock は、repository のロックを、deadline までに取れれば、放す関数を返す。取れなければ nil を返す。
func (w *Worker) tryLock(repo string, deadline time.Time) func() {
	l := w.repoLock(repo)
	select {
	case l <- struct{}{}:
		return func() { <-l }
	default:
	}
	t := time.NewTimer(time.Until(deadline))
	defer t.Stop()
	select {
	case l <- struct{}{}:
		return func() { <-l }
	case <-t.C:
		return nil
	}
}

func (w *Worker) backgroundCtx() context.Context {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.runCtx
}

// Wake は、repository の巡回を、すぐに始めさせる。待たずに返る。
func (w *Worker) Wake(repo string) {
	select {
	case w.wake <- repo:
	default:
	}
}

// Run は、間隔ごとと、起こされたときに巡回する。ctx が終わるまで返らない。
func (w *Worker) Run(ctx context.Context) error {
	w.mu.Lock()
	w.runCtx = ctx
	w.mu.Unlock()
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	w.SyncAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			w.SyncAll(ctx)
		case name := <-w.wake:
			ref, err := w.src.GitForgeRef(ctx, name)
			if err != nil {
				w.cfg.Log.Error("gitforge_ref を求められない", "repository", name, "err", err)
				continue
			}
			if err := w.syncIfFree(ctx, ports.Repository{Name: name, GitForgeRef: ref}); err != nil {
				w.cfg.Log.Error("巡回に失敗した", "repository", name, "err", err)
			}
		}
	}
}

// SyncAll は、すべての repository を巡回し、repositories を書き直す。repository が違えば、並行して処理する。
func (w *Worker) SyncAll(ctx context.Context) {
	repos, err := w.src.ListRepositories(ctx)
	if err != nil {
		w.cfg.Log.Error("repository の一覧を読めない", "err", err)
		return
	}
	var wg sync.WaitGroup
	for _, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.syncIfFree(ctx, r); err != nil {
				w.cfg.Log.Error("巡回に失敗した", "repository", r.Name, "err", err)
			}
		}()
	}
	wg.Wait()
	var names []string
	for _, r := range repos {
		if latest, err := store.GetLatest(ctx, w.store, r.Name); err == nil && latest != "" {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	if err := store.PutRepositories(ctx, w.store, names); err != nil {
		w.cfg.Log.Error("repositories を書けない", "err", err)
	}
}

func (w *Worker) repo(ctx context.Context, name string) (*gitx.Repo, error) {
	dir := filepath.Join(w.cfg.Dir, name+".git")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if _, err := gitx.Init(ctx, dir); err != nil {
			return nil, err
		}
	}
	return &gitx.Repo{Dir: dir, Env: w.cfg.GitEnv}, nil
}

// Sync は、1 つの repository を巡回する（design/backend/worker.md の「すること」）。何度動かしても、結果は同じになる。
// repository のロックを取るまで待つ。
func (w *Worker) Sync(ctx context.Context, r ports.Repository) error {
	defer w.lock(r.Name)()
	return w.syncLocked(ctx, r)
}

// syncIfFree は、repository のロックがすぐに取れるときだけ巡回する。取れなければ（ブランチを読むときの裏の取り込みなどが
// 動いている）、その回は飛ばす。次の巡回で追いつく。1 つの repository の長い取り込みが、ほかの repository の巡回を遅らせないため。
func (w *Worker) syncIfFree(ctx context.Context, r ports.Repository) error {
	unlock := w.tryLock(r.Name, time.Now())
	if unlock == nil {
		return nil
	}
	defer unlock()
	return w.syncLocked(ctx, r)
}

func (w *Worker) syncLocked(ctx context.Context, r ports.Repository) error {
	g, err := w.repo(ctx, r.Name)
	if err != nil {
		return err
	}
	remote := w.src.GitURL(r.GitForgeRef)
	heads, err := g.LsRemote(ctx, remote)
	if err != nil {
		return err
	}
	// 前に取ってきた先頭と違うときだけ、fetch する。
	old, err := g.LocalHeads(ctx)
	if err != nil {
		return err
	}
	if !maps(heads, old) {
		if err := g.Fetch(ctx, remote); err != nil {
			return err
		}
	}
	// 消えたブランチは、置き場のブランチの一覧と比べて見つける。手元の ref と比べると、fetch のあとに止まったとき、記録が失われるから。
	// main より先に行う。main で誤りが出ても、巻き込まれないようにするため。
	if err := w.markDeleted(ctx, r.Name, heads); err != nil {
		return err
	}
	if mainSHA, ok := heads["main"]; ok {
		if err := w.syncMain(ctx, g, r.Name, mainSHA); err != nil {
			return fmt.Errorf("main: %w", err)
		}
	}
	// ブランチ。
	names := make([]string, 0, len(heads))
	for name := range heads {
		if name != "main" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := w.syncBranch(ctx, g, r.Name, name, heads[name], heads["main"]); err != nil {
			w.cfg.Log.Error("ブランチを読めない", "repository", r.Name, "branch", name, "err", err)
		}
	}
	return nil
}

func maps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// syncMain は、main の first-parent の並びのうち、スナップショットのないコミットについて、スナップショットを作り、latest を書く。
func (w *Worker) syncMain(ctx context.Context, g *gitx.Repo, repo, mainSHA string) error {
	chain, err := g.FirstParents(ctx, mainSHA)
	if err != nil {
		return err
	}
	// スナップショットのある、いちばん新しいコミットを探す。
	start := 0
	var parent *snapshot.Main
	for i := len(chain) - 1; i >= 0; i-- {
		m, err := store.GetMain(ctx, w.store, repo, chain[i])
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		start, parent = i+1, m
		break
	}
	latest, err := store.GetLatest(ctx, w.store, repo)
	if err != nil {
		return err
	}
	rewritten := latest != "" && !slices.Contains(chain, latest)
	for i := start; i < len(chain); i++ {
		m, err := w.buildMain(ctx, g, repo, chain[i], parent)
		if err != nil {
			return fmt.Errorf("%s: %w", chain[i], err)
		}
		if rewritten && i == len(chain)-1 {
			m.AddWarnings(tree.Violation{Rule: RuleMainRewritten, Node: chain[i], Detail: "latest " + latest + " が、main の並びにない。main が書き換えられた"})
		}
		if err := store.PutMain(ctx, w.store, repo, m); err != nil {
			return err
		}
		parent = m
	}
	// latest の規則（design/backend/snapshot.md の「latest」）：今の latest が、新しい main の並びにあるときだけ書く。
	// ないときは、main が書き換えられているので、上書きする（警告は、上でコミットに付けた）。
	if latest == mainSHA {
		return nil
	}
	err = store.SetLatest(ctx, w.store, repo, mainSHA, latest)
	if errors.Is(err, store.ErrPrecondition) {
		return fmt.Errorf("latest が、読んだあとに書き換えられた（worker が 2 つ動いている？）")
	}
	return err
}

// RuleMainRewritten は、latest が新しい main の並びになく、main が書き換えられたことの警告である。
const RuleMainRewritten tree.Rule = "main_rewritten"

func (w *Worker) buildMain(ctx context.Context, g *gitx.Repo, repo, sha string, parent *snapshot.Main) (*snapshot.Main, error) {
	c, err := g.ReadCommit(ctx, sha)
	if err != nil {
		return nil, err
	}
	var files tree.Files
	if parent == nil {
		files, err = g.ReadFiles(ctx, sha, w.cfg.MaxFileSize)
	} else {
		var pf tree.Files
		if pf, err = parent.Tree.Files(); err == nil {
			files, err = g.NextFiles(ctx, pf, parent.Commit.Revision, sha, w.cfg.MaxFileSize)
		}
	}
	if err != nil {
		return nil, err
	}
	if err := w.putArtifacts(ctx, repo, files); err != nil {
		return nil, err
	}
	m, err := snapshot.BuildMain(files, commitInfo(c), parent)
	if err != nil {
		return nil, err
	}
	if parent != nil {
		m.AddWarnings(w.overlap(ctx, g, m, parent)...)
	}
	return m, nil
}

// overlap は、反映のコミットについて、対象ノードが、その base のあとに別の反映で変わっていないかを確かめる。
// base は、反映した版のコミット（Logiforge-Version:）と、反映の直前の main の分かれた点である。
func (w *Worker) overlap(ctx context.Context, g *gitx.Repo, m, parent *snapshot.Main) []tree.Violation {
	if snapshot.HasTrailer(m.Commit, snapshot.TrailerApplyRequest) == "" {
		return nil
	}
	unchecked := func(why string) []tree.Violation {
		return []tree.Violation{{Rule: snapshot.RuleOverlapUnchecked, Node: m.Commit.Revision, Detail: why}}
	}
	version := snapshot.VersionCommit(m.Commit)
	if version == "" {
		return unchecked("Logiforge-Version: がない")
	}
	if ok, err := g.HasCommit(ctx, version); err != nil || !ok {
		return unchecked("版のコミット " + version + " が手元にない")
	}
	base, err := g.MergeBase(ctx, version, parent.Commit.Revision)
	if err != nil {
		return unchecked("base を求められない：" + err.Error())
	}
	n := m.Tree.HistoryNumber(base)
	if n == 0 {
		return unchecked("base " + base + " が main の並びにない")
	}
	return m.OverlapWarnings(n)
}

// putArtifacts は、中身を読んだ artifact を、sha256 の名前で置き場に置く。すでにあれば置かない。
func (w *Worker) putArtifacts(ctx context.Context, repo string, files tree.Files) error {
	for p, f := range files {
		if f.Content == nil || !isArtifact(p) {
			continue
		}
		sum := tree.SHA256Hex(f.Content)
		if _, err := w.store.Get(ctx, store.ArtifactKey(repo, sum)); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := w.store.Put(ctx, store.ArtifactKey(repo, sum), f.Content); err != nil {
			return err
		}
	}
	return nil
}

func isArtifact(p string) bool {
	return len(p) > len(tree.ArtifactsDir)+1 && p[:len(tree.ArtifactsDir)+1] == tree.ArtifactsDir+"/"
}

func commitInfo(c gitx.Commit) snapshot.CommitInfo {
	info := snapshot.CommitInfo{Revision: c.SHA, CommittedAt: c.CommittedAt}
	for _, t := range c.Trailers {
		info.Trailers = append(info.Trailers, snapshot.Trailer{Key: t.Key, Value: t.Value})
	}
	return info
}

// BranchResult は、「ブランチを読む」の結果である。
type BranchResult struct {
	Head string `json:"head"`
	Base string `json:"base"`
	// Key は、先頭のコミットのスナップショットのコミットの記録のキー。
	Key string `json:"key"`
}

// syncBranch は、ブランチの先頭のスナップショットを作り、ブランチの履歴に足す。先頭が変わっていなければ、何もしない。
func (w *Worker) syncBranch(ctx context.Context, g *gitx.Repo, repo, name, head, mainSHA string) (BranchResult, error) {
	h, raw, err := store.GetBranchHistory(ctx, w.store, repo, name)
	if err != nil {
		return BranchResult{}, err
	}
	if n := len(h.Entries); n > 0 && !h.Entries[n-1].Deleted && h.Entries[n-1].Head == head {
		last := h.Entries[n-1]
		return BranchResult{Head: head, Base: last.Base, Key: store.CommitKey(repo, head)}, nil
	}
	if mainSHA == "" {
		return BranchResult{}, fmt.Errorf("main がないので、base を求められない")
	}
	base, err := g.MergeBase(ctx, mainSHA, head)
	if err != nil {
		return BranchResult{}, err
	}
	if _, err := store.GetCommit(ctx, w.store, repo, head); errors.Is(err, store.ErrNotFound) {
		if err := w.buildBranch(ctx, g, repo, base, head); err != nil {
			return BranchResult{}, err
		}
	} else if err != nil {
		return BranchResult{}, err
	}
	// ブランチの名前の一覧に、先に足す。履歴を書いたあとに止まっても、消えたことを見つけられるようにするため。
	if err := w.addToIndex(ctx, repo, name); err != nil {
		return BranchResult{}, err
	}
	// 一覧に足すのは、スナップショットを作り終えたコミットだけである。
	h.Entries = append(h.Entries, store.BranchEntry{Head: head, Base: base, SeenAt: w.cfg.Now().UTC()})
	if err := store.PutBranchHistory(ctx, w.store, repo, h, raw); err != nil {
		return BranchResult{}, err
	}
	return BranchResult{Head: head, Base: base, Key: store.CommitKey(repo, head)}, nil
}

func (w *Worker) buildBranch(ctx context.Context, g *gitx.Repo, repo, base, head string) error {
	c, err := g.ReadCommit(ctx, head)
	if err != nil {
		return err
	}
	changes, files, err := g.ChangedFiles(ctx, base, head, w.cfg.MaxFileSize)
	if err != nil {
		return err
	}
	if err := w.putArtifacts(ctx, repo, files); err != nil {
		return err
	}
	var deltas []snapshot.Delta
	for _, ch := range changes {
		d := snapshot.Delta{Path: ch.Path, Status: snapshot.Modified, BaseSHA: ch.OldSHA}
		switch ch.Status {
		case "A":
			d.Status, d.BaseSHA = snapshot.Added, ""
		case "D":
			d.Status = snapshot.Deleted
		}
		deltas = append(deltas, d)
	}
	b, err := snapshot.BuildBranch(base, deltas, files, commitInfo(c))
	if err != nil {
		return err
	}
	return store.PutBranch(ctx, w.store, repo, b)
}

func (w *Worker) addToIndex(ctx context.Context, repo, name string) error {
	names, raw, err := store.GetBranchIndex(ctx, w.store, repo)
	if err != nil {
		return err
	}
	if slices.Contains(names, name) {
		return nil
	}
	names = append(names, name)
	sort.Strings(names)
	return store.PutBranchIndex(ctx, w.store, repo, names, raw)
}

// markDeleted は、置き場のブランチの一覧のうち、履歴の最後が「消えた」でなく、今のリモートにないものに、「消えた」を足す。
func (w *Worker) markDeleted(ctx context.Context, repo string, heads map[string]string) error {
	names, _, err := store.GetBranchIndex(ctx, w.store, repo)
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, ok := heads[name]; ok {
			continue
		}
		h, raw, err := store.GetBranchHistory(ctx, w.store, repo, name)
		if err != nil {
			return err
		}
		if n := len(h.Entries); n == 0 || h.Entries[n-1].Deleted {
			continue
		}
		h.Entries = append(h.Entries, store.BranchEntry{SeenAt: w.cfg.Now().UTC(), Deleted: true})
		if err := store.PutBranchHistory(ctx, w.store, repo, h, raw); err != nil {
			return err
		}
	}
	return nil
}

// ErrSnapshotPending は、「ブランチを読む」の中で、main の取り込みが時間の上限までに終わらなかったことを表す。
// 取り込みは、裏で続ける。少し待って、もう一度頼めばよい。
var ErrSnapshotPending = errors.New("worker: main の取り込みが、まだ終わらない")

// ReadBranch は、ブランチを fetch し、先頭のコミットと base を求め、差分のスナップショットを作る（worker の内部 API の「ブランチを読む」）。
// base のスナップショットのために、先に main を進める。その取り込みが上限（Config.ReadBranchWait）までに終わらなければ、
// 取り込みを裏で続けたまま、ErrSnapshotPending を返す。
func (w *Worker) ReadBranch(ctx context.Context, repoName, branch string) (BranchResult, error) {
	ref, err := w.src.GitForgeRef(ctx, repoName)
	if err != nil {
		return BranchResult{}, err
	}
	r := &ports.Repository{Name: repoName, GitForgeRef: ref}
	// ロックを取るのを待つ時間も、main の取り込みを待つ時間も、1 つの上限の中に収める。
	deadline := time.Now().Add(w.cfg.ReadBranchWait)
	unlock := w.tryLock(r.Name, deadline)
	if unlock == nil {
		// ほかの取り込み（前の「ブランチを読む」の裏の取り込みなど）が、まだ動いている。
		return BranchResult{}, ErrSnapshotPending
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	g, err := w.repo(ctx, r.Name)
	if err != nil {
		return BranchResult{}, err
	}
	remote := w.src.GitURL(r.GitForgeRef)
	heads, err := g.LsRemote(ctx, remote)
	if err != nil {
		return BranchResult{}, err
	}
	head, ok := heads[branch]
	if !ok || branch == "main" {
		return BranchResult{}, ports.ErrNotFound
	}
	if err := g.Fetch(ctx, remote); err != nil {
		return BranchResult{}, err
	}
	// ブランチの base（main と分かれた点）のスナップショットがないと、バックエンドは差分を検査できない。先に main を進める。
	if mainSHA, ok := heads["main"]; ok {
		done := make(chan error, 1)
		// 要求が切れても、取り込みは続ける。Run の ctx が終われば止まる。
		go func() { done <- w.syncMain(w.backgroundCtx(), g, r.Name, mainSHA) }()
		select {
		case err := <-done:
			if err != nil {
				return BranchResult{}, fmt.Errorf("main: %w", err)
			}
		case <-time.After(time.Until(deadline)):
			// 取り込みが終わるまで、repository のロックを持ち続ける。終わったら放す。
			locked = false
			go func() {
				if err := <-done; err != nil {
					w.cfg.Log.Error("main の取り込みに失敗した", "repository", r.Name, "err", err)
				}
				unlock()
			}()
			return BranchResult{}, ErrSnapshotPending
		}
	}
	return w.syncBranch(ctx, g, r.Name, branch, head, heads["main"])
}
