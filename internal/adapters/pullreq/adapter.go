package pullreq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/yamambainc/logiforge/internal/ports"
)

// Repos は、repository の列挙と用意である。GitForge ごとに違うので、それぞれのアダプタが持つ。
type Repos interface {
	ListRepositories(ctx context.Context) ([]ports.Repository, error)
	GitForgeRef(ctx context.Context, name string) (string, error)
	GitURL(gitForgeRef string) string
	MainHead(ctx context.Context, gitForgeRef string) (string, error)
	SetupRepository(ctx context.Context, gitForgeRef string, files []ports.File, message string) error
	Archived(ctx context.Context, gitForgeRef string) (bool, error)
	// Prepared は、用意のコミットがもうあるか（git リポジトリ全体なら main、ディレクトリならそのディレクトリ）を、主体のトークンで読む。
	// 設定の画面で登録したあと、最初の問いを入れる段が要るかを決めるのに使う（docs/webui/settings.md）。
	Prepared(ctx context.Context, gitForgeRef string) (bool, error)
}

// Adapter は、API と鍵から、GitForge のポートを実装する。repo の引数は gitforge_ref である。
// GitForge には、ctx の主体のトークン（ports.WithToken）で読み書きする。
type Adapter struct {
	Repos
	API  API
	Keys Keyring
	// Log は、worker の記録の控えである（design/backend/verify/records.md）。nil なら読まない。
	// 読む記録は、GitForge のコメントと控えを合わせたものである。GitForge から消された記録も、控えにあれば数える。
	Log RecordLog
}

// RecordLog は、worker が見た記録のコメントの控えを読む。repoID は、GitForge の repository の識別子（API.RepoID）である。
type RecordLog interface {
	Comments(ctx context.Context, repoID string, number int) ([]Comment, error)
}

var _ ports.GitForge = (*Adapter)(nil)

// titlePrefix は、pull request と issue のタイトルの接頭辞である。タイトルは、探すための手がかりにだけ使い、記録としては読まない。
const titlePrefix = "[Logiforge] "

type parsed struct {
	Record
	Comment Comment
}

// records は、コメントのうち、その場所に置かれた、署名の合う記録を返す。だれが書いたかは見ない。
// 署名が合わないもの（編集された、ほかの場所から写された、など）は数えず、警告にする。
func (a *Adapter) records(repoID string, comments []Comment) (recs []parsed, warnings []string) {
	return Records(a.Keys, repoID, comments)
}

// Records は、コメントのうち、その場所に置かれた、署名の合う記録を、コメントの id の順に返す。worker も使う。
func Records(keys Keyring, repoID string, comments []Comment) (recs []parsed, warnings []string) {
	comments = slices.Clone(comments)
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	for _, c := range comments {
		r, err := keys.Parse(c.Body, Scope{RepoID: repoID, Number: c.IssueNumber})
		switch {
		case errors.Is(err, ErrNoRecord):
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("コメント %d：%v", c.ID, err))
		default:
			recs = append(recs, parsed{Record: r, Comment: c})
		}
	}
	return recs, warnings
}

func (a *Adapter) comment(ctx context.Context, repo string, number int, r Record) (Comment, error) {
	repoID, err := a.API.RepoID(ctx, repo)
	if err != nil {
		return Comment{}, err
	}
	text, err := a.Keys.Format(r, Scope{RepoID: repoID, Number: number})
	if err != nil {
		return Comment{}, err
	}
	return a.API.CreateComment(ctx, repo, number, text)
}

// recordsIn は、records と同じく読む。repository の識別子を GitForge に問い合わせる。
func (a *Adapter) recordsIn(ctx context.Context, repo string, comments []Comment) ([]parsed, []string, error) {
	repoID, err := a.API.RepoID(ctx, repo)
	if err != nil {
		return nil, nil, err
	}
	recs, warnings := a.records(repoID, comments)
	return recs, warnings, nil
}

func byIssue(comments []Comment) map[int][]Comment {
	m := map[int][]Comment{}
	for _, c := range comments {
		m[c.IssueNumber] = append(m[c.IssueNumber], c)
	}
	return m
}

