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
	"slices"
	"sort"
	"strings"
	"sync"
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
	// Dir は、repository ごとの clone を置く、永続するディレクトリ。ハートビートのファイルも置く。
	Dir string
	// GitEnv は、git に渡す環境変数（読むだけの資格情報の credential helper など）。
	GitEnv []string
	// Interval は、見回りの間隔。0 なら 1 秒（design/backend/worker.md の「見回り」）。
	Interval time.Duration
	// GitTimeout は、git の操作 1 回の時間の上限。0 なら 10 分。
	GitTimeout time.Duration
	// ListInterval は、repository の一覧を GitForge から読み直す間隔。0 なら 1 分。
	ListInterval time.Duration
	// MaxFileSize は、中身を読むファイルの大きさの上限。0 なら artifact の上限（10 MiB）。
	MaxFileSize int64
	Now         func() time.Time
	Log         *slog.Logger
}

// DefaultInterval は、見回りの間隔の既定である。
const DefaultInterval = time.Second

// DefaultListInterval は、repository の一覧を読み直す間隔の既定である。一覧を読むのは GitForge の API なので、見回りごとには読まない。
const DefaultListInterval = time.Minute

// listTimeout は、repository の一覧を読む時間の上限である。
const listTimeout = 30 * time.Second

// DefaultGitTimeout は、git の操作 1 回の時間の上限の既定である。大きな repository の初めての clone でも終わるようにする。
const DefaultGitTimeout = 10 * time.Minute

// Worker は、worker である。repository ごとに、1 つずつ順に処理する。repository が違えば、並行して処理する。
// どのポートでも待ち受けない。見回りで git を読み、置き場に書くだけである。
type Worker struct {
	cfg   Config
	src   Source
	store store.Writer

	mu    sync.Mutex
	locks map[string]chan struct{} // repository ごとのロック（容量 1。入れたら取った、抜いたら放した）
	// moved は、古いフォーマットバージョンの下のブランチのスナップショットを、版の外に移し終えた repository である。プロセスごとに 1 度だけ移す。
	moved map[string]bool
	// hasLatest は、latest があるとわかった repository である。repositories を書くのに使う。
	hasLatest map[string]bool
	// written は、最後に書いた repositories である。変わったときだけ書く。書いていなければ nil。
	written *string
	// repos は、最後に読めた repository の一覧である。見回りは、これを使う。
	repos []ports.Repository
}

