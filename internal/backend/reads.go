package backend

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"

	"github.com/yamambainc/logiforge/internal/ids"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/tree"
)

// ページング（design/api/README.md の「ページング」）。
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// CheckLimit は、limit を確かめる。0 なら既定の値にする。
func CheckLimit(limit int) (int, error) {
	switch {
	case limit == 0:
		return DefaultLimit, nil
	case limit < 1 || limit > MaxLimit:
		return 0, fail(CodeValidationFailed, "limit は 1 から %d まで", MaxLimit)
	}
	return limit, nil
}

// page は、並べた items を、カーソルの次から limit 件に分ける。after は、カーソルの項目の次からかを返す。
func page[T any](items []T, limit int, after func(T) bool, key func(T) string) Page[T] {
	out := Page[T]{Items: []T{}}
	for _, it := range items {
		if !after(it) {
			continue
		}
		if len(out.Items) == limit {
			k := key(out.Items[len(out.Items)-1])
			out.NextCursor = &k
			break
		}
		out.Items = append(out.Items, it)
	}
	return out
}

// numberCursor は、GitForge の連番のカーソルを読む。空なら 0。
func numberCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(cursor)
	if err != nil || n < 0 {
		return 0, fail(CodeValidationFailed, "cursor が正しくない")
	}
	return n, nil
}

// ListRepositories は、主体が読める repository を、名前の順に返す。
func (b *Backend) ListRepositories(ctx context.Context, c Caller, cursor string, limit int) (Page[Repository], error) {
	limit, err := CheckLimit(limit)
	if err != nil {
		return Page[Repository]{}, err
	}
	names, err := b.Snap.Repositories(ctx)
	if err != nil {
		return Page[Repository]{}, err
	}
	slices.Sort(names)
	var out []Repository
	for _, name := range names {
		if name <= cursor {
			continue
		}
		if len(out) == limit+1 {
			break
		}
		r, err := b.GetRepository(ctx, c, name)
		if AsError(err) != nil && AsError(err).Code == CodeNotFound {
			continue
		}
		if err != nil {
			return Page[Repository]{}, err
		}
		out = append(out, r)
	}
	return page(out, limit, func(Repository) bool { return true }, func(r Repository) string { return r.Name }), nil
}

// GetRepository は、repository を返す。
func (b *Backend) GetRepository(ctx context.Context, c Caller, repo string) (Repository, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return Repository{}, err
	}
	v, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return Repository{}, err
	}
	_, dir := ports.SplitRef(ref)
	r := Repository{Name: repo, GitURL: b.Forge.GitURL(ref), Directory: dir}
	if br, err := b.Snap.Broken(ctx, repo); err != nil {
		return Repository{}, err
	} else if br != nil {
		r.Broken = &BrokenState{FirstCommit: br.First, LastGoodCommit: br.LastGood, Reason: br.Reason}
	}
	if r.MigratedFrom, err = b.migratedFrom(ctx, repo); err != nil {
		return Repository{}, err
	}
	if to, err := b.Snap.MigratedTo(ctx, repo); err != nil {
		return Repository{}, err
	} else if to != "" {
		r.MigratedTo = &to
	}
	if root := v.tree.Root(); root != nil {
		r.RootHypothesisID = root.ID
		st := root.Statement
		r.RootStatement = &st
		r.RootValue = v.main.SnapshotTree.Nodes[root.ID].Value
	}
	return r, nil
}

// GetTree は、木の概要を返す。
func (b *Backend) GetTree(ctx context.Context, c Caller, repo string, at At) (Tree, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return Tree{}, err
	}
	v, err := b.readAt(ctx, repo, ref, at)
	if err != nil {
		return Tree{}, err
	}
	root := v.tree.Root()
	if root == nil {
		return Tree{}, fail(CodeNotFound, "根の仮説がない")
	}
	return Tree{RootHypothesisID: root.ID, RootValue: v.main.SnapshotTree.Nodes[root.ID].Value, Commit: v.revision(), Warnings: v.warnings()}, nil
}