// withRepository は、ディレクトリの repository の記録に、trailer Logiforge-Repository:（ディレクトリのパス）を足す。署名に含まれる。
// git リポジトリ全体の repository では、足さない（design/directory/README.md の「apply request とみなす PR」）。
func withRepository(ts []Trailer, ref string) []Trailer {
	if _, dir := ports.SplitRef(ref); dir != "" {
		ts = append(ts, Trailer{KeyRepository, dir})
	}
	return ts
}

// forRepository は、記録のうち、apply_request と human_request の記録で、Logiforge-Repository: が ref のディレクトリと違うもの
// （ほかの repository の記録）を除く。1 つの git リポジトリに、repository がいくつもあるからである。
// 除いた記録の提案や依頼は、ほかの記録があっても、この repository のものにならない。
func forRepository(recs []parsed, ref string) []parsed {
	_, dir := ports.SplitRef(ref)
	out := recs[:0:0]
	for _, r := range recs {
		switch r.Get(KeyRecord) {
		case RecordApplyRequest, RecordHumanRequest:
			if r.Get(KeyRepository) != dir {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// ---- 提案 ----

// applyRequest は、pull request と、その記録から提案を組み立てる。
// 最初の apply_request の記録が、提案の識別子を決める。ほかの記録は、その識別子を持つものだけを数える。
func applyRequest(p Pull, recs []parsed, warnings []string) (ports.ApplyRequest, bool) {
	ar := ports.ApplyRequest{Number: p.Number, Branch: p.Head, Target: p.Base, Description: p.Body, HeadRevision: p.HeadSHA, CreatedAt: p.CreatedAt, Warnings: warnings}
	found, closed := false, false
	for _, r := range recs {
		kind := r.Get(KeyRecord)
		if kind == RecordApplyRequest {
			if found {
				ar.Warnings = append(ar.Warnings, fmt.Sprintf("コメント %d：apply_request の記録が重なっている", r.Comment.ID))
				continue
			}
			found = true
			ar.ID, ar.FirstRevision, ar.Opener, ar.Kind = r.Get(KeyApplyRequest), r.Get(KeyFirstRevision), r.Get(KeyPrincipal), r.Get(KeyApplyRequestKind)
			continue
		}
		if !found || r.Get(KeyApplyRequest) != ar.ID {
			ar.Warnings = append(ar.Warnings, fmt.Sprintf("コメント %d：提案の識別子が合わない記録", r.Comment.ID))
			continue
		}
		switch kind {
		case RecordClose:
			if !closed {
				closed = true
				ar.CloseReason, ar.CloseSession = r.Body, r.Get(KeySession)
			}
		case RecordMergeFailure:
			ar.MergeFailures = append(ar.MergeFailures, ports.MergeFailure{Revision: r.Get(KeyRevision), Reason: r.Get(KeyMergeFailure), Detail: r.Body})
		}
	}
	switch {
	case p.Merged:
		ar.State, ar.MergedRevision = ports.StateMerged, p.MergeCommit
	case closed || p.State == "closed":
		ar.State = ports.StateClosed
	default:
		ar.State = ports.StateOpen
	}
	return ar, found
}

func (a *Adapter) readPull(ctx context.Context, repo string, p Pull) (ports.ApplyRequest, bool, error) {
	comments, err := a.withLog(ctx, repo, p.Number)
	if err != nil {
		return ports.ApplyRequest{}, false, err
	}
	recs, warnings, err := a.recordsIn(ctx, repo, comments)
	if err != nil {
		return ports.ApplyRequest{}, false, err
	}
	ar, ok := applyRequest(p, forRepository(recs, repo), warnings)
	return ar, ok, nil
}

func (a *Adapter) CreateApplyRequest(ctx context.Context, repo string, n ports.NewApplyRequest) (ports.ApplyRequest, error) {
	pulls, err := a.API.ListPulls(ctx, repo)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	var pull *Pull
	for i, p := range pulls {
		if p.State != "open" || p.Merged || p.Head != n.Branch {
			continue
		}
		ar, ok, err := a.readPull(ctx, repo, p)
		if err != nil {
			return ports.ApplyRequest{}, err
		}
		if ok {
			if ar.ID == n.ID {
				return ar, nil
			}
			return ports.ApplyRequest{}, ports.ErrBranchHasOpen
		}
		// 作ったあと、記録を書く前に止まったもの。新しく作らずに、記録を書く。
		pull = &pulls[i]
	}
	adopted := pull != nil
	if !adopted {
		p, err := a.API.CreatePull(ctx, repo, n.Branch, titlePrefix+n.ID, n.Description)
		if err != nil {
			return ports.ApplyRequest{}, err
		}
		pull = &p
	}
	rec := Record{Trailers: []Trailer{{KeyRecord, RecordApplyRequest}, {KeyApplyRequest, n.ID}, {KeyFirstRevision, n.FirstRevision}, {KeyPrincipal, n.Opener}}}
	rec.Trailers = withRepository(rec.Trailers, repo)
	if n.Kind != "" {
		rec.Trailers = append(rec.Trailers, Trailer{KeyApplyRequestKind, n.Kind})
	}
	if _, err := a.comment(ctx, repo, pull.Number, rec); err != nil {
		return ports.ApplyRequest{}, err
	}
	// 書いたあとに読み直す。同じ pull request を、別の要求が同時に引き取ったなら、最初の記録が勝つ。
	ar, ok, err := a.readPull(ctx, repo, *pull)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	if !ok || ar.ID != n.ID {
		return ports.ApplyRequest{}, ports.ErrBranchHasOpen
	}
	return ar, nil
}

// findPull は、識別子の提案を探す。タイトルを手がかりに、その pull request のコメントだけを読む。
// 見つからなければ、repository のコメントの一覧から探す。
func (a *Adapter) findPull(ctx context.Context, repo, id string) (ports.ApplyRequest, error) {
	pulls, err := a.API.ListPulls(ctx, repo)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	for _, p := range pulls {
		if strings.Contains(p.Title, id) {
			ar, ok, err := a.readPull(ctx, repo, p)
			if err != nil {
				return ports.ApplyRequest{}, err
			}
			if ok && ar.ID == id {
				return ar, nil
			}
		}
	}
	all, err := a.listApplyRequests(ctx, repo, pulls)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	for _, ar := range all {
		if ar.ID == id {
			return ar, nil
		}
	}
	return ports.ApplyRequest{}, ports.ErrNotFound
}

func (a *Adapter) MergedApplyRequest(ctx context.Context, repo, sha string) (ports.ApplyRequest, error) {
	p, ok, err := a.API.MergedPull(ctx, repo, sha)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	if !ok {
		return ports.ApplyRequest{}, ports.ErrNotFound
	}
	ar, found, err := a.readPull(ctx, repo, p)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	if !found {
		return ports.ApplyRequest{Number: p.Number, Branch: p.Head, Target: p.Base, HeadRevision: p.HeadSHA, State: ports.StateMerged, MergedRevision: p.MergeCommit}, nil
	}
	return ar, nil
}

func (a *Adapter) GetApplyRequest(ctx context.Context, repo, id string) (ports.ApplyRequest, error) {
	return a.findPull(ctx, repo, id)
}

func (a *Adapter) ListApplyRequests(ctx context.Context, repo string) ([]ports.ApplyRequest, error) {
	pulls, err := a.API.ListPulls(ctx, repo)
	if err != nil {
		return nil, err
	}
	return a.listApplyRequests(ctx, repo, pulls)
}

func (a *Adapter) listApplyRequests(ctx context.Context, repo string, pulls []Pull) ([]ports.ApplyRequest, error) {
	comments, err := a.API.ListRepoComments(ctx, repo)
	if err != nil {
		return nil, err
	}
	repoID, err := a.API.RepoID(ctx, repo)
	if err != nil {
		return nil, err
	}
	by := byIssue(comments)
	var out []ports.ApplyRequest
	seen := map[string]bool{}
	for _, p := range pulls {
		recs, warnings := a.records(repoID, by[p.Number])
		ar, ok := applyRequest(p, forRepository(recs, repo), warnings)
		if !ok || seen[ar.ID] {
			continue
		}
		seen[ar.ID] = true
		out = append(out, ar)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (a *Adapter) UpdateDescription(ctx context.Context, repo, id, description string) error {
	ar, err := a.findPull(ctx, repo, id)
	if err != nil {
		return err
	}
	return a.API.EditPullBody(ctx, repo, ar.Number, description)
}

func (a *Adapter) Merge(ctx context.Context, repo, id, headSHA, title, message string) (string, error) {
	ar, err := a.findPull(ctx, repo, id)
	if err != nil {
		return "", err
	}
	return a.API.MergePull(ctx, repo, ar.Number, headSHA, title, message)
}

func (a *Adapter) Close(ctx context.Context, repo, id, reason, session string) error {
	ar, err := a.findPull(ctx, repo, id)
	if err != nil {
		return err
	}
	if ar.State != ports.StateOpen {
		return nil
	}
	// 記録を先に書く。close の前に止まっても、記録があれば closed として扱う。
	rec := Record{Body: reason, Trailers: []Trailer{{KeyRecord, RecordClose}, {KeyApplyRequest, id}, {KeySession, session}}}
	if _, err := a.comment(ctx, repo, ar.Number, rec); err != nil {
		return err
	}
	return a.API.ClosePull(ctx, repo, ar.Number)
}

func (a *Adapter) RecordMergeFailure(ctx context.Context, repo, id string, f ports.MergeFailure) error {
	ar, err := a.findPull(ctx, repo, id)
	if err != nil {
		return err
	}
	_, err = a.comment(ctx, repo, ar.Number, Record{Body: f.Detail, Trailers: []Trailer{{KeyRecord, RecordMergeFailure}, {KeyApplyRequest, id}, {KeyMergeFailure, f.Reason}, {KeyRevision, f.Revision}}})
	return err
}

// ---- レビュー ----

// noPrevious は、最初のレビューの Logiforge-Previous-Review: の値である。
const noPrevious = "-"

// AddReview は、レビューを、pull request の記録のコメントとして書く（design/adapters/records.md の「レビューの書き方」）。
// 直前のレビューの識別子を Logiforge-Previous-Review: に書き、レビューを鎖にする。
func (a *Adapter) AddReview(ctx context.Context, repo, applyRequestID string, r ports.Review) error {
	ar, err := a.findPull(ctx, repo, applyRequestID)
	if err != nil {
		return err
	}
	got, err := a.ListReviews(ctx, repo, applyRequestID)
	if err != nil {
		return err
	}
	if got.ChainBroken {
		return fmt.Errorf("pullreq: レビューの鎖が切れているので、レビューを足さない：%v", got.Warnings)
	}
	previous := noPrevious
	if n := len(got.Items); n > 0 {
		previous = got.Items[n-1].ID
	}
	_, err = a.comment(ctx, repo, ar.Number, Record{Body: r.Body, Trailers: []Trailer{
		{KeyRecord, RecordReview}, {KeyApplyRequest, applyRequestID}, {KeyReviewID, r.ID}, {KeyPreviousReview, previous},
		{KeyReviewKind, string(r.Kind)}, {KeyReviewRevision, r.Revision}, {KeyPrincipal, r.Reviewer}, {KeySession, r.Session},
		{KeyPromptRevision, r.PromptRevision},
	}})
	return err
}

func (a *Adapter) ListReviews(ctx context.Context, repo, applyRequestID string) (ports.Reviews, error) {
	ar, err := a.findPull(ctx, repo, applyRequestID)
	if err != nil {
		return ports.Reviews{}, err
	}
	repoID, err := a.API.RepoID(ctx, repo)
	if err != nil {
		return ports.Reviews{}, err
	}
	comments, err := a.withLog(ctx, repo, ar.Number)
	if err != nil {
		return ports.Reviews{}, err
	}
	return Reviews(a.Keys, Scope{RepoID: repoID, Number: ar.Number}, applyRequestID, comments), nil
}

// withLog は、pull request か issue のコメントに、worker の控えのコメントを合わせて返す（design/backend/verify/records.md）。
// 同じコメントの id が両方にあれば、控えのもの（worker が最初に見た中身）を使う。GitForge から消されたものも、控えにあれば返す。
func (a *Adapter) withLog(ctx context.Context, repo string, number int) ([]Comment, error) {
	cur, err := a.API.ListComments(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if a.Log == nil {
		return cur, nil
	}
	repoID, err := a.API.RepoID(ctx, repo)
	if err != nil {
		return nil, err
	}
	logged, err := a.Log.Comments(ctx, repoID, number)
	if err != nil {
		return nil, err
	}
	return MergeComments(logged, cur), nil
}

// MergeComments は、控えのコメントと今のコメントを合わせる。同じ id は、本文だけ控えのもの（worker が最初に見た中身）を使い、
// 添付などの GitForge の側の情報は、今のものを使う（控えは、本文しか持たないから）。id の順に並べる。
func MergeComments(logged, cur []Comment) []Comment {
	now := map[int64]Comment{}
	for _, c := range cur {
		now[c.ID] = c
	}
	seen := map[int64]bool{}
	out := make([]Comment, 0, len(logged)+len(cur))
	for _, c := range logged {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if x, ok := now[c.ID]; ok {
			x.Body = c.Body
			c = x
		}
		out = append(out, c)
	}
	for _, c := range cur {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// reviewMark は、レビューの記録の目印である。これを含むのに署名が合わないコメントは、書き換えられたレビューとみなす。
const reviewMark = KeyRecord + ": " + RecordReview

// Reviews は、pull request のコメントから、その提案のレビューを、コメントの id の順に読む。worker も使う。
//   - 署名の合う、その提案のレビューの記録だけを数える。だれが書いたかは見ない。
//   - レビューの記録の目印があるのに数えないもの（署名が合わない、提案の識別子が違う、識別子が重なる）があれば、鎖が切れたとする。
//   - 各レビューの Logiforge-Previous-Review: が、それより前に数えたレビューのどれか（最初は `-`）でなければ、鎖が切れたとする。
//     同時に 2 つのレビューが同じ直前のレビューを指すのは、正しい。鎖が見つけるのは、途中のレビューが丸ごと消されたことである。
//
// 鎖が切れていれば、バックエンドは反映しない。
func Reviews(keys Keyring, at Scope, id string, comments []Comment) ports.Reviews {
	var out ports.Reviews
	seen := map[string]bool{noPrevious: true}
	comments = slices.Clone(comments)
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	for _, c := range comments {
		if !strings.Contains(c.Body, reviewMark) {
			continue
		}
		broken := func(why string) {
			out.ChainBroken = true
			out.Warnings = append(out.Warnings, fmt.Sprintf("コメント %d：%s", c.ID, why))
		}
		r, err := keys.Parse(c.Body, at)
		if err != nil {
			broken(err.Error())
			continue
		}
		if r.Get(KeyRecord) != RecordReview {
			continue
		}
		if r.Get(KeyApplyRequest) != id {
			broken("提案の識別子が合わない")
			continue
		}
		rev, rid := r.Get(KeyReviewRevision), r.Get(KeyReviewID)
		if rid == noPrevious || seen[rid] {
			broken("識別子が重なっている")
			continue
		}
		if !seen[r.Get(KeyPreviousReview)] {
			broken("Logiforge-Previous-Review: が、前に数えたレビューを指していない（途中のレビューが消された）")
		}
		seen[rid] = true
		out.Items = append(out.Items, ports.Review{
			ID: rid, Kind: ports.ReviewKind(r.Get(KeyReviewKind)), Body: r.Body, Revision: rev,
			Reviewer: r.Get(KeyPrincipal), Session: r.Get(KeySession), PromptRevision: r.Get(KeyPromptRevision), CreatedAt: c.CreatedAt,
		})
	}
	return out
}

// ---- 人への依頼 ----

// humanRequest は、issue と、その記録から人への依頼を組み立てる。answer の記録は、依頼の識別子が合うものの最初だけを数える。
func humanRequest(is Issue, recs []parsed, warnings []string) (ports.HumanRequest, *parsed, bool) {
	hr := ports.HumanRequest{Number: is.Number, CreatedAt: is.CreatedAt, Warnings: warnings}
	var answer *parsed
	found := false
	for i, r := range recs {
		switch r.Get(KeyRecord) {
		case RecordHumanRequest:
			if found {
				hr.Warnings = append(hr.Warnings, fmt.Sprintf("コメント %d：human_request の記録が重なっている", r.Comment.ID))
				continue
			}
			found = true
			hr.ID, hr.Kind, hr.TargetNode = r.Get(KeyHumanRequest), ports.HumanRequestKind(r.Get(KeyHumanKind)), r.Get(KeyTargetNode)
			hr.Body, hr.Requester, hr.Session = r.Body, r.Get(KeyPrincipal), r.Get(KeySession)
		case RecordAnswer:
			switch {
			case !found || r.Get(KeyHumanRequest) != hr.ID:
				hr.Warnings = append(hr.Warnings, fmt.Sprintf("コメント %d：依頼の識別子が合わない回答", r.Comment.ID))
			case answer != nil:
				hr.Warnings = append(hr.Warnings, fmt.Sprintf("コメント %d：回答が重なっている", r.Comment.ID))
			default:
				answer = &recs[i]
				ans := &ports.Answer{Body: r.Body, AnsweredBy: r.Get(KeyAnsweredBy), CreatedAt: r.Comment.CreatedAt}
				for _, v := range r.All(KeyAttachment) {
					name, sha := splitAttachment(v)
					ans.Attachments = append(ans.Attachments, ports.Attachment{Name: name, SHA256: sha, ID: r.Comment.Attachments[name]})
				}
				hr.Answer = ans
			}
		}
	}
	return hr, answer, found
}

func attachmentTrailer(name, sha string) string { return name + " sha256:" + sha }

func splitAttachment(v string) (name, sha string) {
	i := strings.LastIndex(v, " sha256:")
	if i < 0 {
		return v, ""
	}
	return v[:i], v[i+len(" sha256:"):]
}

func (a *Adapter) CreateHumanRequest(ctx context.Context, repo string, n ports.NewHumanRequest) (ports.HumanRequest, error) {
	title := titlePrefix + n.ID
	if first, _, _ := strings.Cut(strings.TrimSpace(n.Body), "\n"); first != "" {
		title += " " + truncate(first, 60)
	}
	is, err := a.API.CreateIssue(ctx, repo, title, n.Body, []string{"logiforge/" + string(n.Kind)})
	if err != nil {
		return ports.HumanRequest{}, err
	}
	rec := Record{Body: n.Body, Trailers: []Trailer{
		{KeyRecord, RecordHumanRequest}, {KeyHumanRequest, n.ID}, {KeyHumanKind, string(n.Kind)}, {KeyTargetNode, n.TargetNode},
		{KeyPrincipal, n.Requester}, {KeySession, n.Session},
	}}
	rec.Trailers = withRepository(rec.Trailers, repo)
	if _, err := a.comment(ctx, repo, is.Number, rec); err != nil {
		return ports.HumanRequest{}, err
	}
	f, err := a.readIssue(ctx, repo, is)
	if err != nil {
		return ports.HumanRequest{}, err
	}
	return f.hr, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

type foundIssue struct {
	hr     ports.HumanRequest
	issue  Issue
	answer *parsed
	ok     bool
}

func (a *Adapter) readIssue(ctx context.Context, repo string, is Issue) (foundIssue, error) {
	comments, err := a.withLog(ctx, repo, is.Number)
	if err != nil {
		return foundIssue{}, err
	}
	recs, warnings, err := a.recordsIn(ctx, repo, comments)
	if err != nil {
		return foundIssue{}, err
	}
	hr, answer, ok := humanRequest(is, forRepository(recs, repo), warnings)
	return foundIssue{hr, is, answer, ok}, nil
}

func (a *Adapter) findIssue(ctx context.Context, repo, id string) (foundIssue, error) {
	issues, err := a.API.ListIssues(ctx, repo)
	if err != nil {
		return foundIssue{}, err
	}
	for _, is := range issues {
		if strings.Contains(is.Title, id) {
			f, err := a.readIssue(ctx, repo, is)
			if err != nil {
				return foundIssue{}, err
			}
			if f.ok && f.hr.ID == id {
				return f, nil
			}
		}
	}
	comments, err := a.API.ListRepoComments(ctx, repo)
	if err != nil {
		return foundIssue{}, err
	}
	repoID, err := a.API.RepoID(ctx, repo)
	if err != nil {
		return foundIssue{}, err
	}
	by := byIssue(comments)
	for _, is := range issues {
		recs, warnings := a.records(repoID, by[is.Number])
		if hr, answer, ok := humanRequest(is, forRepository(recs, repo), warnings); ok && hr.ID == id {
			return foundIssue{hr, is, answer, true}, nil
		}
	}
	return foundIssue{}, ports.ErrNotFound
}

func (a *Adapter) GetHumanRequest(ctx context.Context, repo, id string) (ports.HumanRequest, error) {
	f, err := a.findIssue(ctx, repo, id)
	return f.hr, err
}

func (a *Adapter) ListHumanRequests(ctx context.Context, repo string) ([]ports.HumanRequest, error) {
	issues, err := a.API.ListIssues(ctx, repo)
	if err != nil {
		return nil, err
	}
	comments, err := a.API.ListRepoComments(ctx, repo)
	if err != nil {
		return nil, err
	}
	repoID, err := a.API.RepoID(ctx, repo)
	if err != nil {
		return nil, err
	}
	by := byIssue(comments)
	var out []ports.HumanRequest
	seen := map[string]bool{}
	for _, is := range issues {
		recs, warnings := a.records(repoID, by[is.Number])
		if hr, _, ok := humanRequest(is, forRepository(recs, repo), warnings); ok && !seen[hr.ID] {
			seen[hr.ID] = true
			out = append(out, hr)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func attachmentKeys(names, shas []string) []string {
	out := make([]string, len(names))
	for i := range names {
		out[i] = attachmentTrailer(names[i], shas[i])
	}
	slices.Sort(out)
	return out
}

// Answer は、回答を記録する。記録を書いたあとに添付を上げ、issue を閉じる。
// 途中で止まったときは、同じ本文・回答者・添付で呼び直せば、足りない添付を上げて閉じる。中身が違えば ErrAlreadyAnswered を返す。
func (a *Adapter) Answer(ctx context.Context, repo, id, body, answeredBy string, files []ports.NewAttachment) error {
	// 添付を持てない GitForge では、記録を書く前に断る。書いたあとでは、上げられない添付を持つ記録が残るから。
	if len(files) > 0 && !a.API.AttachmentsSupported() {
		return ports.ErrAttachmentsUnsupported
	}
	f, err := a.findIssue(ctx, repo, id)
	if err != nil {
		return err
	}
	var names, shas []string
	for _, file := range files {
		if slices.Contains(names, file.Name) {
			return fmt.Errorf("pullreq: 添付の名前 %q が重なっている", file.Name)
		}
		names, shas = append(names, file.Name), append(shas, sha256Hex(file.Content))
	}
	var commentID int64
	have := map[string]bool{}
	if f.hr.Answer != nil {
		var gotNames, gotShas []string
		for _, at := range f.hr.Answer.Attachments {
			gotNames, gotShas = append(gotNames, at.Name), append(gotShas, at.SHA256)
			if at.ID != "" {
				have[at.SHA256] = true
			}
		}
		normalized := strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
		if f.hr.Answer.Body != normalized || f.hr.Answer.AnsweredBy != answeredBy || !slices.Equal(attachmentKeys(names, shas), attachmentKeys(gotNames, gotShas)) {
			return ports.ErrAlreadyAnswered
		}
		commentID = f.answer.Comment.ID
	} else {
		trailers := []Trailer{{KeyRecord, RecordAnswer}, {KeyHumanRequest, id}, {KeyAnsweredBy, answeredBy}}
		for i := range files {
			trailers = append(trailers, Trailer{KeyAttachment, attachmentTrailer(names[i], shas[i])})
		}
		// 添付の sha256 の一覧も MAC に含める。添付は、記録を書いたあとに付ける。
		c, err := a.comment(ctx, repo, f.issue.Number, Record{Body: body, Trailers: trailers})
		if err != nil {
			return err
		}
		commentID = c.ID
	}
	for i, file := range files {
		if have[shas[i]] {
			continue
		}
		if _, err := a.API.UploadAttachment(ctx, repo, commentID, file.Name, file.Content); err != nil {
			return err
		}
		have[shas[i]] = true
	}
	return a.API.CloseIssue(ctx, repo, f.issue.Number)
}

// ReadAttachment は、回答の添付を sha256 で引いて読み、中身の sha256 を確かめる。
func (a *Adapter) ReadAttachment(ctx context.Context, repo, humanRequestID, sha string) ([]byte, error) {
	f, err := a.findIssue(ctx, repo, humanRequestID)
	if err != nil {
		return nil, err
	}
	if f.hr.Answer == nil {
		return nil, ports.ErrNotFound
	}
	for _, at := range f.hr.Answer.Attachments {
		if at.SHA256 != sha {
			continue
		}
		if at.ID == "" {
			return nil, ports.ErrNotFound
		}
		data, err := a.API.DownloadAttachment(ctx, repo, f.answer.Comment.ID, at.ID)
		if err != nil {
			return nil, err
		}
		if sha256Hex(data) != sha {
			return nil, ports.ErrAttachmentTamper
		}
		return data, nil
	}
	return nil, ports.ErrNotFound
}
