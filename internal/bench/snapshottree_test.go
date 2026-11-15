// Package bench は、スナップショットツリーを作る時間と、読む時間と、大きさを測る（測定用。製品のコードではない）。
//
//	BENCH_SNAPSHOT_TREE=1 go test ./internal/bench/ -run TestSnapshotTree -v -timeout 60m
//
// 測る repository は、main のコミットが BENCH_COMMITS（既定 1000）個で、ノードが 100、1,000、4,000 個に育つもの 3 つである。
// ノードの作り方は TestE2ELargeTree と同じ（1 コミットで、ストーリー 1 つと、その下の仮説 3 つを足す。4 ノード）。
// ノードが目標に届いたあとのコミットは、仮説の background.md の末尾に文を足す（node.edit）。
// 測ること：
//  1. ふだんの取り込み：main のすべてのコミットについて、前のコミットのファイルに差分を重ね（ApplyChanges）、
//     tree.Build と tree.Check で不変条件を確かめ、snapshot.BuildTree でスナップショットツリーを作り、圧縮して置く（PutTree と同じ処理）。
//     すべてのコミットにスナップショットツリーを作るので、これがそのまま「初めての取り込み」の時間と容量になる。
//  2. 全体を読んで作る：先頭のコミットで ReadFiles → BuildTree → PutTree。
//  3. バックエンドが AR を読む：先頭のスナップショットツリーを読んで展開し、Overlay でブランチの差分を重ね、tree.Build と tree.Compute。
//  4. 圧縮後のスナップショットツリーの大きさ。
package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/gitx"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/store"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
	"github.com/yamambainc/hypothesis-verification-harness/internal/workcopy"
)

const repoName = "big"

// gen は、fast-import で repository を作る。
type gen struct {
	buf     bytes.Buffer
	mark    int
	ts      int64
	leaves  []string          // まだ子のない仮説のディレクトリ（幅優先で育てる）
	hyps    []string          // すべての仮説のディレクトリ
	bg      map[string]string // 仮説のディレクトリ → background.md の中身
	nodes   int
	commits int
}

func (g *gen) data(s string) { fmt.Fprintf(&g.buf, "data %d\n%s\n", len(s), s) }

func (g *gen) commit(ref, msg string, files map[string]string, from string) {
	g.mark++
	g.ts++
	fmt.Fprintf(&g.buf, "commit %s\nmark :%d\ncommitter t <t@example.com> %d +0000\n", ref, g.mark, 1_700_000_000+g.ts)
	g.data(msg)
	if from != "" {
		fmt.Fprintf(&g.buf, "from %s\n", from)
	}
	for _, p := range slices.Sorted(mapsKeys(files)) {
		fmt.Fprintf(&g.buf, "M 100644 inline %s\n", p)
		g.data(files[p])
	}
	g.buf.WriteString("\n")
}

func mapsKeys(m map[string]string) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// grow は、ストーリー 1 つと仮説 3 つを足すファイルを返す（TestE2ELargeTree と同じ）。
func (g *gen) grow(i int) map[string]string {
	parent := g.leaves[0]
	g.leaves = g.leaves[1:]
	sid := fmt.Sprintf("s-%010d", i)
	sdir := parent + "/" + sid
	files := map[string]string{sdir + "/story.md": string(tree.MarshalStory(tree.StoryContent{ID: sid, Title: fmt.Sprintf("道筋 %d", i), Summary: strings.Repeat("概要", 20)}))}
	for j := 0; j < 3; j++ {
		hid := fmt.Sprintf("h-%08d%02d", i, j)
		hdir := sdir + "/" + hid
		files[hdir+"/hypothesis.md"] = string(tree.MarshalHypothesis(tree.HypothesisContent{ID: hid, Statement: fmt.Sprintf("仮説 %d-%d", i, j)}))
		g.bg[hdir] = strings.Repeat("背景の文章。", 50) + "\n"
		files[hdir+"/background.md"] = g.bg[hdir]
		g.leaves = append(g.leaves, hdir)
		g.hyps = append(g.hyps, hdir)
	}
	g.nodes += 4
	return files
}