// ListNodes は、木か部分木のノードを返す。depth が負なら、すべての段。
func (b *Backend) ListNodes(ctx context.Context, c Caller, repo string, at At, root string, depth int) (NodeList, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return NodeList{}, err
	}
	v, err := b.readAt(ctx, repo, ref, at)
	if err != nil {
		return NodeList{}, err
	}
	open, err := b.readOpen(ctx, repo, ref, v)
	if err != nil {
		return NodeList{}, err
	}
	if root == "" {
		r := v.tree.Root()
		if r == nil {
			return NodeList{}, fail(CodeNotFound, "根の仮説がない")
		}
		root = r.ID
	}
	nodes := v.subtree(root, depth, open.claims)
	if len(nodes) == 0 {
		return NodeList{}, fail(CodeNotFound, "ノード %s がない", root)
	}
	return NodeList{Commit: v.revision(), Nodes: nodes}, nil
}

// GetNode は、ノードの詳細を返す。
func (b *Backend) GetNode(ctx context.Context, c Caller, repo, id string, at At) (NodeDetail, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return NodeDetail{}, err
	}
	v, err := b.readAt(ctx, repo, ref, at)
	if err != nil {
		return NodeDetail{}, err
	}
	open, err := b.readOpen(ctx, repo, ref, v)
	if err != nil {
		return NodeDetail{}, err
	}
	n := v.node(id, open.claims)
	if n == nil {
		return NodeDetail{}, fail(CodeNotFound, "ノード %s がない", id)
	}
	d := NodeDetail{Commit: v.revision(), Node: n, Children: []Node{}, CitedBy: []string{}}
	if h, ok := v.tree.Hypotheses[id]; ok {
		for _, s := range h.Stories {
			d.Children = append(d.Children, v.story(s, open.claims))
		}
		if valid := h.ValidVerdicts(); len(valid) == 1 {
			vd, err := b.verdict(ctx, v, valid[0])
			if err != nil {
				return NodeDetail{}, err
			}
			d.Verdict = &vd
		}
	} else if s, ok := v.tree.Stories[id]; ok {
		for _, h := range s.Hypotheses {
			d.Children = append(d.Children, v.hypothesis(h, open.claims))
		}
	}
	for _, vid := range sortedKeys(v.tree.Verdicts) {
		for _, ci := range v.tree.Verdicts[vid].Citations {
			if ci.TargetKind == "subtree" && ci.NodeID == id {
				d.CitedBy = append(d.CitedBy, vid)
				break
			}
		}
	}
	return d, nil
}

// verdict は、検証結果の応答を作る。加わったコミットと、差し替えられたコミットを、ノードの変更の一覧から探す。
func (b *Backend) verdict(ctx context.Context, v *view, tv *tree.Verdict) (Verdict, error) {
	out := Verdict{ID: tv.ID, HypothesisID: tv.Hypothesis.ID, Value: tv.Value, ReportBody: tv.Report,
		Artifacts: tv.Artifacts, Citations: tv.Citations, Supersedes: tv.Supersedes}
	if out.Artifacts == nil {
		out.Artifacts = []tree.ArtifactRef{}
	}
	if out.Citations == nil {
		out.Citations = []tree.Citation{}
	}
	rev, ar, err := b.addedIn(ctx, v, tv.Hypothesis.ID, tv.Path)
	if err != nil {
		return Verdict{}, err
	}
	out.SubmittedCommit, out.ApplyRequestID = rev, ar
	for _, other := range tv.Hypothesis.Verdicts {
		if slices.Contains(other.Supersedes, tv.ID) {
			r, _, err := b.addedIn(ctx, v, other.Hypothesis.ID, other.Path)
			if err != nil {
				return Verdict{}, err
			}
			out.SupersededCommit = &r
			break
		}
	}
	return out, nil
}

// addedIn は、検証結果のファイル p が加わったコミットと、その反映の apply request を返す。
// worker がスナップショットツリーのファイルに書いた値を使うので、置き場を読まない（design/backend/snapshot.md の「ノードを変えたコミット」）。
func (b *Backend) addedIn(_ context.Context, v *view, node, p string) (string, string, error) {
	n := v.main.SnapshotTree.Nodes[node]
	if n == nil {
		return "", "", nil
	}
	for _, f := range n.Files {
		if f.Path == p && f.Added != nil {
			return f.Added.Revision, f.Added.ApplyRequest, nil
		}
	}
	return "", "", nil
}

