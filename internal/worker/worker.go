// Package worker は、git を読んでスナップショットを作り続ける worker である（design/backend/worker.md）。
// git にも GitForge にも書かない。置き場に書くのは、worker だけである。
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/gitx"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
	"github.com/yamambainc/hypothesis-verification-harness/internal/workcopy"
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
	// BuildTreeWait は、「木を作る」で、作り終えるのを待つ時間の上限。0 なら 60 秒。過ぎたら、裏で続けたまま返す。
	BuildTreeWait time.Duration
	Now           func() time.Time
	Log           *slog.Logger
}

// Worker は、worker である。repository ごとに、1 つずつ順に処理する。
type Worker struct {
	cfg   Config
	src   Source
	store store.Writer

	mu    sync.Mutex
	locks map[string]chan struct{} // repository ごとのロック（容量 1。入れたら取った、抜いたら放した）
	wake  chan string
	// moved は、古いフォーマットバージョンの下のブランチのスナップショットを、版の外に移し終えた repository である。プロセスごとに 1 度だけ移す。
	moved map[string]bool
	// progress は、最後に巡回を始めたか、main のコミットを 1 つ取り込んだ時刻（UnixNano）である。ヘルスチェックに使う。
	progress atomic.Int64
}

// stallFactor は、巡回の間隔の何倍のあいだ進まなければ、止まっているとみなすかである。
const stallFactor = 5

// touch は、進んだことを記録する。
func (w *Worker) touch() { w.progress.Store(time.Now().UnixNano()) }

// Healthy は、worker が止まっていないかを返す。最後に巡回を始めたか、取り込みが進んでから、
// 間隔の stallFactor 倍より長くたっていれば、止まっているとみなす。
// 取り込みが進んだ時刻も見るのは、初めての取り込みが長く、次の巡回を始められないあいだも、止まっているとみなさないためである。
func (w *Worker) Healthy() bool {
	return time.Since(time.Unix(0, w.progress.Load())) <= stallFactor*w.cfg.Interval
}

