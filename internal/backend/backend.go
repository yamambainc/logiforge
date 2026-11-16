// Package backend は、Logiforge のバックエンド（core）である（design/backend/README.md）。
// ルールを持つのは、このパッケージだけである。データは保持しない。要求ごとに、ポートから読む。
package backend

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ids"
	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
	"github.com/yamambainc/hypothesis-verification-harness/internal/snapshot"
	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// Backend は、バックエンドである。ポートは、起動処理が差し込む（design/architecture.md の「起動時の組み立て」）。
type Backend struct {
	Forge ports.GitForge
	Auth  ports.Authenticator
	Snap  ports.SnapshotStore
	// Git は、apply request の作成で、ブランチを git から読む（internal/workcopy の作業用の複製）。
	Git GitReader
	// WaitTimeout は、書いたものが読む側に出るまで待つ時間の上限（design/api/writes.md の「書いた直後の読み」）。0 なら 10 秒。
	WaitTimeout time.Duration
	// Now は、時刻を返す。nil なら time.Now。
	Now func() time.Time
}

// GitReader は、apply request の作成で、git からブランチを読むことである。実装は internal/workcopy の作業用の複製で、
// テストのために interface にする（design/backend/apply-request.md の「作成」）。
type GitReader interface {
	// ReadBranch は、main とブランチを fetch し、main と分かれた点からの差分のスナップショットを返す。ブランチがなければ ports.ErrNotFound。
	ReadBranch(ctx context.Context, repo, gitForgeRef, branch string) (*snapshot.Branch, error)
	// ReadFiles は、ReadBranch で fetch したコミットの木のファイルを読む。
	ReadFiles(ctx context.Context, repo, gitForgeRef, sha string) (tree.Files, error)
}

// Caller は、API の要求を送った主体と、そのトークンである。
type Caller struct {
	Token     string
	Principal ports.Principal
}

// Authenticate は、トークンの主体を確かめる。
func (b *Backend) Authenticate(ctx context.Context, token string) (Caller, error) {
	if token == "" {
		return Caller{}, fail(CodeUnauthenticated, "トークンがない")
	}
	p, err := b.Auth.Authenticate(ctx, token)
	if err != nil {
		return Caller{}, err
	}
	return Caller{Token: token, Principal: p}, nil
}

// access は、主体が repository を使えるかを確かめ、gitforge_ref を返す。
// 読めない repository と、latest のない repository は、どちらも not_found にする（design/backend/README.md の「状態を持たない」）。
func (b *Backend) access(ctx context.Context, c Caller, repo string) (string, error) {
	if !ids.RepositoryNamePattern.MatchString(repo) {
		return "", fail(CodeNotFound, "repository %q がない", repo)
	}
	ok, err := b.Auth.CanRead(ctx, c.Token, repo)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fail(CodeNotFound, "repository %q がない", repo)
	}
	if _, err := b.Snap.Latest(ctx, repo); errors.Is(err, ports.ErrNotFound) {
		return "", fail(CodeNotFound, "repository %q がない", repo)
	} else if err != nil {
		return "", err
	}
	return b.Forge.GitForgeRef(ctx, repo)
}

// view は、ある main のコミットのスナップショットツリーと、そこから戻したファイルと木である。値は、worker が計算したものを持つ。
type view struct {
	repo  string
	main  *snapshot.Main
	tree  *tree.Tree
	files tree.Files
}

// mainAt は、revision の main のスナップショットを読む。revision が空なら、latest を読む。
// スナップショットがなければ snapshot_pending にする。まだ作られていないのか、git にもないのかは、区別しない。
func (b *Backend) mainAt(ctx context.Context, repo, revision string) (*view, error) {
	if revision == "" {
		latest, err := b.Snap.Latest(ctx, repo)
		if err != nil {
			return nil, err
		}
		revision = latest
	}
	m, err := b.Snap.Main(ctx, repo, revision)
	// main のすべてのコミットにスナップショットツリーを作るので、ないのは worker が追いついていないか、git にもないときである。
	if errors.Is(err, ports.ErrNotFound) {
		return nil, fail(CodeSnapshotPending, "main のコミット %s のスナップショットが、まだない", revision)
	}
	if err != nil {
		return nil, err
	}
	files, err := m.SnapshotTree.Files()
	if err != nil {
		return nil, err
	}
	return &view{repo: repo, main: m, tree: tree.Build(files), files: files}, nil
}

func (v *view) revision() string { return v.main.Commit.Revision }

// dirNode は、ディレクトリからノードの識別子を引く。
func (v *view) dirNode() map[string]string {
	m := map[string]string{}
	for id, n := range v.main.SnapshotTree.Nodes {
		m[n.Dir] = id
	}
	return m
}