// GetVerdict は、検証結果を返す。差し替えられた古い検証結果も読める。
func (b *Backend) GetVerdict(ctx context.Context, c Caller, repo, id string) (Verdict, error) {
	if _, err := b.access(ctx, c, repo); err != nil {
		return Verdict{}, err
	}
	v, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return Verdict{}, err
	}
	tv, ok := v.tree.Verdicts[id]
	if !ok {
		return Verdict{}, fail(CodeNotFound, "検証結果 %s がない", id)
	}
	return b.verdict(ctx, v, tv)
}

// GetNodeHistory は、ノードを変えた main のコミットを、新しい順に返す。カーソルは、次に読むコミットの番号である。
func (b *Backend) GetNodeHistory(ctx context.Context, c Caller, repo, id, cursor string, limit int) (Page[NodeHistoryEntry], error) {
	limit, err := CheckLimit(limit)
	if err != nil {
		return Page[NodeHistoryEntry]{}, err
	}
	from, err := numberCursor(cursor)
	if err != nil {
		return Page[NodeHistoryEntry]{}, err
	}
	if _, err := b.access(ctx, c, repo); err != nil {
		return Page[NodeHistoryEntry]{}, err
	}
	v, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return Page[NodeHistoryEntry]{}, err
	}
	n := v.main.SnapshotTree.Nodes[id]
	if n == nil {
		return Page[NodeHistoryEntry]{}, fail(CodeNotFound, "ノード %s がない", id)
	}
	// 始めのコミット。カーソルがあれば、その番号のコミットを main の並びの索引で引く。
	start := n.LastChanged
	if from > 0 && from > start.Number {
		return Page[NodeHistoryEntry]{}, fail(CodeValidationFailed, "カーソルが正しくない")
	}
	if from > 0 && from < start.Number {
		sha, err := b.Snap.MainAt(ctx, repo, from)
		if errors.Is(err, ports.ErrNotFound) {
			return Page[NodeHistoryEntry]{}, fail(CodeValidationFailed, "カーソルが正しくない")
		}
		if err != nil {
			return Page[NodeHistoryEntry]{}, err
		}
		start = snapshot.Rev{Number: from, Revision: sha}
		// カーソルは、そのノードを変えたコミットでなければならない。
		c, err := b.Snap.Commit(ctx, repo, sha)
		if err != nil {
			return Page[NodeHistoryEntry]{}, err
		}
		if !slices.ContainsFunc(c.Changes, func(ch snapshot.NodeChange) bool { return ch.Node == id }) {
			return Page[NodeHistoryEntry]{}, fail(CodeValidationFailed, "カーソルが正しくない")
		}
	}
	out := Page[NodeHistoryEntry]{Items: []NodeHistoryEntry{}}
	err = b.walkNodeChanges(ctx, repo, id, start, func(r snapshot.Rev, cm snapshot.Commit, _ snapshot.NodeChange) bool {
		if len(out.Items) == limit {
			next := strconv.Itoa(r.Number)
			out.NextCursor = &next
			return false
		}
		e := NodeHistoryEntry{Commit: r.Revision, CommittedAt: cm.CommittedAt, SessionIDs: []string{}}
		if ar := snapshot.ApplyRequestOf(cm); ar != "" {
			e.ApplyRequestID, e.Reflected = &ar, true
		}
		for _, t := range cm.Trailers {
			if t.Key == trailerSession {
				e.SessionIDs = append(e.SessionIDs, t.Value)
			}
		}
		out.Items = append(out.Items, e)
		return true
	})
	if err != nil {
		return Page[NodeHistoryEntry]{}, err
	}
	return out, nil
}

// ListApplyRequests は、提案を作った順に返す。カーソルは、GitForge の連番（pull request の番号）である。
func (b *Backend) ListApplyRequests(ctx context.Context, c Caller, repo, state, cursor string, limit int) (Page[ApplyRequest], error) {
	limit, err := CheckLimit(limit)
	if err != nil {
		return Page[ApplyRequest]{}, err
	}
	after, err := numberCursor(cursor)
	if err != nil {
		return Page[ApplyRequest]{}, err
	}
	if state != "" && state != "open" && state != "merged" && state != "closed" {
		return Page[ApplyRequest]{}, fail(CodeValidationFailed, "state が正しくない")
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return Page[ApplyRequest]{}, err
	}
	all, err := b.Forge.ListApplyRequests(ctx, ref)
	if err != nil {
		return Page[ApplyRequest]{}, err
	}
	latest, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return Page[ApplyRequest]{}, err
	}
	out := Page[ApplyRequest]{Items: []ApplyRequest{}}
	last := after
	for _, ar := range all {
		if ar.Number <= after {
			continue
		}
		d, err := b.applyRequest(ctx, repo, ref, ar, false, latest)
		if err != nil {
			return Page[ApplyRequest]{}, err
		}
		if state != "" && d.model.State != state {
			continue
		}
		if len(out.Items) == limit {
			next := strconv.Itoa(last)
			out.NextCursor = &next
			break
		}
		out.Items = append(out.Items, d.model)
		last = ar.Number
	}
	return out, nil
}

