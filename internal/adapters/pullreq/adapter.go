package pullreq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
)

// Repos は、repository の列挙と用意である。GitForge ごとに違うので、それぞれのアダプタが持つ。
type Repos interface {
	ListRepositories(ctx context.Context) ([]ports.Repository, error)
	GitForgeRef(ctx context.Context, name string) (string, error)
	SetupRepository(ctx context.Context, gitForgeRef string, files []ports.File, message string) error
}

// Adapter は、API と鍵から、GitForge のポートを実装する。repo の引数は gitforge_ref である。
type Adapter struct {
	Repos
	API  API
	Keys Keyring
}

var _ ports.GitForge = (*Adapter)(nil)

// titlePrefix は、pull request と issue のタイトルの接頭辞である。タイトルは、探すための手がかりにだけ使い、記録としては読まない。
const titlePrefix = "[Logiforge] "

// records は、書き込み主体のコメントのうち、正しい記録を返す。MAC が合わないものは警告にする。
func (a *Adapter) records(comments []Comment) (recs []parsed, warnings []string) {
	for _, c := range comments {
		if c.Author != a.API.WriterLogin() {
			continue
		}
		r, err := a.Keys.Parse(c.Body)
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

type parsed struct {
	Record
	Comment Comment
}

func (a *Adapter) comment(ctx context.Context, repo string, number int, r Record) (Comment, error) {
	text, err := a.Keys.Format(r)
	if err != nil {
		return Comment{}, err
	}
	return a.API.CreateComment(ctx, repo, number, text)
}

// ---- 提案 ----

func (a *Adapter) applyRequest(p Pull, recs []parsed, warnings []string) (ports.ApplyRequest, bool) {
	ar := ports.ApplyRequest{Number: p.Number, Branch: p.Head, Description: p.Body, HeadRevision: p.HeadSHA, CreatedAt: p.CreatedAt, Warnings: warnings}
	found, closed := false, false
	for _, r := range recs {
		switch r.Get(KeyRecord) {
		case RecordApplyRequest:
			if !found {
				found = true
				ar.ID, ar.FirstRevision, ar.Opener = r.Get(KeyApplyRequest), r.Get(KeyFirstRevision), r.Get(KeyPrincipal)
			}
		case RecordClose:
			closed = true
			ar.CloseReason, ar.CloseSession = r.Body, r.Get(KeySession)
		case RecordMergeFailure:
			ar.MergeFailures = append(ar.MergeFailures, r.Get(KeyMergeFailure))
		}
	}
	switch {
	case p.Merged:
		ar.State = ports.StateMerged
	case closed || p.State == "closed":
		ar.State = ports.StateClosed
	default:
		ar.State = ports.StateOpen
	}
	return ar, found
}

func (a *Adapter) CreateApplyRequest(ctx context.Context, repo string, n ports.NewApplyRequest) (ports.ApplyRequest, error) {
	pulls, err := a.API.ListPulls(ctx, repo)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	var pull *Pull
	for i, p := range pulls {
		if p.Author != a.API.WriterLogin() || p.State != "open" || p.Merged || p.Head != n.Branch {
			continue
		}
		comments, err := a.API.ListComments(ctx, repo, p.Number)
		if err != nil {
			return ports.ApplyRequest{}, err
		}
		recs, warnings := a.records(comments)
		if ar, ok := a.applyRequest(p, recs, warnings); ok {
			if ar.ID == n.ID {
				return ar, nil
			}
			return ports.ApplyRequest{}, ports.ErrBranchHasOpen
		}
		// 作ったあと、記録を書く前に止まったもの。新しく作らずに、記録を書く。
		pull = &pulls[i]
	}
	if pull == nil {
		p, err := a.API.CreatePull(ctx, repo, n.Branch, titlePrefix+n.ID, n.Description)
		if err != nil {
			return ports.ApplyRequest{}, err
		}
		pull = &p
	}
	rec := Record{Trailers: []Trailer{{KeyRecord, RecordApplyRequest}, {KeyApplyRequest, n.ID}, {KeyFirstRevision, n.FirstRevision}, {KeyPrincipal, n.Opener}}}
	c, err := a.comment(ctx, repo, pull.Number, rec)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	ar, _ := a.applyRequest(*pull, []parsed{{Record: rec, Comment: c}}, nil)
	return ar, nil
}

// findPull は、識別子の提案の pull request と記録を探す。タイトルを手がかりにし、なければ repository のコメントから探す。
func (a *Adapter) findPull(ctx context.Context, repo, id string) (ports.ApplyRequest, error) {
	pulls, err := a.API.ListPulls(ctx, repo)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	try := func(p Pull) (ports.ApplyRequest, bool, error) {
		comments, err := a.API.ListComments(ctx, repo, p.Number)
		if err != nil {
			return ports.ApplyRequest{}, false, err
		}
		recs, warnings := a.records(comments)
		ar, ok := a.applyRequest(p, recs, warnings)
		return ar, ok && ar.ID == id, nil
	}
	for _, p := range pulls {
		if p.Author == a.API.WriterLogin() && strings.Contains(p.Title, id) {
			ar, ok, err := try(p)
			if err != nil || ok {
				return ar, err
			}
		}
	}
	all, err := a.ListApplyRequests(ctx, repo)
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

func (a *Adapter) GetApplyRequest(ctx context.Context, repo, id string) (ports.ApplyRequest, error) {
	return a.findPull(ctx, repo, id)
}

func (a *Adapter) ListApplyRequests(ctx context.Context, repo string) ([]ports.ApplyRequest, error) {
	pulls, err := a.API.ListPulls(ctx, repo)
	if err != nil {
		return nil, err
	}
	comments, err := a.API.ListRepoComments(ctx, repo)
	if err != nil {
		return nil, err
	}
	byIssue := map[int][]Comment{}
	for _, c := range comments {
		byIssue[c.IssueNumber] = append(byIssue[c.IssueNumber], c)
	}
	var out []ports.ApplyRequest
	for _, p := range pulls {
		if p.Author != a.API.WriterLogin() {
			continue
		}
		recs, warnings := a.records(byIssue[p.Number])
		if ar, ok := a.applyRequest(p, recs, warnings); ok {
			out = append(out, ar)
		}
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
	if _, err := a.comment(ctx, repo, ar.Number, Record{Body: reason, Trailers: []Trailer{{KeyRecord, RecordClose}, {KeySession, session}}}); err != nil {
		return err
	}
	return a.API.ClosePull(ctx, repo, ar.Number)
}

func (a *Adapter) RecordMergeFailure(ctx context.Context, repo, id, reason string) error {
	ar, err := a.findPull(ctx, repo, id)
	if err != nil {
		return err
	}
	_, err = a.comment(ctx, repo, ar.Number, Record{Trailers: []Trailer{{KeyRecord, RecordMergeFailure}, {KeyMergeFailure, reason}}})
	return err
}

// ---- レビュー ----

func (a *Adapter) AddReview(ctx context.Context, repo, applyRequestID string, r ports.Review) error {
	ar, err := a.findPull(ctx, repo, applyRequestID)
	if err != nil {
		return err
	}
	text, err := a.Keys.Format(Record{Body: r.Body, Trailers: []Trailer{
		{KeyRecord, RecordReview}, {KeyReviewID, r.ID}, {KeyReviewKind, string(r.Kind)}, {KeyReviewRevision, r.Revision},
		{KeyPrincipal, r.Reviewer}, {KeySession, r.Session}, {KeyPromptRevision, r.PromptRevision},
	}})
	if err != nil {
		return err
	}
	return a.API.CreateReview(ctx, repo, ar.Number, r.Revision, text)
}

func (a *Adapter) ListReviews(ctx context.Context, repo, applyRequestID string) ([]ports.Review, error) {
	ar, err := a.findPull(ctx, repo, applyRequestID)
	if err != nil {
		return nil, err
	}
	reviews, err := a.API.ListReviews(ctx, repo, ar.Number)
	if err != nil {
		return nil, err
	}
	var out []ports.Review
	for _, pr := range reviews {
		if pr.Author != a.API.WriterLogin() {
			continue
		}
		r, err := a.Keys.Parse(pr.Body)
		if err != nil || r.Get(KeyRecord) != RecordReview {
			continue
		}
		rev := r.Get(KeyReviewRevision)
		if pr.CommitSHA != "" && pr.CommitSHA != rev {
			continue
		}
		out = append(out, ports.Review{
			ID: r.Get(KeyReviewID), Kind: ports.ReviewKind(r.Get(KeyReviewKind)), Body: r.Body, Revision: rev,
			Reviewer: r.Get(KeyPrincipal), Session: r.Get(KeySession), PromptRevision: r.Get(KeyPromptRevision), CreatedAt: pr.CreatedAt,
		})
	}
	return out, nil
}

// ---- 人への依頼 ----

func (a *Adapter) humanRequest(is Issue, recs []parsed, warnings []string) (ports.HumanRequest, bool) {
	hr := ports.HumanRequest{Number: is.Number, CreatedAt: is.CreatedAt, Warnings: warnings}
	found := false
	for _, r := range recs {
		switch r.Get(KeyRecord) {
		case RecordHumanRequest:
			if !found {
				found = true
				hr.ID, hr.Kind, hr.TargetNode = r.Get(KeyHumanRequest), ports.HumanRequestKind(r.Get(KeyHumanKind)), r.Get(KeyTargetNode)
				hr.Body, hr.Requester, hr.Session = r.Body, r.Get(KeyPrincipal), r.Get(KeySession)
			}
		case RecordAnswer:
			if hr.Answer == nil {
				ans := &ports.Answer{Body: r.Body, AnsweredBy: r.Get(KeyAnsweredBy), CreatedAt: r.Comment.CreatedAt}
				for _, v := range r.All(KeyAttachment) {
					name, sha := splitAttachment(v)
					ans.Attachments = append(ans.Attachments, ports.Attachment{Name: name, SHA256: sha, ID: r.Comment.Attachments[name]})
				}
				hr.Answer = ans
			}
		}
	}
	return hr, found
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
	c, err := a.comment(ctx, repo, is.Number, rec)
	if err != nil {
		return ports.HumanRequest{}, err
	}
	hr, _ := a.humanRequest(is, []parsed{{Record: rec, Comment: c}}, nil)
	return hr, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

type foundIssue struct {
	hr    ports.HumanRequest
	issue Issue
	recs  []parsed
}

func (a *Adapter) findIssue(ctx context.Context, repo, id string) (foundIssue, error) {
	issues, err := a.API.ListIssues(ctx, repo)
	if err != nil {
		return foundIssue{}, err
	}
	check := func(is Issue, comments []Comment) (foundIssue, bool) {
		recs, warnings := a.records(comments)
		hr, ok := a.humanRequest(is, recs, warnings)
		return foundIssue{hr, is, recs}, ok && hr.ID == id
	}
	for _, is := range issues {
		if is.Author == a.API.WriterLogin() && strings.Contains(is.Title, id) {
			comments, err := a.API.ListComments(ctx, repo, is.Number)
			if err != nil {
				return foundIssue{}, err
			}
			if f, ok := check(is, comments); ok {
				return f, nil
			}
		}
	}
	comments, err := a.API.ListRepoComments(ctx, repo)
	if err != nil {
		return foundIssue{}, err
	}
	byIssue := map[int][]Comment{}
	for _, c := range comments {
		byIssue[c.IssueNumber] = append(byIssue[c.IssueNumber], c)
	}
	for _, is := range issues {
		if f, ok := check(is, byIssue[is.Number]); ok && is.Author == a.API.WriterLogin() {
			return f, nil
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
	byIssue := map[int][]Comment{}
	for _, c := range comments {
		byIssue[c.IssueNumber] = append(byIssue[c.IssueNumber], c)
	}
	var out []ports.HumanRequest
	for _, is := range issues {
		if is.Author != a.API.WriterLogin() {
			continue
		}
		recs, warnings := a.records(byIssue[is.Number])
		if hr, ok := a.humanRequest(is, recs, warnings); ok {
			out = append(out, hr)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (a *Adapter) Answer(ctx context.Context, repo, id, body, answeredBy string, files []ports.NewAttachment) error {
	f, err := a.findIssue(ctx, repo, id)
	if err != nil {
		return err
	}
	if f.hr.Answer != nil {
		return ports.ErrAlreadyAnswered
	}
	trailers := []Trailer{{KeyRecord, RecordAnswer}, {KeyHumanRequest, id}, {KeyAnsweredBy, answeredBy}}
	for _, file := range files {
		sum := sha256.Sum256(file.Content)
		trailers = append(trailers, Trailer{KeyAttachment, attachmentTrailer(file.Name, hex.EncodeToString(sum[:]))})
	}
	// 添付の sha256 の一覧も MAC に含める。添付は、記録を書いたあとに付ける。
	c, err := a.comment(ctx, repo, f.issue.Number, Record{Body: body, Trailers: trailers})
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, err := a.API.UploadAttachment(ctx, repo, c.ID, file.Name, file.Content); err != nil {
			return err
		}
	}
	return a.API.CloseIssue(ctx, repo, f.issue.Number)
}

func (a *Adapter) ReadAttachment(ctx context.Context, repo, humanRequestID, sha string) ([]byte, error) {
	f, err := a.findIssue(ctx, repo, humanRequestID)
	if err != nil {
		return nil, err
	}
	if f.hr.Answer == nil {
		return nil, ports.ErrNotFound
	}
	var commentID int64
	for _, r := range f.recs {
		if r.Get(KeyRecord) == RecordAnswer {
			commentID = r.Comment.ID
			break
		}
	}
	for _, at := range f.hr.Answer.Attachments {
		if at.SHA256 != sha {
			continue
		}
		if at.ID == "" {
			return nil, ports.ErrNotFound
		}
		data, err := a.API.DownloadAttachment(ctx, repo, commentID, at.ID)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != sha {
			return nil, ports.ErrAttachmentTamper
		}
		return data, nil
	}
	return nil, ports.ErrNotFound
}