func makeRepo(t *testing.T, targetNodes, commits int) (*gitx.Repo, string, []string) {
	dir := filepath.Join(t.TempDir(), "r.git")
	ctx := context.Background()
	repo, err := gitx.Init(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	g := &gen{bg: map[string]string{}}
	root := "tree/h-0000000000"
	g.leaves, g.nodes = []string{root}, 1
	g.commit("refs/heads/main", "用意\n\nLogiforge-Init: 1", map[string]string{
		root + "/hypothesis.md": string(tree.MarshalHypothesis(tree.HypothesisContent{ID: "h-0000000000", Statement: "大きな問い"})),
	}, "")
	for i := 1; i < commits; i++ {
		var files map[string]string
		if g.nodes+4 <= targetNodes {
			files = g.grow(i)
		} else {
			h := g.hyps[(i*7919)%len(g.hyps)]
			g.bg[h] += fmt.Sprintf("足した文章 %d\n", i)
			files = map[string]string{h + "/background.md": g.bg[h]}
		}
		g.commit("refs/heads/main", fmt.Sprintf("反映 %d\n\nLogiforge-Apply-Request: apr_gen%d\nLogiforge-Session-Id: s-gen", i, i), files, "")
	}
	tip := fmt.Sprintf(":%d", g.mark)
	// AR のブランチ：先頭の main から、ストーリー 1 つと仮説 3 つを足す（4 ノード）。
	g.commit("refs/heads/ar-edit", "提案\n\nLogiforge-Session-Id: s-gen", g.grow(commits), tip)
	cmd := exec.Command("git", "--git-dir", dir, "fast-import", "--quiet")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	cmd.Stdin = &g.buf
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v: %s", err, out)
	}
	chain, err := repo.FirstParents(ctx, revParse(t, dir, "main"))
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != commits {
		t.Fatalf("コミットの数 %d", len(chain))
	}
	return repo, chain[len(chain)-1], chain
}

// phases は、1 コミットのふだんの取り込みの、段階ごとの時間である。
type phases struct {
	git      time.Duration // ReadCommit と Diff（worker が、どちらの取り込みでもコミットごとに呼ぶ）
	apply    time.Duration // ApplyChanges
	check    time.Duration // tree.Build と tree.Check
	build    time.Duration // snapshot.BuildTree
	encode   time.Duration // JSON と gzip
	write    time.Duration // 置き場に書く
	treeSize int           // 圧縮後のスナップショットツリーの大きさ
	rawSize  int           // 圧縮前の JSON の大きさ
	commit   int           // 圧縮後のコミットの記録の大きさ
}

func (p phases) withoutPut() time.Duration { return p.apply + p.check + p.build }
func (p phases) total() time.Duration      { return p.git + p.withoutPut() + p.encode + p.write }

type result struct {
	nodes, commits            int
	perCommit                 []phases
	ingest                    time.Duration // すべてのコミットの取り込みの合計（git の読みを含む）
	fullRead, fullBuild       time.Duration
	fullPut                   time.Duration
	beGet, beFiles, beOverlay time.Duration
	beBuild, beCompute        time.Duration
	beCheckChange             time.Duration
	storeBytes                int64
}

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