// New は、worker を作る。
func New(cfg Config, src Source, st store.Writer) *Worker {
	if cfg.Interval == 0 {
		cfg.Interval = time.Minute
	}
	if cfg.BuildTreeWait == 0 {
		cfg.BuildTreeWait = 60 * time.Second
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
	w := &Worker{cfg: cfg, src: src, store: st, locks: map[string]chan struct{}{}, moved: map[string]bool{}, wake: make(chan string, 64)}
	w.touch()
	return w
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

// Wake は、repository の巡回を、すぐに始めさせる。待たずに返る。
func (w *Worker) Wake(repo string) {
	select {
	case w.wake <- repo:
	default:
	}
}

// Run は、間隔ごとと、起こされたときに巡回する。ctx が終わるまで返らない。
func (w *Worker) Run(ctx context.Context) error {
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
	w.touch()
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

// syncIfFree は、repository のロックがすぐに取れるときだけ巡回する。取れなければ（「木を作る」の裏の作業などが
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
	if err := w.moveLegacy(ctx, r.Name); err != nil {
		return err
	}
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
	if !sameHeads(heads, old) {
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
		if err := w.syncBranch(ctx, g, r.Name, name, heads[name], heads["main"]); err != nil {
			w.cfg.Log.Error("ブランチを読めない", "repository", r.Name, "branch", name, "err", err)
		}
	}
	return nil
}

func sameHeads(a, b map[string]string) bool {
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

// moveLegacy は、古いフォーマットバージョンの下に置いていたブランチのスナップショットを、版の外に移す（store.MoveLegacyBranchSnapshots）。
// フォーマットバージョンを上げても、消えたブランチの版を読めるようにするため。repository ごとに、プロセスで 1 度だけ動かす。
// repository のロックを持って呼ぶ。
func (w *Worker) moveLegacy(ctx context.Context, repo string) error {
	w.mu.Lock()
	done := w.moved[repo]
	w.mu.Unlock()
	if done {
		return nil
	}
	n, err := store.MoveLegacyBranchSnapshots(ctx, w.store, repo)
	if err != nil {
		return fmt.Errorf("古い形のブランチのスナップショットを移せない：%w", err)
	}
	if n > 0 {
		w.cfg.Log.Info("古い形のブランチのスナップショットを移した", "repository", repo, "count", n)
	}
	w.mu.Lock()
	w.moved[repo] = true
	w.mu.Unlock()
	return nil
}

// syncMain は、main の、latest より新しいコミットを、古い順に 1 つずつ取り込む（design/backend/worker.md の「すること」）。
// 1 つのコミットごとに、コミットの記録、main の並びの索引、スナップショットツリーを置き、latest をそのコミットに進める。
// スナップショットツリーは、1 つ前のコミットのスナップショットツリーのファイルに、そのコミットの差分を重ねて作る。
func (w *Worker) syncMain(ctx context.Context, g *gitx.Repo, repo, mainSHA string) error {
	latest, err := store.GetLatest(ctx, w.store, repo)
	if err != nil {
		return err
	}
	if latest == mainSHA {
		return nil
	}
	chain, err := g.FirstParents(ctx, mainSHA)
	if err != nil {
		return err
	}
	// 続きから取り込む位置。latest が並びにあれば、その次から。なければ main が書き換えられているので、
	// 並びのうち、スナップショットツリーのあるいちばん新しいコミットの次から（design/backend/snapshot.md の「latest」）。
	start, rewritten := 0, false
	if latest != "" {
		if i := slices.Index(chain, latest); i >= 0 {
			start = i + 1
		} else {
			rewritten = true
			for i := len(chain) - 1; i >= 0; i-- {
				ok, err := store.HasSnapshotTree(ctx, w.store, repo, chain[i])
				if err != nil {
					return err
				}
				if ok {
					start = i + 1
					break
				}
			}
		}
	}
	x := snapshot.NewIndex()
	var files tree.Files
	var broken []tree.Violation
	if start > 0 {
		m, err := store.GetMain(ctx, w.store, repo, chain[start-1])
		if err != nil {
			return fmt.Errorf("%s のスナップショットツリー: %w", chain[start-1], err)
		}
		x = snapshot.IndexFromSnapshotTree(m.Commit, &m.SnapshotTree)
		if files, err = m.SnapshotTree.Files(); err != nil {
			return err
		}
		broken = m.SnapshotTree.Broken
	}
	for i := start; i < len(chain); i++ {
		sha, prev := chain[i], ""
		if i > 0 {
			prev = chain[i-1]
		}
		c, err := g.ReadCommit(ctx, sha)
		if err != nil {
			return fmt.Errorf("%s: %w", sha, err)
		}
		changes, err := g.Diff(ctx, prev, sha)
		if err != nil {
			return fmt.Errorf("%s: %w", sha, err)
		}
		paths := make([]string, len(changes))
		for j, ch := range changes {
			paths[j] = ch.Path
		}
		rec := x.Record(workcopy.CommitInfo(c), paths)
		if files, err = g.ApplyChanges(ctx, files, changes, w.cfg.MaxFileSize); err != nil {
			return fmt.Errorf("%s: %w", sha, err)
		}
		if err := w.putArtifacts(ctx, repo, files, paths); err != nil {
			return err
		}
		t := snapshot.BuildSnapshotTree(files, x)
		newly, _ := tree.NewlyBroken(broken, t.Broken)
		x.AddNewlyBroken(&rec, newly...)
		broken = t.Broken
		if i > 0 {
			x.AddWorkerWarnings(&rec, w.overlap(ctx, g, repo, rec)...)
		}
		if rewritten && i == start {
			x.AddWorkerWarnings(&rec, tree.Violation{Rule: RuleMainRewritten, Node: sha, Detail: "latest " + latest + " が、main の並びにない。main が書き換えられた"})
		}
		t.Warned = x.Warned()
		// コミットの記録、main の並びの索引、スナップショットツリーの順に置き、最後に latest を進める。
		if err := store.PutCommit(ctx, w.store, repo, rec); err != nil {
			return err
		}
		if err := store.PutMainIndex(ctx, w.store, repo, rec.Number, sha); err != nil {
			return err
		}
		if err := store.PutSnapshotTree(ctx, w.store, repo, sha, t); err != nil {
			return err
		}
		err = store.SetLatest(ctx, w.store, repo, sha, latest)
		if errors.Is(err, store.ErrPrecondition) {
			return fmt.Errorf("latest が、読んだあとに書き換えられた（worker が 2 つ動いている？）")
		}
		if err != nil {
			return err
		}
		latest = sha
		w.touch()
	}
	// main が、取り込み終えたコミットまで短く書き換えられたときは、取り込むコミットがない。latest だけを戻す。
	if rewritten && start == len(chain) {
		w.cfg.Log.Warn("main が、取り込み終えたコミットまで書き換えられた", "repository", repo, "latest", latest, "main", mainSHA)
		if err := store.SetLatest(ctx, w.store, repo, mainSHA, latest); err != nil {
			return err
		}
	}
	return nil
}

// RuleMainRewritten は、latest が新しい main の並びになく、main が書き換えられたことの警告である。
const RuleMainRewritten = snapshot.RuleMainRewritten

// overlap は、反映のコミットについて、対象ノードが、その base のあとに別の反映で変わっていないかを確かめる。
// base は、反映した先頭のコミット（Logiforge-Version:）と、反映の直前の main の分かれた点である。
// c は、記録を作ったばかりの反映のコミットで、その親までは、コミットの記録と main の並びの索引が置いてある。
func (w *Worker) overlap(ctx context.Context, g *gitx.Repo, repo string, c snapshot.Commit) []tree.Violation {
	if snapshot.HasTrailer(c, snapshot.TrailerApplyRequest) == "" {
		return nil
	}
	unchecked := func(why string) []tree.Violation {
		return []tree.Violation{{Rule: snapshot.RuleOverlapUnchecked, Node: c.Revision, Detail: why}}
	}
	version := snapshot.VersionCommit(c)
	if version == "" {
		return unchecked("Logiforge-Version: がない")
	}
	if ok, err := g.HasCommit(ctx, version); err != nil || !ok {
		return unchecked("反映した先頭のコミット " + version + " が手元にない")
	}
	base, err := g.MergeBase(ctx, version, *c.Parent)
	if err != nil {
		return unchecked("base を求められない：" + err.Error())
	}
	n := w.mainNumber(ctx, repo, base, c.Number-1)
	if n == 0 {
		return unchecked("base " + base + " が main の並びにない")
	}
	return snapshot.OverlapWarnings(c, n)
}

// mainNumber は、sha が、番号 upTo までの main の並びにあれば、その番号を返す。なければ 0。
func (w *Worker) mainNumber(ctx context.Context, repo, sha string, upTo int) int {
	rec, err := store.GetCommit(ctx, w.store, repo, sha)
	if err != nil || rec.Kind != snapshot.KindMain || rec.Number > upTo {
		return 0
	}
	if got, err := store.GetMainIndex(ctx, w.store, repo, rec.Number); err != nil || got != sha {
		return 0
	}
	return rec.Number
}

// putArtifacts は、paths のうち、中身を読んだ artifact を、sha256 の名前で置き場に置く。すでにあれば置かない。
func (w *Worker) putArtifacts(ctx context.Context, repo string, files tree.Files, paths []string) error {
	for _, p := range paths {
		f, ok := files[p]
		if !ok || f.Content == nil || !isArtifact(p) {
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

// syncBranch は、ブランチの先頭のスナップショットを作り、ブランチの履歴に足す。先頭が変わっていなければ、何もしない。
func (w *Worker) syncBranch(ctx context.Context, g *gitx.Repo, repo, name, head, mainSHA string) error {
	h, raw, err := store.GetBranchHistory(ctx, w.store, repo, name)
	if err != nil {
		return err
	}
	if n := len(h.Entries); n > 0 && !h.Entries[n-1].Deleted && h.Entries[n-1].Head == head {
		return nil
	}
	if mainSHA == "" {
		return fmt.Errorf("main がないので、base を求められない")
	}
	base, err := g.MergeBase(ctx, mainSHA, head)
	if err != nil {
		return err
	}
	if _, err := store.GetCommit(ctx, w.store, repo, head); errors.Is(err, store.ErrNotFound) {
		if err := w.buildBranch(ctx, g, repo, base, head); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	// ブランチの名前の一覧に、先に足す。履歴を書いたあとに止まっても、消えたことを見つけられるようにするため。
	if err := w.addToIndex(ctx, repo, name); err != nil {
		return err
	}
	// 一覧に足すのは、スナップショットを作り終えたコミットだけである。
	h.Entries = append(h.Entries, store.BranchEntry{Head: head, Base: base, SeenAt: w.cfg.Now().UTC()})
	if err := store.PutBranchHistory(ctx, w.store, repo, h, raw); err != nil {
		return err
	}
	return nil
}

func (w *Worker) buildBranch(ctx context.Context, g *gitx.Repo, repo, base, head string) error {
	b, files, err := workcopy.BranchSnapshot(ctx, g, base, head, w.cfg.MaxFileSize)
	if err != nil {
		return err
	}
	if err := w.putArtifacts(ctx, repo, files, slices.Collect(maps.Keys(files))); err != nil {
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

// ErrSnapshotPending は、「木を作る」が、時間の上限までに終わらなかったことを表す。
// 作るのは、裏で続ける。少し待って、もう一度頼めばよい。
var ErrSnapshotPending = errors.New("worker: 木を作るのが、まだ終わらない")

var shaPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// BuildTree は、main のコミット sha のスナップショットツリーがあるかを確かめる（worker の内部 API の「木を作る」）。
// main のすべてのコミットにスナップショットツリーを作るので、作るものはない。あれば nil、記録はあるがまだなければ ErrSnapshotPending、
// 記録もなければ ports.ErrNotFound を返す。
func (w *Worker) BuildTree(ctx context.Context, repoName, sha string) error {
	if !shaPattern.MatchString(sha) {
		return ports.ErrNotFound
	}
	if ok, err := store.HasSnapshotTree(ctx, w.store, repoName, sha); err != nil || ok {
		return err
	}
	if _, err := store.GetCommit(ctx, w.store, repoName, sha); errors.Is(err, store.ErrNotFound) {
		return ports.ErrNotFound
	} else if err != nil {
		return err
	}
	return ErrSnapshotPending
}