// GetApplyRequest は、提案の詳細を返す。
func (b *Backend) GetApplyRequest(ctx context.Context, c Caller, repo, id string) (ApplyRequest, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return ApplyRequest{}, err
	}
	ar, err := b.Forge.GetApplyRequest(ctx, ref, id)
	if err != nil {
		return ApplyRequest{}, err
	}
	latest, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return ApplyRequest{}, err
	}
	d, err := b.applyRequest(ctx, repo, ref, ar, true, latest)
	if err != nil {
		return ApplyRequest{}, err
	}
	return d.model, nil
}

// GetArtifact は、artifact の中身を返す。revision を指定すれば、そのコミット（main のコミットか、apply request の先頭のコミット）にあるものだけを返す。
func (b *Backend) GetArtifact(ctx context.Context, c Caller, repo, sha, revision string) ([]byte, error) {
	if !ids.Sha256Pattern.MatchString(sha) {
		return nil, fail(CodeValidationFailed, "sha256 の形が正しくない")
	}
	if _, err := b.access(ctx, c, repo); err != nil {
		return nil, err
	}
	if !b.hasArtifact(ctx, repo, sha, revision) {
		return nil, fail(CodeNotFound, "artifact %s が、そのコミットにない", sha)
	}
	return b.Snap.Artifact(ctx, repo, sha)
}

func (b *Backend) hasArtifact(ctx context.Context, repo, sha, revision string) bool {
	inMain := func(rev string) bool {
		v, err := b.mainAt(ctx, repo, rev)
		if err != nil {
			return false
		}
		for _, a := range v.main.SnapshotTree.Artifacts {
			if a.SHA256 == sha {
				return true
			}
		}
		return false
	}
	if revision == "" {
		return inMain("")
	}
	if inMain(revision) {
		return true
	}
	br, err := b.Snap.Branch(ctx, repo, revision)
	if err != nil {
		return false
	}
	for _, e := range br.Diff.Entries {
		if e.File != nil && e.File.SHA256 == sha {
			return true
		}
	}
	return inMain(br.Diff.Base)
}

// toHumanRequest は、人への依頼の応答を作る。
func (b *Backend) toHumanRequest(repo string, hr ports.HumanRequest) HumanRequest {
	out := HumanRequest{ID: hr.ID, NodeID: hr.TargetNode, Kind: string(hr.Kind), Body: hr.Body, State: "open"}
	if a := hr.Answer; a != nil {
		out.State = "answered"
		ans := &Answer{Body: a.Body, AnsweredAt: a.CreatedAt, Attachments: []tree.ArtifactRef{}}
		for _, at := range a.Attachments {
			ans.Attachments = append(ans.Attachments, tree.ArtifactRef{Kind: "file", Name: at.Name, SHA256: at.SHA256,
				URI: "/v1/repositories/" + repo + "/human-requests/" + hr.ID + "/attachments/" + at.SHA256})
		}
		out.Answer = ans
	}
	return out
}

// ListHumanRequests は、人への依頼を作った順に返す。
func (b *Backend) ListHumanRequests(ctx context.Context, c Caller, repo, kind, state, nodeID, cursor string, limit int) (Page[HumanRequest], error) {
	limit, err := CheckLimit(limit)
	if err != nil {
		return Page[HumanRequest]{}, err
	}
	after, err := numberCursor(cursor)
	if err != nil {
		return Page[HumanRequest]{}, err
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return Page[HumanRequest]{}, err
	}
	hrs, err := b.Forge.ListHumanRequests(ctx, ref)
	if err != nil {
		return Page[HumanRequest]{}, err
	}
	var filtered []ports.HumanRequest
	for _, hr := range hrs {
		h := b.toHumanRequest(repo, hr)
		if (kind == "" || h.Kind == kind) && (state == "" || h.State == state) && (nodeID == "" || h.NodeID == nodeID) {
			filtered = append(filtered, hr)
		}
	}
	p := page(filtered, limit, func(hr ports.HumanRequest) bool { return hr.Number > after },
		func(hr ports.HumanRequest) string { return strconv.Itoa(hr.Number) })
	out := Page[HumanRequest]{Items: []HumanRequest{}, NextCursor: p.NextCursor}
	for _, hr := range p.Items {
		out.Items = append(out.Items, b.toHumanRequest(repo, hr))
	}
	return out, nil
}

