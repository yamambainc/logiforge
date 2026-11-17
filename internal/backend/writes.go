package backend

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/ids"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/tree"
)

// Written は、書き込みの結果である。Pending が空でなければ、書いたものが、待つ時間の上限までに読む側に出なかった
// （Logiforge-Pending-Commit に入れるコミット）。
type Written[T any] struct {
	Value   T
	Pending string
}

// 書き込みの決まり（design/backend/README.md の「認可」、design/api/README.md の「セッション ID」）。

func requireUser(c Caller) error {
	if c.Principal.Kind != ports.PrincipalUser {
		return fail(CodeForbidden, "この操作は、利用者（user）だけが使える")
	}
	return nil
}

func requireClient(c Caller, session string) error {
	if c.Principal.Kind != ports.PrincipalClient {
		return fail(CodeForbidden, "この操作は、client だけが使える")
	}
	if session == "" {
		return fail(CodeSessionRequired, "Logiforge-Session-Id がない")
	}
	if len(session) > 128 {
		return fail(CodeValidationFailed, "Logiforge-Session-Id は 128 文字まで")
	}
	return nil
}

func (b *Backend) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// requestID は、Idempotency-Key があれば決定的に、なければ ULID で、識別子を作る。
func (b *Backend) requestID(prefix, repo string, c Caller, key string) (string, error) {
	if key == "" {
		return ids.NewULID(prefix, b.now()), nil
	}
	if len(key) > 128 {
		return "", fail(CodeValidationFailed, "Idempotency-Key は 128 文字まで")
	}
	return ids.FromKey(prefix, repo, c.Principal.Login, key), nil
}