func measure(t *testing.T, targetNodes, commits int) result {
	ctx := context.Background()
	repo, tip, chain := makeRepo(t, targetNodes, commits)
	st := &store.Dir{Root: t.TempDir()}
	max := int64(tree.MaxArtifactSize)
	r := result{commits: commits}

	// 1. ふだんの取り込みを、main のすべてのコミットについて行う（すべてのコミットにスナップショットツリーを作る）。
	x := snapshot.NewIndex()
	var files tree.Files
	var broken []tree.Violation
	start := time.Now()
	for i, sha := range chain {
		var p phases
		prev := ""
		if i > 0 {
			prev = chain[i-1]
		}
		t0 := time.Now()
		c, err := repo.ReadCommit(ctx, sha)
		if err != nil {
			t.Fatal(err)
		}
		changes, err := repo.Diff(ctx, prev, sha)
		if err != nil {
			t.Fatal(err)
		}
		p.git = time.Since(t0)
		paths := make([]string, len(changes))
		for j, ch := range changes {
			paths[j] = ch.Path
		}
		rec := x.Record(workcopy.CommitInfo(c), paths)

		t0 = time.Now()
		if files, err = repo.ApplyChanges(ctx, files, changes, max); err != nil {
			t.Fatal(err)
		}
		p.apply = time.Since(t0)

		t0 = time.Now()
		now := tree.Check(tree.Build(files))
		newly, _ := tree.NewlyBroken(broken, now)
		x.AddNewlyBroken(&rec, newly...)
		broken = now
		p.check = time.Since(t0)

		t0 = time.Now()
		tr := snapshot.BuildTree(files, x)
		p.build = time.Since(t0)

		// store.PutTree と同じ処理を、圧縮と書き込みに分けて測る。
		t0 = time.Now()
		b, err := snapshot.Encode(&snapshot.TreeDoc{FormatVersion: snapshot.FormatVersion, Tree: *tr})
		if err != nil {
			t.Fatal(err)
		}
		p.encode = time.Since(t0)
		t0 = time.Now()
		if err := st.Put(ctx, store.SnapshotKey(repoName, sha), b); err != nil {
			t.Fatal(err)
		}
		p.write = time.Since(t0)
		p.treeSize = len(b)
		cb, err := snapshot.Encode(&snapshot.CommitDoc{FormatVersion: snapshot.FormatVersion, Commit: rec})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Put(ctx, store.CommitKey(repoName, sha), cb); err != nil {
			t.Fatal(err)
		}
		p.commit = len(cb)
		if i == len(chain)-1 {
			var raw bytes.Buffer
			_ = jsonEncode(&raw, &snapshot.TreeDoc{FormatVersion: snapshot.FormatVersion, Tree: *tr})
			p.rawSize = raw.Len()
			r.nodes = len(tr.Nodes)
		}
		r.perCommit = append(r.perCommit, p)
	}
	r.ingest = time.Since(start)
	r.storeBytes = dirSize(t, st.Root)

	// 2. 全体を読んで作る（先頭のコミット）。5 回の中央値。
	var reads, builds, puts []time.Duration
	for k := 0; k < 5; k++ {
		t0 := time.Now()
		all, err := repo.ReadFiles(ctx, tip, max)
		if err != nil {
			t.Fatal(err)
		}
		reads = append(reads, time.Since(t0))
		t0 = time.Now()
		tr := snapshot.BuildTree(all, x)
		builds = append(builds, time.Since(t0))
		t0 = time.Now()
		if err := store.PutTree(ctx, st, repoName, tip, tr); err != nil {
			t.Fatal(err)
		}
		puts = append(puts, time.Since(t0))
	}
	r.fullRead, r.fullBuild, r.fullPut = median(reads), median(builds), median(puts)

	// 3. バックエンドが AR を読む。ブランチの差分は、worker と同じく workcopy.BranchSnapshot で作る。
	br, _, err := workcopy.BranchSnapshot(ctx, repo, tip, revParse(t, repo.Dir, "ar-edit"), max)
	if err != nil {
		t.Fatal(err)
	}
	var gets, fls, ovs, bls, cps, ccs []time.Duration
	for k := 0; k < 10; k++ {
		t0 := time.Now()
		m, err := store.GetMain(ctx, st, repoName, tip)
		if err != nil {
			t.Fatal(err)
		}
		gets = append(gets, time.Since(t0))
		t0 = time.Now()
		base, err := m.Tree.Files()
		if err != nil {
			t.Fatal(err)
		}
		fls = append(fls, time.Since(t0))
		t0 = time.Now()
		h, conflicts, err := snapshot.Overlay(base, br.Diff)
		if err != nil || len(conflicts) > 0 {
			t.Fatal(err, conflicts)
		}
		ovs = append(ovs, time.Since(t0))
		t0 = time.Now()
		ht := tree.Build(h)
		bls = append(bls, time.Since(t0))
		t0 = time.Now()
		_ = tree.Compute(ht)
		cps = append(cps, time.Since(t0))
		t0 = time.Now()
		res, err := tree.CheckChange(base, h, tree.Context{})
		if err != nil || !res.OK() {
			t.Fatal(err, res.Rejected)
		}
		ccs = append(ccs, time.Since(t0))
	}
	r.beGet, r.beFiles, r.beOverlay, r.beBuild, r.beCompute, r.beCheckChange = median(gets), median(fls), median(ovs), median(bls), median(cps), median(ccs)
	return r
}