// New は、worker を作る。
func New(cfg Config, src Source, st store.Writer) *Worker {
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.GitTimeout == 0 {
		cfg.GitTimeout = DefaultGitTimeout
	}
	if cfg.ListInterval == 0 {
		cfg.ListInterval = DefaultListInterval
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
	return &Worker{cfg: cfg, src: src, store: st, locks: map[string]chan struct{}{}, moved: map[string]bool{}, hasLatest: map[string]bool{}}
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

// tryLock は、repository のロックがすぐに取れれば、放す関数を返す。取れなければ nil を返す。
func (w *Worker) tryLock(repo string) func() {
	l := w.repoLock(repo)
	select {
	case l <- struct{}{}:
		return func() { <-l }
	default:
		return nil
	}
}

// Run は、見回りの間隔ごとに、すべての repository の見回りを始める。ctx が終わるまで返らない（design/backend/worker.md の「見回り」）。
// 前の回の処理が終わっていない repository は、その回は飛ばす。repository の処理は待たない。
// そのため、1 つの repository の長い取り込みが、ほかの repository の見回りとハートビートを遅らせない。
func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	// repository の一覧は、別の goroutine で、間隔ごとに読み直す。GitForge が止まっても、見回りとハートビートを止めないため。
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.listLoop(ctx)
	}()
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	for {
		w.tick(ctx, &wg)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// tick は、1 回の見回りを始める。ハートビートを書き、repository ごとの処理を、待たずに始める。
func (w *Worker) tick(ctx context.Context, wg *sync.WaitGroup) {
	if err := WriteHeartbeat(w.cfg.Dir, w.cfg.Now()); err != nil {
		w.cfg.Log.Error("ハートビートを書けない", "err", err)
	}
	w.mu.Lock()
	repos := w.repos
	w.mu.Unlock()
	for _, r := range repos {
		unlock := w.tryLock(r.Name)
		if unlock == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer unlock()
			w.syncLogged(ctx, r)
		}()
	}
	w.writeRepositories(ctx, repos)
}

// listLoop は、repository の一覧を、すぐに 1 回と、そのあと ListInterval ごとに読み直す。読むたびに、時間の上限を付ける。
// 読めなければ、前に読めた一覧を使い続ける。
func (w *Worker) listLoop(ctx context.Context) {
	t := time.NewTicker(w.cfg.ListInterval)
	defer t.Stop()
	for {
		w.refreshRepos(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *Worker) refreshRepos(ctx context.Context) {
	lctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	repos, err := w.src.ListRepositories(lctx)
	if err != nil {
		w.cfg.Log.Error("repository の一覧を読めない。前に読めた一覧を使う", "err", err)
		return
	}
	w.mu.Lock()
	w.repos = repos
	w.mu.Unlock()
}

// SyncAll は、repository の一覧を読み、すべての repository を処理し終えるまで待ってから、repositories を書く。
// テストだけが使う。Run は、これを呼ばない（見回りでは、repository の処理を待たない）。
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
			defer w.lock(r.Name)()
			w.syncLogged(ctx, r)
		}()
	}
	wg.Wait()
	w.writeRepositories(ctx, repos)
}

// syncLogged は、repository を処理し、誤りを repository の名前をつけてログに書く。運用は、このログで、latest が進まないことに気づく。
func (w *Worker) syncLogged(ctx context.Context, r ports.Repository) {
	if err := w.syncLocked(ctx, r); err != nil {
		w.cfg.Log.Error("見回りに失敗した", "repository", r.Name, "err", err, "timeout", errors.Is(err, gitx.ErrTimeout))
	}
}

// writeRepositories は、latest がある repository の名前の一覧を、変わったときだけ、置き場の repositories に書く。
func (w *Worker) writeRepositories(ctx context.Context, repos []ports.Repository) {
	var names []string
	for _, r := range repos {
		w.mu.Lock()
		ok := w.hasLatest[r.Name]
		w.mu.Unlock()
		if !ok {
			latest, err := store.GetLatest(ctx, w.store, r.Name)
			if err != nil || latest == "" {
				continue
			}
			w.mu.Lock()
			w.hasLatest[r.Name] = true
			w.mu.Unlock()
		}
		names = append(names, r.Name)
	}
	sort.Strings(names)
	key := strings.Join(names, "\n")
	w.mu.Lock()
	same := w.written != nil && *w.written == key
	w.mu.Unlock()
	if same {
		return
	}
	if err := store.PutRepositories(ctx, w.store, names); err != nil {
		w.cfg.Log.Error("repositories を書けない", "err", err)
		return
	}
	w.mu.Lock()
	w.written = &key
	w.mu.Unlock()
}

func (w *Worker) repo(ctx context.Context, name string) (*gitx.Repo, error) {
	dir := filepath.Join(w.cfg.Dir, name+".git")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if _, err := gitx.Init(ctx, dir); err != nil {
			return nil, err
		}
	}
	return &gitx.Repo{Dir: dir, Env: w.cfg.GitEnv, Timeout: w.cfg.GitTimeout}, nil
}

// Sync は、1 つの repository を見回りする（design/backend/worker.md の「すること」）。何度動かしても、結果は同じになる。
// repository のロックを取るまで待つ。
func (w *Worker) Sync(ctx context.Context, r ports.Repository) error {
	defer w.lock(r.Name)()
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
