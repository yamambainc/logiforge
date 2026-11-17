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

	"github.com/yamambainc/logiforge/internal/gitx"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
	"github.com/yamambainc/logiforge/internal/workcopy"
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
	// ListInterval は、repository の一覧を GitForge から読み直す間隔。0 なら 10 秒。
	ListInterval time.Duration
	// MaxFileSize は、中身を読むファイルの大きさの上限。0 なら artifact の上限（10 MiB）。
	MaxFileSize int64
	// Verifier は、main のコミットが反映によるものかを、中身で決める（design/backend/verify.md）。nil なら決めない（trailer で決める）。
	Verifier Verifier
	// Records は、記録の控えに、GitForge の新しい記録を足す（design/backend/verify.md の「記録の控え」）。nil なら足さない。
	Records RecordLog
	// RecordInterval は、記録を読んで控えに足す間隔。0 なら見回りごと。
	RecordInterval time.Duration
	Now            func() time.Time
	Log            *slog.Logger
}

// DefaultInterval は、見回りの間隔の既定である。
const DefaultInterval = time.Second

// DefaultListInterval は、repository の一覧を読み直す間隔の既定である。一覧を読むのは GitForge の API なので、見回りごとには読まない。
const DefaultListInterval = 10 * time.Second

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
	// firstSeen は、merge した pull request がまだ見つからないコミットを、worker が初めて見た時刻である。
	// コミットの時刻は push する人が決められるので、待つ時間は、これから数える。プロセスごとに持つ（止まれば、待ち直すだけである）。
	firstSeen map[string]time.Time
	// repos は、最後に読めた repository の一覧である。見回りは、これを使う。
	repos []ports.Repository
	// recordsAt は、repository ごとに、最後に記録の控えに足した時刻である。
	recordsAt map[string]time.Time
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
	// 一度も一覧を読めていないあいだは、repository の処理も、repositories を書くこともしない。
	// 空の一覧で書くと、GitForge に届かないまま再起動したときに、バックエンドの repository の一覧が空になるから。
	if repos == nil {
		return
	}
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
	if repos == nil {
		repos = []ports.Repository{}
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
	// ディレクトリの repository では、ディレクトリの中だけを読む（design/directory/history.md）。
	g.Prefix = r.Directory()
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
	// 記録の控えは、main より先に足す。反映かの判定に使うから。
	if err := w.syncRecords(ctx, r); err != nil {
		return fmt.Errorf("記録の控え: %w", err)
	}
	if mainSHA, ok := heads["main"]; ok {
		if err := w.syncMain(ctx, g, r, mainSHA); err != nil {
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

// syncRecords は、前に足してから RecordInterval が過ぎていれば、記録の控えに、GitForge の新しい記録を足す。
func (w *Worker) syncRecords(ctx context.Context, r ports.Repository) error {
	if w.cfg.Records == nil {
		return nil
	}
	now := w.cfg.Now()
	w.mu.Lock()
	last, ok := w.recordsAt[r.Name]
	w.mu.Unlock()
	if ok && now.Sub(last) < w.cfg.RecordInterval {
		return nil
	}
	if err := w.cfg.Records.Sync(ctx, r.GitForgeRef); err != nil {
		return err
	}
	w.mu.Lock()
	if w.recordsAt == nil {
		w.recordsAt = map[string]time.Time{}
	}
	w.recordsAt[r.Name] = now
	w.mu.Unlock()
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
func (w *Worker) syncMain(ctx context.Context, g *gitx.Repo, r ports.Repository, mainSHA string) error {
	repo := r.Name
	latest, err := store.GetLatest(ctx, w.store, repo)
	if err != nil {
		return err
	}
	if latest == mainSHA {
		return nil
	}
	// ディレクトリの repository では、版はディレクトリを変えたコミットだけである。latest は版で、git の main の先頭とは限らないので、
	// どこまで読んだかは scanned で持つ（design/directory/history.md の「worker」）。
	dir := r.Directory()
	scanned := ""
	if dir != "" {
		if scanned, err = store.GetScanned(ctx, w.store, repo); err != nil {
			return err
		}
		if scanned == mainSHA {
			return nil
		}
	}
	br, err := store.GetBroken(ctx, w.store, repo)
	if err != nil {
		return err
	}
	chain, err := g.FirstParents(ctx, mainSHA)
	if err != nil {
		return err
	}
	// 続きから取り込む位置。latest が並びにあれば、その次から。なければ main が書き換えられているので、
	// 並びのうち、スナップショットツリーのあるいちばん新しいコミットの次から（design/backend/snapshot.md の「latest」）。
	// prevVersion は、続きの最初の版の、1 つ前の版である（そのスナップショットツリーに重ねる）。
	start, rewritten, prevVersion := 0, false, ""
	if latest != "" {
		if i := slices.Index(chain, latest); i >= 0 {
			start, prevVersion = i+1, latest
			// ディレクトリの repository では、latest のあとのコードのコミットは、読み終えていれば読み直さない。
			if j := slices.Index(chain, scanned); dir != "" && j > i {
				start = j + 1
			}
		} else {
			rewritten = true
			for i := len(chain) - 1; i >= 0; i-- {
				ok, err := store.HasSnapshotTree(ctx, w.store, repo, chain[i])
				if err != nil {
					return err
				}
				if ok {
					start, prevVersion = i+1, chain[i]
					break
				}
			}
		}
	}
	// main の書き換えは、壊れた状態にする（design/backend/broken.md の「壊れている状態」）。最後に正しかった時点は、書き換えの前の latest。
	// すでに壊れていれば、引き継ぐ。
	if rewritten && br == nil {
		first := mainSHA
		if start < len(chain) {
			first = chain[start]
		}
		br = &store.Broken{LastGood: latest, First: first, Reason: string(RuleMainRewritten)}
	}
	x := snapshot.NewIndex()
	var files tree.Files
	var broken []tree.Violation
	if prevVersion != "" {
		m, err := store.GetMain(ctx, w.store, repo, prevVersion)
		if err != nil {
			return fmt.Errorf("%s のスナップショットツリー: %w", prevVersion, err)
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
		changes, outside, err := g.DiffAll(ctx, prev, sha)
		if err != nil {
			return fmt.Errorf("%s: %w", sha, err)
		}
		// ディレクトリを変えないコミット（コードのコミット）は、版にしない（design/directory/history.md の「版」）。
		if dir != "" && len(changes) == 0 {
			continue
		}
		version, before := x.Head().Number, x.Head().Revision
		paths := make([]string, len(changes))
		for j, ch := range changes {
			paths[j] = ch.Path
		}
		info := workcopy.CommitInfo(c)
		if files, err = g.ApplyChanges(ctx, files, changes, w.cfg.MaxFileSize); err != nil {
			return fmt.Errorf("%s: %w", sha, err)
		}
		v, err := w.authorized(ctx, g, r, sha, c.Parents, before, version, info, changes, outside, files, br)
		if err != nil {
			// 判定できないコミットは、取り込まずに、次の見回りでやり直す。
			return fmt.Errorf("%s が反映によるものかを確かめられない: %w", sha, err)
		}
		// 根のコミットが用意のコミットでなければ、最後に正しかった時点がないので、取り込まない。repository は作り直す
		// （design/backend/verify.md の「repository の用意のコミット」）。
		if v != nil && version == 0 && !v.Authorized {
			return fmt.Errorf("根のコミット %s が、Logiforge の用意のコミットでない（%s）。repository を作り直す", sha, v.Detail)
		}
		if v != nil {
			info.Authorized, info.ApplyRequest, info.AuthorizationDetail = &v.Authorized, v.ApplyRequest, v.Detail
		}
		// 別リポジトリ方式から移した用意のコミットなら、元の repository の置き場に、移した先の名前を書く（design/directory/migrate.md）。
		// 移した元は、用意のコミットの署名に含まれるので、偽れない。
		if from := trailerOf(info, snapshot.TrailerMigratedFrom); version == 0 && v != nil && v.Authorized && from != "" {
			if src, _, _ := strings.Cut(from, " "); src != "" && src != repo {
				if err := store.PutMigratedTo(ctx, w.store, src, repo); err != nil {
					return err
				}
			}
		}
		rec := x.Record(info, paths)
		if br == nil && info.Authorized != nil && !*info.Authorized {
			br = &store.Broken{LastGood: before, First: sha, Reason: string(snapshot.RuleNoApplyRequest)}
		} else if br != nil && v != nil && v.Authorized && v.Kind != "" {
			// 直すための apply request の反映で、直す条件は、反映かの判定で確かめた（design/backend/broken.md の「直すための apply request」）。
			br = nil
		}
		if err := w.putArtifacts(ctx, repo, files, paths); err != nil {
			return err
		}
		t := snapshot.BuildSnapshotTree(files, x)
		newly, _ := tree.NewlyBroken(broken, t.Broken)
		x.AddNewlyBroken(&rec, newly...)
		broken = t.Broken
		if version > 0 {
			x.AddWorkerWarnings(&rec, w.overlap(ctx, g, repo, rec)...)
		}
		// 反映のコミットが、ディレクトリの外も変えていたら、警告にする（判定には使わない。design/directory/history.md の「反映かの判定」）。
		if len(outside) > 0 && v != nil && v.Authorized {
			x.AddWorkerWarnings(&rec, tree.Violation{Rule: tree.RuleOutsideDirectory, Node: sha, Detail: "ディレクトリの外も変えている：" + strings.Join(outside, "、")})
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
		// 壊れていることの記録は、latest を進める前に書く。読む側が、壊れた木を正しいものとして読まないようにするため。
		if err := w.putBroken(ctx, repo, br); err != nil {
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
	if rewritten && latest != prevVersion && prevVersion != "" && (start == len(chain) || dir != "") {
		w.cfg.Log.Warn("main が、取り込み終えたコミットまで書き換えられた", "repository", repo, "latest", latest, "main", mainSHA)
		if err := w.putBroken(ctx, repo, br); err != nil {
			return err
		}
		if err := store.SetLatest(ctx, w.store, repo, prevVersion, latest); err != nil {
			return err
		}
	}
	if dir != "" {
		return store.PutScanned(ctx, w.store, repo, mainSHA)
	}
	return nil
}

// Verifier は、main のコミットが反映によるものかを、中身で決める（design/backend/verify.md）。
// parents はコミットの親（最初の親が 1 つ前の main のコミット）、files はそのコミットの木、broken は 1 つ前の main までの壊れていることの記録である。
//
// prevVersion は、1 つ前の版の SHA である。判定は、その版の木に重ねて行う。
type Verifier interface {
	VerifyMerge(ctx context.Context, r ports.Repository, sha string, parents []string, prevVersion string, files tree.Files, broken *store.Broken) (ports.MergeVerdict, error)
	// VerifyInit は、用意のコミットの trailer Logiforge-Init-Signature: が、種類 kind（ports.InitRoot、ports.InitSecond、ports.InitDirectory）、
	// repository（ディレクトリを含む）、1 つ前のコミット parent（init-second のほかは空）、移した元 migratedFrom、
	// 加えたファイル（パスから中身の sha256。ディレクトリの repository では、ディレクトリの中のパス）に対する署名かを確かめる。
	VerifyInit(ctx context.Context, r ports.Repository, kind, parent, migratedFrom, signature string, added map[string]string) (bool, error)
}

// RecordLog は、記録の控えに、GitForge の新しい記録を足す（design/backend/verify.md の「記録の控え」）。
type RecordLog interface {
	Sync(ctx context.Context, gitForgeRef string) error
}

// authorized は、main のコミットが反映によるものかを決める。Verifier がなければ nil（trailer で決める）。
// 用意のコミット（根のコミットと、GitHub で背景情報を置く根の次のコミット）は、trailer Logiforge-Init-Signature: の署名と、
// 差分がファイルを加えるだけであることで決める（design/backend/verify.md の「repository の用意のコミット」）。
// なぜ根も署名で決めるか：GitHub では、管理者が Write を与えてから用意するまでのあいだに、ほかの人が根を push できるから。
//
// version は、このコミットの前の版の数（最初の版なら 0）、prevVersion は 1 つ前の版の SHA である。git リポジトリ全体の repository では、
// どのコミットも版なので、git の first-parent と同じである。ディレクトリの repository では、1 つ前の版は、git の親とは限らない。
// outside は、ディレクトリの repository で、このコミットがディレクトリの外で変えたパスである。
func (w *Worker) authorized(ctx context.Context, g *gitx.Repo, r ports.Repository, sha string, parents []string, prevVersion string, version int, info snapshot.CommitInfo, changes []gitx.Change, outside []string, files tree.Files, br *store.Broken) (*ports.MergeVerdict, error) {
	if w.cfg.Verifier == nil {
		return nil, nil
	}
	if version == 0 {
		sig := trailerOf(info, snapshot.TrailerInitSignature)
		if !hasTrailer(info, snapshot.TrailerInit) || sig == "" {
			return &ports.MergeVerdict{Detail: "最初の版に、Logiforge-Init: と Logiforge-Init-Signature: がない"}, nil
		}
		kind := ports.InitRoot
		if r.Directory() != "" {
			// ディレクトリの repository の用意のコミット（design/directory/history.md の「用意のコミット」）。
			kind = ports.InitDirectory
		}
		return w.initVerdict(ctx, r, kind, "", sig, info, changes, outside, files)
	}
	if sig := trailerOf(info, snapshot.TrailerInitSignature); r.Directory() == "" && version == 1 && sig != "" && len(parents) == 1 {
		return w.initVerdict(ctx, r, ports.InitSecond, parents[0], sig, info, changes, outside, files)
	}
	v, err := w.cfg.Verifier.VerifyMerge(ctx, r, sha, parents, prevVersion, files, br)
	if err != nil {
		return nil, err
	}
	// 先頭のコミットのスナップショットがなければ（ブランチを取り込む前に merge された）、作ってから決め直す。
	// 先頭のコミットは、fetch した refs/pull/*/head にある。
	if v.MissingHead != "" {
		ok, err := g.HasCommit(ctx, v.MissingHead)
		if err != nil {
			return nil, err
		}
		if !ok {
			v = ports.MergeVerdict{Pending: true, Detail: "pull request の先頭のコミット " + v.MissingHead + " が手元にない"}
		} else {
			base, err := g.MergeBase(ctx, parents[0], v.MissingHead)
			if err != nil {
				return nil, err
			}
			if err := w.buildBranch(ctx, g, r.Name, base, v.MissingHead); err != nil {
				return nil, err
			}
			if v, err = w.cfg.Verifier.VerifyMerge(ctx, r, sha, parents, prevVersion, files, br); err != nil {
				return nil, err
			}
			if v.MissingHead != "" {
				return nil, fmt.Errorf("先頭のコミット %s のスナップショットを作ったのに、読めない", v.MissingHead)
			}
		}
	}
	// GitForge は、merge のすぐあと、merge のコミットから pull request を引けないことがある（Forgejo で確かめた）。
	// worker が初めて見てから待つ時間のあいだは、反映によらないと決めずに、やり直す。
	// コミットの時刻は使わない。push する人が、未来の時刻にして、待ちを終わらせないようにできるから。
	if v.Pending {
		now := w.cfg.Now()
		w.mu.Lock()
		if w.firstSeen == nil {
			w.firstSeen = map[string]time.Time{}
		}
		seen, ok := w.firstSeen[sha]
		if !ok {
			seen = now
			w.firstSeen[sha] = now
		}
		waiting := now.Sub(seen) < mergeLookupGrace
		if !waiting {
			delete(w.firstSeen, sha)
		}
		w.mu.Unlock()
		if waiting {
			return nil, fmt.Errorf("%s。次の見回りでやり直す", v.Detail)
		}
		v = ports.MergeVerdict{Detail: v.Detail}
	}
	w.mu.Lock()
	delete(w.firstSeen, sha)
	w.mu.Unlock()
	return &v, nil
}

// initVerdict は、用意のコミットを確かめる。差分は、ファイルを加えることだけで、その中身の sha256 に署名が合う。parent は、根では空。
func (w *Worker) initVerdict(ctx context.Context, r ports.Repository, kind, parent, sig string, info snapshot.CommitInfo, changes []gitx.Change, outside []string, files tree.Files) (*ports.MergeVerdict, error) {
	if len(outside) > 0 {
		return &ports.MergeVerdict{Detail: "用意のコミットが、ディレクトリの外も変えている（" + strings.Join(outside, "、") + "）"}, nil
	}
	added := map[string]string{}
	for _, ch := range changes {
		if ch.Status != "A" {
			return &ports.MergeVerdict{Detail: "用意のコミットが、ファイルを加えること以外の変更（" + ch.Path + "）を含む"}, nil
		}
		f, ok := files[ch.Path]
		switch {
		case ok && f.Content != nil:
			added[ch.Path] = tree.SHA256Hex(f.Content)
		case ok && f.SHA256 != "":
			added[ch.Path] = f.SHA256
		default:
			return &ports.MergeVerdict{Detail: "用意のコミットの " + ch.Path + " の中身を読めない"}, nil
		}
	}
	ok, err := w.cfg.Verifier.VerifyInit(ctx, r, kind, parent, trailerOf(info, snapshot.TrailerMigratedFrom), sig, added)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &ports.MergeVerdict{Detail: "Logiforge-Init-Signature: の署名が合わない"}, nil
	}
	return &ports.MergeVerdict{Authorized: true}, nil
}

func trailerOf(info snapshot.CommitInfo, key string) string {
	for _, t := range info.Trailers {
		if t.Key == key {
			return t.Value
		}
	}
	return ""
}

func hasTrailer(info snapshot.CommitInfo, key string) bool {
	for _, t := range info.Trailers {
		if t.Key == key {
			return true
		}
	}
	return false
}

// mergeLookupGrace は、merge した pull request の見つからないコミットを、反映によらないと決めるまで待つ時間である。
// なぜ 60 秒か：GitForge が merge のコミットと pull request を結びつけるまでの遅れ（数秒）より十分長く、
// 反映によらないコミットで、壊れた状態になるのを、長くは遅らせないため。
const mergeLookupGrace = 60 * time.Second

// putBroken は、壊れていることの記録を、変わったときだけ書く。
func (w *Worker) putBroken(ctx context.Context, repo string, br *store.Broken) error {
	old, err := store.GetBroken(ctx, w.store, repo)
	if err != nil {
		return err
	}
	if (old == nil && br == nil) || (old != nil && br != nil && *old == *br) {
		return nil
	}
	if br != nil {
		w.cfg.Log.Warn("repository が壊れた", "repository", repo, "reason", br.Reason, "first", br.First, "last_good", br.LastGood)
	} else {
		w.cfg.Log.Info("repository が直った", "repository", repo)
	}
	return store.PutBroken(ctx, w.store, repo, br)
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
	// ディレクトリの repository では、ディレクトリの中を変えるブランチだけを、ブランチの履歴に入れる（design/directory/history.md の「worker」）。
	if g.Prefix != "" {
		changes, _, err := g.DiffAll(ctx, base, head)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return nil
		}
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