func dirSize(t *testing.T, root string) int64 {
	var n int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}

func ms(d time.Duration) string { return strconv.FormatFloat(float64(d.Microseconds())/1000, 'f', 2, 64) }

// tipAvg は、最後の n コミット（ノードの数が目標に届いたあと）の、段階ごとの中央値である。
func tipAvg(ps []phases, n int) phases {
	ps = ps[len(ps)-n:]
	pick := func(f func(phases) time.Duration) time.Duration {
		var ds []time.Duration
		for _, p := range ps {
			ds = append(ds, f(p))
		}
		return median(ds)
	}
	last := ps[len(ps)-1]
	return phases{
		git: pick(func(p phases) time.Duration { return p.git }), apply: pick(func(p phases) time.Duration { return p.apply }),
		check: pick(func(p phases) time.Duration { return p.check }), build: pick(func(p phases) time.Duration { return p.build }),
		encode: pick(func(p phases) time.Duration { return p.encode }), write: pick(func(p phases) time.Duration { return p.write }),
		treeSize: last.treeSize, rawSize: last.rawSize, commit: last.commit,
	}
}

func TestSnapshotTree(t *testing.T) {
	if os.Getenv("BENCH_SNAPSHOT_TREE") == "" {
		t.Skip("BENCH_SNAPSHOT_TREE=1 のときだけ動かす")
	}
	commits := 1000
	if n, _ := strconv.Atoi(os.Getenv("BENCH_COMMITS")); n > 0 {
		commits = n
	}
	t.Logf("go %s, GOMAXPROCS %d, main のコミット %d", runtime.Version(), runtime.GOMAXPROCS(0), commits)
	var out strings.Builder
	out.WriteString("\n| ノード | git 読み | ApplyChanges | Build+Check | BuildTree | PutTree 抜き計 | gzip+JSON | 書く | 1 コミット計 | 全体 ReadFiles | 全体 BuildTree | 全体 PutTree | BE GetMain | BE 展開 | BE Overlay | BE Build | BE Compute | BE 計 | (参考) CheckChange | 圧縮後 | 圧縮前 | コミットの記録 | 全コミット取り込み | 置き場の合計 |\n")
	out.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, n := range []int{100, 1000, 4000} {
		r := measure(t, n, commits)
		p := tipAvg(r.perCommit, 20)
		be := r.beGet + r.beFiles + r.beOverlay + r.beBuild + r.beCompute
		fmt.Fprintf(&out, "| %d | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %.1f KiB | %.1f KiB | %d B | %.1f s | %.1f MiB |\n",
			r.nodes, ms(p.git), ms(p.apply), ms(p.check), ms(p.build), ms(p.withoutPut()), ms(p.encode), ms(p.write), ms(p.total()),
			ms(r.fullRead), ms(r.fullBuild), ms(r.fullPut),
			ms(r.beGet), ms(r.beFiles), ms(r.beOverlay), ms(r.beBuild), ms(r.beCompute), ms(be), ms(r.beCheckChange),
			float64(p.treeSize)/1024, float64(p.rawSize)/1024, p.commit, r.ingest.Seconds(), float64(r.storeBytes)/1024/1024)
		t.Logf("ノード %d：済み（取り込み %.1f s）", r.nodes, r.ingest.Seconds())
	}
	t.Log("時間の単位は ms。1 コミットの値は、最後の 20 コミットの中央値。全体と BE は 5 回・10 回の中央値。" + out.String())
}

func jsonEncode(w *bytes.Buffer, v any) error { return json.NewEncoder(w).Encode(v) }

func revParse(t *testing.T, dir, rev string) string {
	out, err := exec.Command("git", "--git-dir", dir, "rev-parse", rev).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