// waitLatest は、done が真になるまで、上限つきで待つ。上限を過ぎたら false を返す。
// worker は起こさない。worker が見回りで git を見て、latest を進める（design/api/writes.md の「書いた直後の読み」）。
func (b *Backend) waitLatest(ctx context.Context, repo string, done func(latest string) bool) bool {
	timeout := b.WaitTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		latest, err := b.Snap.Latest(ctx, repo)
		if err == nil && done(latest) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// caughtUp は、worker が、GitForge の main の先頭 head まで読み終えたかを返す。
//   - git リポジトリ全体の repository：head が、latest までの main の並びにある。
//   - ディレクトリの repository：scanned が、head と同じか、head より先にある（design/directory/main.md の「反映の順番」）。
//     コードの merge では版が進まないので、latest は GitForge の main に追いつかないからである。
func (b *Backend) caughtUp(ctx context.Context, repo, ref, latest, head string) bool {
	if _, dir := ports.SplitRef(ref); dir == "" {
		return b.inHistory(ctx, repo, latest, head)
	}
	scanned, err := b.Snap.Scanned(ctx, repo)
	if err != nil || scanned == "" {
		return false
	}
	ok, err := b.Git.ReachedMain(ctx, repo, ref, head, scanned)
	return err == nil && ok
}

// ---- repository の作成 ----

// initCommit は、コミットが repository の用意で置いたコミット（trailer `Logiforge-Init:` を持つ）かを返す。
// 用意のコミットは、GitForge によって 1 つか 2 つである（design/adapters/github.md）。
func initCommit(c snapshot.Commit) bool {
	if c.Number > snapshot.MaxInitCommits {
		return false
	}
	for _, t := range c.Trailers {
		if t.Key == snapshot.TrailerInit {
			return true
		}
	}
	return false
}

// CreateRepository は、repository を用意する（design/backend/README.md の「repository の用意」）。user だけが使える。
// 途中で失敗したら、同じ要求をもう一度送れば、続きから終わらせられる。
func (b *Backend) CreateRepository(ctx context.Context, c Caller, name, statement, background string) (Written[Repository], error) {
	var out Written[Repository]
	if err := requireUser(c); err != nil {
		return out, err
	}
	if !ids.RepositoryNamePattern.MatchString(name) {
		return out, fail(CodeValidationFailed, "repository の名前は、小文字の英数字とハイフンで、63 文字以下")
	}
	if n := tree.CharCount(statement); n == 0 || n > tree.MaxStatementChars {
		return out, fail(CodeValidationFailed, "根の主文は 1 字以上 %d 字以下", tree.MaxStatementChars)
	}
	ref, err := b.Forge.GitForgeRef(ctx, name)
	if errors.Is(err, ports.ErrNotRegistered) {
		return out, fail(CodeRepositoryNotRegistered, "repository %q は、サーバの設定に登録されていない。管理者が GitHub の repository を作って登録する", name)
	}
	if err != nil {
		return out, err
	}
	root := ids.New(ids.Hypothesis)
	dir := tree.TreeDir + "/" + root
	files := []ports.File{{Path: dir + "/" + tree.HypothesisFile, Content: tree.MarshalHypothesis(tree.HypothesisContent{ID: root, Statement: statement})}}
	if background != "" {
		files = append(files, ports.File{Path: dir + "/" + tree.BackgroundFile, Content: []byte(background)})
	}
	err = b.Forge.SetupRepository(ctx, ref, files, "repository を用意した\n\nLogiforge-Init: 1")
	if errors.Is(err, ports.ErrMainExists) {
		// 送り直しか、2 つの用意が同時に走ったか、別の repository か。main のスナップショットで判定する。
		// アダプタが、止まっていた用意の続きを置いたかもしれないので、今の main の先頭が読む側に出るまで待つ。
		head, err := b.Forge.MainHead(ctx, ref)
		if err != nil {
			return out, err
		}
		if !b.waitLatest(ctx, name, func(l string) bool { return l != "" && b.caughtUp(ctx, name, ref, l, head) }) {
			return out, fail(CodeSnapshotPending, "main がすでにあるが、そのスナップショットがまだない。少し待って送り直す")
		}
		v, err := b.currentMain(ctx, name)
		if err != nil {
			return out, err
		}
		r := v.tree.Root()
		if !initCommit(v.main.Commit) || r == nil || r.Statement != statement || r.Background != background || len(r.Stories) > 0 || len(r.Verdicts) > 0 {
			return out, fail(CodeRepositoryExists, "同じ名前の repository がすでにある")
		}
	} else if err != nil {
		return out, err
	}
	if !b.waitLatest(ctx, name, func(l string) bool { return l != "" }) {
		_, dir := ports.SplitRef(ref)
		out.Value = Repository{Name: name, GitURL: b.Forge.GitURL(ref), Directory: dir}
		out.Pending = "main"
		if head, err := b.Forge.MainHead(ctx, ref); err == nil {
			out.Pending = head
		}
		return out, nil
	}
	repo, err := b.GetRepository(ctx, c, name)
	out.Value = repo
	return out, err
}

// ---- apply request の作成と更新 ----

// NewApplyRequest は、apply request の作成の要求である。
type NewApplyRequest struct {
	// Kind は、change（ふつう）、repair.revert、repair.accept のどれか。空は change（design/backend/broken.md）。
	Kind           string
	Branch         string
	Commit         string
	Description    string
	Session        string
	IdempotencyKey string
}

// checkContext は、差分の検査に要る、ファイルの外の問い合わせを、latest の main のスナップショットで作る。
func (b *Backend) checkContext(ctx context.Context, latest *view) tree.Context {
	return tree.Context{
		Others: []*tree.Tree{latest.tree},
		// 仮説が、祖先のストーリーが inactive になる直前に葉だったか。inactivated.md が加わったコミットの 1 つ前のスナップショットツリーで確かめる。
		WasLeafBeforeInactivation: func(hid string) (bool, error) {
			h := latest.tree.Hypotheses[hid]
			if h == nil {
				return false, nil
			}
			var story *tree.Story
			for s := h.Parent; s != nil; s = s.Parent.Parent {
				if s.Inactivated {
					story = s
					break
				}
			}
			if story == nil {
				return true, nil
			}
			added := 0
			if n := latest.main.SnapshotTree.Nodes[story.ID]; n != nil {
				added = n.InactivatedAt
			}
			if added <= 1 {
				return false, nil
			}
			prev, err := b.Snap.MainAt(ctx, latest.repo, added-1)
			if err != nil {
				return false, err
			}
			before, err := b.mainAt(ctx, latest.repo, prev)
			if err != nil {
				return false, err
			}
			n := before.main.SnapshotTree.Nodes[hid]
			return n != nil && n.Leaf, nil
		},
		// 引用したコミットが main の並びにあり、そのスナップショットツリーに、そのノードがあるか。
		SubtreeExists: func(nodeID, revision string) (bool, error) {
			if n, err := b.mainNumber(ctx, latest, revision); err != nil || n == 0 {
				return false, err
			}
			v, err := b.mainAt(ctx, latest.repo, revision)
			if err != nil {
				return false, err
			}
			return v.main.SnapshotTree.Nodes[nodeID] != nil, nil
		},
	}
}

// checkVersion は、先頭のコミットの差分を、onto の木に重ねて検査する。重ねられなければ conflicts を返す。
func (b *Backend) checkVersion(ctx context.Context, onto, latest *view, br *snapshot.Branch) (tree.Result, []string, error) {
	head, conflicts, err := snapshot.Overlay(onto.files, br.Diff)
	if err != nil || len(conflicts) > 0 {
		return tree.Result{}, conflicts, err
	}
	res, err := tree.CheckChange(onto.files, head, b.checkContext(ctx, latest))
	return res, nil, err
}

func violations(vs []tree.Violation) string {
	var parts []string
	for _, v := range vs {
		parts = append(parts, fmt.Sprintf("%s（%s）：%s", v.Rule, v.Node, v.Detail))
	}
	return strings.Join(parts, "；")
}

// CreateApplyRequest は、push したブランチから apply request を作る（design/backend/apply-request.md の「作成」）。client だけが使える。
func (b *Backend) CreateApplyRequest(ctx context.Context, c Caller, repo string, n NewApplyRequest) (Written[ApplyRequest], error) {
	var out Written[ApplyRequest]
	if err := requireClient(c, n.Session); err != nil {
		return out, err
	}
	if n.Branch == "" || n.Branch == "main" {
		return out, fail(CodeValidationFailed, "branch がないか、main である")
	}
	kind, err := normalizeKind(n.Kind)
	if err != nil {
		return out, err
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return out, err
	}
	// 移した元の repository は、読むことだけができる（design/directory/migrate.md の「元の repository」）。
	if err := b.notMigrated(ctx, repo); err != nil {
		return out, err
	}

	id, err := b.requestID(ids.PrefixApplyRequest, repo, c, n.IdempotencyKey)
	if err != nil {
		return out, err
	}
	// 同じ識別子のものがあれば、それを返す。中身は比べない。
	if ar, err := b.Forge.GetApplyRequest(ctx, ref, id); err == nil {
		v, err := b.GetApplyRequest(ctx, c, repo, ar.ID)
		out.Value = v
		return out, err
	} else if !errors.Is(err, ports.ErrNotFound) {
		return out, err
	}
	// ブランチは、worker を待たずに、作業用の複製から git を読んで検査する（design/backend/apply-request.md の「作成」）。
	br, err := b.Git.ReadBranch(ctx, repo, ref, n.Branch)
	if errors.Is(err, ports.ErrNotFound) {
		return out, fail(CodeValidationFailed, "ブランチ %s がない（push したか確かめる）", n.Branch)
	}
	if err != nil {
		return out, err
	}
	// ディレクトリの repository では、差分はディレクトリの中だけにする（design/directory/README.md の outside_directory）。
	if len(br.Outside) > 0 {
		return out, fail(CodeValidationFailed, "差分の検査に通らない：%s（ディレクトリの外）：%s", tree.RuleOutsideDirectory, strings.Join(br.Outside, "、"))
	}
	head := br.Commit.Revision
	if n.Commit != "" && n.Commit != head {
		return out, fail(CodeValidationFailed, "commit %s が、ブランチの今の先頭のコミット %s と違う", n.Commit, head)
	}
	latest, err := b.currentMain(ctx, repo)
	if err != nil {
		return out, err
	}
	if kind != "" {
		// 直すための apply request は、差分の検査のかわりに、直す条件を確かめる（design/backend/broken.md）。
		broken, err := b.Snap.Broken(ctx, repo)
		if err != nil {
			return out, err
		}
		problem, err := b.repairProblem(ctx, repo, kind, br, latest, broken)
		if err != nil {
			return out, err
		}
		if problem != "" {
			return out, fail(CodeValidationFailed, "%s", problem)
		}
		return b.createForge(ctx, c, repo, ref, id, kind, n, head, nil)
	}
	baseFiles, err := b.baseFiles(ctx, repo, ref, br.Diff.Base)
	if err != nil {
		return out, err
	}
	res, conflicts, err := b.checkVersion(ctx, &view{files: baseFiles}, latest, br)
	if err != nil {
		return out, err
	}
	if len(conflicts) > 0 {
		return out, fail(CodeValidationFailed, "差分を base に重ねられない：%s", strings.Join(conflicts, "、"))
	}
	if !res.OK() {
		return out, fail(CodeValidationFailed, "差分の検査に通らない：%s", violations(res.Rejected))
	}
	if len(res.Operations) == 0 {
		return out, fail(CodeValidationFailed, "差分がない")
	}
	return b.createForge(ctx, c, repo, ref, id, "", n, head, res.Operations)
}

// createForge は、検査を通った apply request を、GitForge に作る。ops は、いま検査した操作（worker の取り込みの前に返すため）。
func (b *Backend) createForge(ctx context.Context, c Caller, repo, ref, id, kind string, n NewApplyRequest, head string, ops []tree.Operation) (Written[ApplyRequest], error) {
	var out Written[ApplyRequest]
	_, err := b.Forge.CreateApplyRequest(ctx, ref, ports.NewApplyRequest{ID: id, Kind: kind, Branch: n.Branch, Description: n.Description, FirstRevision: head, Opener: c.Principal.Login})
	if errors.Is(err, ports.ErrBranchHasOpen) {
		return out, fail(CodeValidationFailed, "ブランチ %s に、open の apply request がすでにある", n.Branch)
	}
	if err != nil {
		return out, err
	}
	// 先頭のコミットの一覧とレビューの材料は、worker のスナップショットから読む。worker は、次の見回りでブランチを取り込む。
	v, err := b.GetApplyRequest(ctx, c, repo, id)
	if err == nil && len(v.Operations) == 0 {
		// worker が、まだブランチのスナップショットを作っていない。操作の一覧は、いま検査した結果から返す。
		for i, op := range ops {
			v.Operations = append(v.Operations, toOperation(i+1, op))
		}
	}
	out.Value = v
	return out, err
}

// baseFiles は、ブランチの base の木のファイルを返す。置き場に木があればそれを使い、なければ作業用の複製から読む。
// worker の取り込みを待たないためである。
func (b *Backend) baseFiles(ctx context.Context, repo, ref, base string) (tree.Files, error) {
	m, err := b.Snap.Main(ctx, repo, base)
	if err == nil {
		return m.SnapshotTree.Files()
	}
	if !errors.Is(err, ports.ErrNotFound) {
		return nil, err
	}
	return b.Git.ReadFiles(ctx, repo, ref, base)
}

// UpdateApplyRequest は、説明欄を書き換える。client なら、どのセッションからでもよい。
func (b *Backend) UpdateApplyRequest(ctx context.Context, c Caller, repo, id, session, description string) (ApplyRequest, error) {
	if err := requireClient(c, session); err != nil {
		return ApplyRequest{}, err
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return ApplyRequest{}, err
	}
	d, err := b.openApplyRequest(ctx, repo, ref, id)
	if err != nil {
		return ApplyRequest{}, err
	}
	if err := b.Forge.UpdateDescription(ctx, ref, d.port.ID, description); err != nil {
		return ApplyRequest{}, err
	}
	return b.GetApplyRequest(ctx, c, repo, id)
}

// openApplyRequest は、open の提案を読む。open でなければ not_open にする。
func (b *Backend) openApplyRequest(ctx context.Context, repo, ref, id string) (*arDetail, error) {
	ar, err := b.Forge.GetApplyRequest(ctx, ref, id)
	if err != nil {
		return nil, err
	}
	d, err := b.applyRequest(ctx, repo, ref, ar, true, nil)
	if err != nil {
		return nil, err
	}
	if d.model.State != string(ports.StateOpen) {
		return nil, fail(CodeNotOpen, "apply request %s は %s である", id, d.model.State)
	}
	return d, nil
}

// CloseApplyRequest は、apply request を close する。すでに閉じていれば、今の状態を返す。
func (b *Backend) CloseApplyRequest(ctx context.Context, c Caller, repo, id, session, reason string) (ApplyRequest, error) {
	if err := requireClient(c, session); err != nil {
		return ApplyRequest{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return ApplyRequest{}, fail(CodeValidationFailed, "reason がない")
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return ApplyRequest{}, err
	}
	if _, err := b.openApplyRequest(ctx, repo, ref, id); err != nil {
		if e := AsError(err); e != nil && e.Code == CodeNotOpen {
			return b.GetApplyRequest(ctx, c, repo, id)
		}
		return ApplyRequest{}, err
	}
	if err := b.Forge.Close(ctx, ref, id, reason, session); err != nil {
		return ApplyRequest{}, err
	}
	return b.GetApplyRequest(ctx, c, repo, id)
}

// ---- 人への依頼 ----

// NewHumanRequest は、人への依頼の起票の要求である。
type NewHumanRequest struct {
	NodeID         string
	Kind           string
	Body           string
	Session        string
	IdempotencyKey string
}

// CreateHumanRequest は、人への依頼を起票する。client だけが使える。対象ノードは、今の main の木にあるものに限る。
func (b *Backend) CreateHumanRequest(ctx context.Context, c Caller, repo string, n NewHumanRequest) (HumanRequest, error) {
	if err := requireClient(c, n.Session); err != nil {
		return HumanRequest{}, err
	}
	if n.Kind != string(ports.HumanRequestPVR) && n.Kind != string(ports.HumanRequestDecision) {
		return HumanRequest{}, fail(CodeValidationFailed, "kind は pvr か decision")
	}
	if strings.TrimSpace(n.Body) == "" {
		return HumanRequest{}, fail(CodeValidationFailed, "body がない")
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return HumanRequest{}, err
	}
	// 移した元の repository は、読むことだけができる（design/directory/migrate.md の「元の repository」）。
	if err := b.notMigrated(ctx, repo); err != nil {
		return HumanRequest{}, err
	}

	id, err := b.requestID(ids.PrefixHumanRequest, repo, c, n.IdempotencyKey)
	if err != nil {
		return HumanRequest{}, err
	}
	if hr, err := b.Forge.GetHumanRequest(ctx, ref, id); err == nil {
		return b.toHumanRequest(repo, hr), nil
	} else if !errors.Is(err, ports.ErrNotFound) {
		return HumanRequest{}, err
	}
	latest, err := b.currentMain(ctx, repo)
	if err != nil {
		return HumanRequest{}, err
	}
	// 対象は、active な仮説で、葉か要再検討であるもの（docs/human-requests/README.md）。
	node := latest.main.SnapshotTree.Nodes[n.NodeID]
	switch {
	case node == nil:
		return HumanRequest{}, fail(CodeValidationFailed, "ノード %s が、今の main の木にない", n.NodeID)
	case node.Kind != snapshot.NodeHypothesis:
		return HumanRequest{}, fail(CodeValidationFailed, "対象は仮説に限る")
	case node.Inactive:
		return HumanRequest{}, fail(CodeValidationFailed, "仮説 %s は inactive である", n.NodeID)
	case !node.Leaf && !node.NeedsReconsideration:
		return HumanRequest{}, fail(CodeValidationFailed, "仮説 %s は、葉でも要再検討でもない", n.NodeID)
	}
	hr, err := b.Forge.CreateHumanRequest(ctx, ref, ports.NewHumanRequest{ID: id, Kind: ports.HumanRequestKind(n.Kind), TargetNode: n.NodeID,
		Body: n.Body, Requester: c.Principal.Login, Session: n.Session})
	if err != nil {
		return HumanRequest{}, err
	}
	return b.toHumanRequest(repo, hr), nil
}

// AnswerFile は、回答の添付である。file は中身、link は URL を持つ。
type AnswerFile struct {
	Kind          string
	Name          string
	ContentBase64 string
	URI           string
}

// AnswerHumanRequest は、人への依頼に回答する。user だけが使える。1 回だけである。
// 送り直しは、回答がすでにあり、本文・回答した利用者・添付が同じなら、成功とする。
// link の添付は、GitForge に置かず、回答の本文の末尾に書く。
func (b *Backend) AnswerHumanRequest(ctx context.Context, c Caller, repo, id, body string, files []AnswerFile) (HumanRequest, error) {
	if err := requireUser(c); err != nil {
		return HumanRequest{}, err
	}
	if strings.TrimSpace(body) == "" {
		return HumanRequest{}, fail(CodeValidationFailed, "body がない")
	}
	var atts []ports.NewAttachment
	var links []string
	for _, f := range files {
		if strings.TrimSpace(f.Name) == "" {
			return HumanRequest{}, fail(CodeValidationFailed, "添付の name がない")
		}
		switch f.Kind {
		case "file":
			data, err := base64.StdEncoding.DecodeString(f.ContentBase64)
			if err != nil {
				return HumanRequest{}, fail(CodeValidationFailed, "添付 %s の content_base64 が正しくない", f.Name)
			}
			if len(data) > tree.MaxArtifactSize {
				return HumanRequest{}, fail(CodeTooLarge, "添付 %s が 10 MiB を超える", f.Name)
			}
			atts = append(atts, ports.NewAttachment{Name: f.Name, Content: data})
		case "link":
			if f.URI == "" {
				return HumanRequest{}, fail(CodeValidationFailed, "link の添付 %s に uri がない", f.Name)
			}
			links = append(links, fmt.Sprintf("- %s：%s", f.Name, f.URI))
		default:
			return HumanRequest{}, fail(CodeValidationFailed, "添付の kind は file か link")
		}
	}
	if len(links) > 0 {
		body = strings.TrimRight(body, "\n") + "\n\n添付（リンク）：\n" + strings.Join(links, "\n")
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return HumanRequest{}, err
	}
	// 移した元の repository は、読むことだけができる（design/directory/migrate.md の「元の repository」）。
	if err := b.notMigrated(ctx, repo); err != nil {
		return HumanRequest{}, err
	}

	err = b.Forge.Answer(ctx, ref, id, body, c.Principal.Login, atts)
	if errors.Is(err, ports.ErrAttachmentsUnsupported) {
		return HumanRequest{}, fail(CodeValidationFailed, "この構成では、回答の添付はリンクだけである（kind: link）")
	}
	if errors.Is(err, ports.ErrAlreadyAnswered) {
		return HumanRequest{}, fail(CodeNotOpen, "人への依頼 %s には、すでに回答がある", id)
	}
	if err != nil {
		return HumanRequest{}, err
	}
	return b.GetHumanRequest(ctx, c, repo, id)
}