// GetHumanRequest は、人への依頼と、その回答を返す。
func (b *Backend) GetHumanRequest(ctx context.Context, c Caller, repo, id string) (HumanRequest, error) {
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return HumanRequest{}, err
	}
	hr, err := b.Forge.GetHumanRequest(ctx, ref, id)
	if err != nil {
		return HumanRequest{}, err
	}
	return b.toHumanRequest(repo, hr), nil
}

// GetAnswerAttachment は、回答の添付の中身を返す。
func (b *Backend) GetAnswerAttachment(ctx context.Context, c Caller, repo, id, sha string) ([]byte, error) {
	if !ids.Sha256Pattern.MatchString(sha) {
		return nil, fail(CodeValidationFailed, "sha256 の形が正しくない")
	}
	ref, err := b.access(ctx, c, repo)
	if err != nil {
		return nil, err
	}
	data, err := b.Forge.ReadAttachment(ctx, ref, id, sha)
	if errors.Is(err, ports.ErrAttachmentTamper) {
		return nil, fail(CodeNotFound, "添付の中身が、記録の sha256 と合わない")
	}
	return data, err
}

// ListCommits は、worker が取り込んだ main のコミットを、新しい順に返す（design/webui/screens.md の「足す API」）。
// カーソルは、次に読むコミットの number である。main の並びの索引とコミットの記録から引く。
func (b *Backend) ListCommits(ctx context.Context, c Caller, repo, cursor string, limit int) (Page[MainCommit], error) {
	limit, err := CheckLimit(limit)
	if err != nil {
		return Page[MainCommit]{}, err
	}
	from, err := numberCursor(cursor)
	if err != nil {
		return Page[MainCommit]{}, err
	}
	if _, err := b.access(ctx, c, repo); err != nil {
		return Page[MainCommit]{}, err
	}
	latest, err := b.mainAt(ctx, repo, "")
	if err != nil {
		return Page[MainCommit]{}, err
	}
	n := latest.main.Commit.Number
	if from > n {
		return Page[MainCommit]{}, fail(CodeValidationFailed, "カーソルが正しくない")
	}
	if from > 0 {
		n = from
	}
	count := min(limit, n)
	out := Page[MainCommit]{Items: make([]MainCommit, count)}
	if n > count {
		next := strconv.Itoa(n - count)
		out.NextCursor = &next
	}
	// 置き場を、listCommitsParallel 本ずつ並べて読む
	errs := make([]error, count)
	sem := make(chan struct{}, listCommitsParallel)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		sem <- struct{}{}
		go func(i, num int) {
			defer func() { <-sem; wg.Done() }()
			out.Items[i], errs[i] = b.mainCommit(ctx, repo, num)
		}(i, n-i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return Page[MainCommit]{}, err
		}
	}
	return out, nil
}

// listCommitsParallel は、listCommits で並べて読む数である。
const listCommitsParallel = 12

// mainCommit は、番号の main のコミットを、main の並びの索引とコミットの記録から引く。どちらかがなければ、worker が書いている途中なので snapshot_pending。
func (b *Backend) mainCommit(ctx context.Context, repo string, num int) (MainCommit, error) {
	sha, err := b.Snap.MainAt(ctx, repo, num)
	if err == nil {
		var cm snapshot.Commit
		if cm, err = b.Snap.Commit(ctx, repo, sha); err == nil {
			e := MainCommit{Number: num, Commit: sha, CommittedAt: cm.CommittedAt}
			if ar := snapshot.ApplyRequestOf(cm); ar != "" {
				e.ApplyRequestID = &ar
			}
			return e, nil
		}
	}
	if errors.Is(err, ports.ErrNotFound) {
		return MainCommit{}, fail(CodeSnapshotPending, "main のコミット %d の記録が、まだない。少し待って送り直す", num)
	}
	return MainCommit{}, err
}