// targetOf は、ファイルのパスから、その変化の対象ノードを求める。そのパスを含む、いちばん近い、今の木にあるノードである。
//   - 検証結果と背景情報：その仮説
//   - 新しいストーリーと仮説：親の仮説（新しいディレクトリは今の木にないので、さかのぼる）
//   - inactive 化：そのストーリー
func targetOf(dirs map[string]string, p string) string {
	for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
		if id, ok := dirs[d]; ok {
			return id
		}
	}
	return ""
}

// hypothesis は、仮説のノードの応答を作る。
func (v *view) hypothesis(h *tree.Hypothesis, claims claims) *Hypothesis {
	n := v.main.SnapshotTree.Nodes[h.ID]
	out := &Hypothesis{NodeType: "hypothesis", ID: h.ID, Statement: h.Statement, Background: h.Background,
		OpenApplyRequestIDs: claims.ar(h.ID), OpenHumanRequestIDs: claims.hr(h.ID)}
	if h.Parent != nil {
		p := h.Parent.ID
		out.ParentStoryID = &p
	}
	if n != nil {
		out.Value, out.Leaf, out.NeedsReconsideration, out.Inactive = n.Value, n.Leaf, n.NeedsReconsideration, n.Inactive
	}
	if valid := h.ValidVerdicts(); len(valid) == 1 {
		id := valid[0].ID
		out.ValidVerdictID = &id
	}
	return out
}

// story は、ストーリーのノードの応答を作る。
func (v *view) story(s *tree.Story, claims claims) *Story {
	n := v.main.SnapshotTree.Nodes[s.ID]
	out := &Story{NodeType: "story", ID: s.ID, ParentHypothesisID: s.Parent.ID, Title: s.Title, Summary: s.Summary, State: "active",
		OpenApplyRequestIDs: claims.ar(s.ID), OpenHumanRequestIDs: claims.hr(s.ID)}
	if s.Inactivated {
		out.State = "inactive"
		r := s.Reason
		out.Reason = &r
	}
	if n != nil {
		out.Value, out.Inactive = n.Value, n.Inactive
	}
	return out
}

// node は、識別子のノードの応答を作る。なければ nil。
func (v *view) node(id string, claims claims) Node {
	if h, ok := v.tree.Hypotheses[id]; ok {
		return v.hypothesis(h, claims)
	}
	if s, ok := v.tree.Stories[id]; ok {
		return v.story(s, claims)
	}
	return nil
}

// subtree は、根から depth 段までのノードを、深さ優先の順に返す。depth が負なら、すべて。
func (v *view) subtree(rootID string, depth int, claims claims) []Node {
	var out []Node
	var walkH func(h *tree.Hypothesis, d int)
	var walkS func(s *tree.Story, d int)
	walkH = func(h *tree.Hypothesis, d int) {
		out = append(out, v.hypothesis(h, claims))
		if depth >= 0 && d >= depth {
			return
		}
		for _, s := range h.Stories {
			walkS(s, d+1)
		}
	}
	walkS = func(s *tree.Story, d int) {
		out = append(out, v.story(s, claims))
		if depth >= 0 && d >= depth {
			return
		}
		for _, h := range s.Hypotheses {
			walkH(h, d+1)
		}
	}
	if h, ok := v.tree.Hypotheses[rootID]; ok {
		walkH(h, 0)
	} else if s, ok := v.tree.Stories[rootID]; ok {
		walkS(s, 0)
	}
	return out
}

// inSubtree は、id のノードが、root の部分木にあるかを返す。
func (v *view) inSubtree(root, id string) bool {
	for id != "" {
		if id == root {
			return true
		}
		n := v.main.SnapshotTree.Nodes[id]
		if n == nil {
			return false
		}
		id = n.Parent
	}
	return false
}

// warnings は、反映の手順によらない変更や、worker が見つけた警告を、木の警告にする。
func (v *view) warnings() []string {
	out := []string{}
	for _, h := range v.main.SnapshotTree.Warned {
		if h.NoApplyRequest {
			out = append(out, h.Revision+"：反映の記録がないコミット（反映の手順によらない変更）")
		}
		if h.NewlyBroken {
			out = append(out, h.Revision+"：このコミットで、木の不変条件が破れた")
		}
		if h.WorkerWarned {
			out = append(out, h.Revision+"：反映の対象ノードの重なり、または main の書き換えの警告がある")
		}
	}
	return out
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func trailerValue(ts []snapshot.Trailer, key string) string {
	for _, t := range ts {
		if strings.EqualFold(t.Key, key) {
			return t.Value
		}
	}
	return ""
}

type treeVerdict = tree.Verdict

type snapshotTrailer = snapshot.Trailer
