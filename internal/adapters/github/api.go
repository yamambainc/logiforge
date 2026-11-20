package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
)

// api は、pullreq.API を GitHub で実装する。ctx の主体のトークン（ports.WithToken）で呼ぶ。
type api struct{ *forge }

var _ pullreq.API = api{}

type user struct {
	Login string `json:"login"`
}

type labelJSON struct {
	Name string `json:"name"`
}

func labelNames(ls []labelJSON) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

type pullJSON struct {
	Number    int         `json:"number"`
	Title     string      `json:"title"`
	Body      string      `json:"body"`
	State     string      `json:"state"`
	Merged    bool        `json:"merged"`
	MergedAt  *string     `json:"merged_at"`
	Mergeable *bool       `json:"mergeable"`
	MergeSHA  string      `json:"merge_commit_sha"`
	User      user        `json:"user"`
	Labels    []labelJSON `json:"labels"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Head      struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// merged は、merge されたかを返す。一覧の応答には merged がないので、merged_at で見る。
func (p pullJSON) merged() bool { return p.Merged || p.MergedAt != nil }

func (p pullJSON) pull() pullreq.Pull {
	pr := pullreq.Pull{Number: p.Number, Title: p.Title, Body: p.Body, Head: p.Head.Ref, HeadSHA: p.Head.SHA,
		State: p.State, Merged: p.merged(), Base: p.Base.Ref, Author: p.User.Login, Labels: labelNames(p.Labels),
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	if pr.Merged {
		pr.MergeCommit = p.MergeSHA
	}
	return pr
}

func (a api) CreatePull(ctx context.Context, repo, head, title, body string) (pullreq.Pull, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Pull{}, err
	}
	var p pullJSON
	err = a.w(ctx, repo, http.MethodPost, rp+"/pulls", map[string]string{"head": head, "base": "main", "title": title, "body": body}, &p)
	// 同じブランチに open の pull request があると、422（"A pull request already exists"）を返す。
	if statusOf(err) == http.StatusUnprocessableEntity && strings.Contains(err.Error(), "already exists") {
		return pullreq.Pull{}, ports.ErrBranchHasOpen
	}
	return p.pull(), err
}

func (a api) getPull(ctx context.Context, repo string, number int) (pullJSON, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullJSON{}, err
	}
	var p pullJSON
	err = a.w(ctx, repo, http.MethodGet, fmt.Sprintf("%s/pulls/%d", rp, number), nil, &p)
	if statusOf(err) == http.StatusNotFound {
		return p, ports.ErrNotFound
	}
	return p, err
}

func (a api) GetPull(ctx context.Context, repo string, number int) (pullreq.Pull, error) {
	p, err := a.getPull(ctx, repo, number)
	return p.pull(), err
}

func (a api) ListPulls(ctx context.Context, repo, state string) ([]pullreq.Pull, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	ps, err := list[pullJSON](ctx, a.c, ports.TokenFrom(ctx), rp+"/pulls?state="+url.QueryEscape(state)+"&sort=created&direction=asc")
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Pull, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.pull())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// closedPageSize は、反映した pull request の一覧の 1 ページの件数である。
const closedPageSize = 100

func (a api) ClosedPullsPage(ctx context.Context, repo string, page int) ([]pullreq.Pull, bool, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, false, err
	}
	var ps []pullJSON
	path := fmt.Sprintf("%s/pulls?state=closed&sort=updated&direction=desc&per_page=%d&page=%d", rp, closedPageSize, page)
	if err := a.w(ctx, repo, http.MethodGet, path, nil, &ps); err != nil {
		return nil, false, err
	}
	out := make([]pullreq.Pull, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.pull())
	}
	return out, len(ps) == closedPageSize, nil
}

func (a api) EditPullBody(ctx context.Context, repo string, number int, body string) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, repo, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", rp, number), map[string]string{"body": body}, nil)
}

func (a api) ClosePull(ctx context.Context, repo string, number int) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, repo, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", rp, number), map[string]string{"state": "closed"}, nil)
}

// MergePull は、head の SHA を指定して squash merge する。
// GitHub は、merge できるかを非同期に確かめる（mergeable が null）。確かめ終わるまで待って呼び直す。
// 送り直しでもよい。すでに merge されていれば、head が同じなら反映のコミットを返し、違えば ErrHeadMoved を返す。
func (a api) MergePull(ctx context.Context, repo string, number int, headSHA, title, message string) (string, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	wait := a.cfg.MergeWait
	if wait == 0 {
		wait = 15 * time.Second
	}
	deadline := time.Now().Add(wait)
	body := map[string]string{"sha": headSHA, "merge_method": "squash", "commit_title": title, "commit_message": message}
	for {
		var res struct {
			SHA string `json:"sha"`
		}
		err := a.w(ctx, repo, http.MethodPut, fmt.Sprintf("%s/pulls/%d/merge", rp, number), body, &res)
		if err == nil {
			return res.SHA, nil
		}
		// 403 は、主体に merge の権限がないこと（Write がない、トークンの権限が足りない）。制限（二次の上限を含む）の 403 は、
		// client が Retry-After を守って待ち、待つ上限を超えたときだけ返るので、権限がないことにしない。
		if statusOf(err) == http.StatusForbidden && !limited(err) {
			return "", fmt.Errorf("%w（GitHub：%v）", ports.ErrMergeForbidden, err)
		}
		// 405 の "Repository rule violations found"：ルールセットが、この主体の merge を断った（例：ディレクトリの守りで、
		// reviewer の team に入っていない。design/adapters/github.md の「merge を断られたとき」）。待っても変わらないので、merge の権限がないこととする。
		if statusOf(err) == http.StatusMethodNotAllowed && strings.Contains(errMessage(err), "Repository rule violations") {
			return "", fmt.Errorf("%w（GitHub：%v）", ports.ErrMergeForbidden, err)
		}
		switch statusOf(err) {
		case http.StatusConflict:
			// 409：head が sha と違う（"Head branch was modified"）。
			return "", ports.ErrHeadMoved
		case http.StatusMethodNotAllowed:
			// 405：merge できない（衝突、まだ確かめている、すでに merge された、守る設定）。pull request を読み直して決める。
			p, gerr := a.getPull(ctx, repo, number)
			if gerr != nil {
				return "", gerr
			}
			if p.merged() {
				if p.Head.SHA == headSHA {
					return p.MergeSHA, nil
				}
				return "", ports.ErrHeadMoved
			}
			if p.Head.SHA != headSHA {
				return "", ports.ErrHeadMoved
			}
			if p.Mergeable != nil && !*p.Mergeable {
				return "", fmt.Errorf("%w（GitHub：%v）", ports.ErrConflict, err)
			}
			if p.Mergeable != nil || time.Now().After(deadline) {
				// mergeable が true なのに断られたのは、衝突ではない（守る設定など）。そのまま返す。
				return "", err
			}
			if err := a.c.sleep(ctx, 500*time.Millisecond); err != nil {
				return "", err
			}
		default:
			return "", err
		}
	}
}

type commentJSON struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      user      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

func (c commentJSON) comment(number int) pullreq.Comment {
	return pullreq.Comment{ID: c.ID, IssueNumber: number, Body: c.Body, Author: c.User.Login, CreatedAt: c.CreatedAt}
}

func (a api) CreateComment(ctx context.Context, repo string, number int, body string) (pullreq.Comment, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Comment{}, err
	}
	var c commentJSON
	if err := a.w(ctx, repo, http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", rp, number), map[string]string{"body": body}, &c); err != nil {
		return pullreq.Comment{}, err
	}
	return c.comment(number), nil
}

func (a api) ListComments(ctx context.Context, repo string, number int) ([]pullreq.Comment, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	cs, err := list[commentJSON](ctx, a.c, ports.TokenFrom(ctx), fmt.Sprintf("%s/issues/%d/comments", rp, number))
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.comment(number))
	}
	return out, nil
}

// reviewState は、GitHub の review の state と、ports.ReviewKind の対応である。
var reviewState = map[string]ports.ReviewKind{"APPROVED": ports.ReviewApprove, "CHANGES_REQUESTED": ports.ReviewRequestChanges, "COMMENTED": ports.ReviewComment}

var reviewEvent = map[ports.ReviewKind]string{ports.ReviewApprove: "APPROVE", ports.ReviewRequestChanges: "REQUEST_CHANGES", ports.ReviewComment: "COMMENT"}

type reviewJSON struct {
	ID          int64     `json:"id"`
	User        user      `json:"user"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	CommitID    string    `json:"commit_id"`
	Association string    `json:"author_association"`
	SubmittedAt time.Time `json:"submitted_at"`
}

func (r reviewJSON) review() (pullreq.PullReview, bool) {
	if r.State == "DISMISSED" {
		return pullreq.PullReview{ID: r.ID, Author: r.User.Login, Body: r.Body, CommitID: r.CommitID, Dismissed: true, Association: r.Association, SubmittedAt: r.SubmittedAt}, true
	}
	k, ok := reviewState[r.State]
	if !ok {
		return pullreq.PullReview{}, false
	}
	return pullreq.PullReview{ID: r.ID, Kind: k, Author: r.User.Login, Body: r.Body, CommitID: r.CommitID, Association: r.Association, SubmittedAt: r.SubmittedAt}, true
}

func (a api) ListReviews(ctx context.Context, repo string, number int) ([]pullreq.PullReview, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	rs, err := list[reviewJSON](ctx, a.c, ports.TokenFrom(ctx), fmt.Sprintf("%s/pulls/%d/reviews", rp, number))
	if err != nil {
		return nil, err
	}
	var out []pullreq.PullReview
	for _, r := range rs {
		if x, ok := r.review(); ok {
			out = append(out, x)
		}
	}
	return out, nil
}

func (a api) CreateReview(ctx context.Context, repo string, number int, event ports.ReviewKind, body, commitID string) (pullreq.PullReview, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.PullReview{}, err
	}
	var r reviewJSON
	err = a.w(ctx, repo, http.MethodPost, fmt.Sprintf("%s/pulls/%d/reviews", rp, number),
		map[string]string{"event": reviewEvent[event], "body": body, "commit_id": commitID}, &r)
	if err != nil {
		return pullreq.PullReview{}, err
	}
	x, _ := r.review()
	return x, nil
}

// ReviewerCanPush は、approve か request changes を書いた人の、repository の権限を読む（GET /collaborators/{user}/permission）。
// 1 件の apply request を読むときに、書いた人ごとに 1 回だけ呼ぶ。
func (a api) ReviewerCanPush(ctx context.Context, repo string, r pullreq.PullReview) (bool, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return false, err
	}
	var res struct {
		Permission string `json:"permission"`
	}
	err = a.w(ctx, repo, http.MethodGet, rp+"/collaborators/"+url.PathEscape(r.Author)+"/permission", nil, &res)
	// 読む主体のトークンで権限を読めなければ、review の author_association で決める。
	if s := statusOf(err); s == http.StatusNotFound || s == http.StatusForbidden {
		return r.Association == "OWNER" || r.Association == "MEMBER" || r.Association == "COLLABORATOR", nil
	}
	if err != nil {
		return false, err
	}
	return res.Permission == "admin" || res.Permission == "write" || res.Permission == "maintain", nil
}

type statusJSON struct {
	Context     string    `json:"context"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	Creator     user      `json:"creator"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a api) SetStatus(ctx context.Context, repo, sha string, s pullreq.Status) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, repo, http.MethodPost, rp+"/statuses/"+url.PathEscape(sha),
		map[string]string{"state": s.State, "context": s.Context, "description": s.Description}, nil)
}

func (a api) ListStatuses(ctx context.Context, repo, sha string) ([]pullreq.Status, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	ss, err := list[statusJSON](ctx, a.c, ports.TokenFrom(ctx), rp+"/commits/"+url.PathEscape(sha)+"/statuses")
	if statusOf(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Status, 0, len(ss))
	for _, s := range ss {
		out = append(out, pullreq.Status{Context: s.Context, State: s.State, Description: s.Description, Creator: s.Creator.Login, CreatedAt: s.CreatedAt})
	}
	return out, nil
}

// EnsureLabel は、ラベルがなければ作る。GitHub は、ない名前のラベルを付けると既定の色で作るので、付ける前に呼ぶ。
func (a api) EnsureLabel(ctx context.Context, repo string, l pullreq.Label) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	err = a.w(ctx, repo, http.MethodGet, rp+"/labels/"+url.PathEscape(l.Name), nil, nil)
	if err == nil {
		return nil
	}
	if statusOf(err) != http.StatusNotFound {
		return err
	}
	err = a.w(ctx, repo, http.MethodPost, rp+"/labels",
		map[string]string{"name": l.Name, "color": strings.TrimPrefix(l.Color, "#"), "description": l.Description}, nil)
	// 同時に作られていれば 422（already_exists）。
	if statusOf(err) == http.StatusUnprocessableEntity {
		return nil
	}
	return err
}

func (a api) AddLabel(ctx context.Context, repo string, number int, name string) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, repo, http.MethodPost, fmt.Sprintf("%s/issues/%d/labels", rp, number), map[string][]string{"labels": {name}}, nil)
}

func (a api) RemoveLabel(ctx context.Context, repo string, number int, name string) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	err = a.w(ctx, repo, http.MethodDelete, fmt.Sprintf("%s/issues/%d/labels/%s", rp, number, url.PathEscape(name)), nil, nil)
	if statusOf(err) == http.StatusNotFound {
		return nil
	}
	return err
}

type eventJSON struct {
	Event     string    `json:"event"`
	Actor     user      `json:"actor"`
	Label     labelJSON `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

// ListEvents は、issue の events（GET /issues/{number}/events）を読む。pull request も issue として読める。
func (a api) ListEvents(ctx context.Context, repo string, number int) ([]pullreq.Event, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	es, err := list[eventJSON](ctx, a.c, ports.TokenFrom(ctx), fmt.Sprintf("%s/issues/%d/events", rp, number))
	if err != nil {
		return nil, err
	}
	var out []pullreq.Event
	for _, e := range es {
		switch e.Event {
		case "labeled", "unlabeled", "closed", "reopened":
			out = append(out, pullreq.Event{Kind: e.Event, Label: e.Label.Name, Actor: e.Actor.Login, CreatedAt: e.CreatedAt})
		}
	}
	return out, nil
}

type issueJSON struct {
	Number      int         `json:"number"`
	Title       string      `json:"title"`
	Body        string      `json:"body"`
	State       string      `json:"state"`
	User        user        `json:"user"`
	Labels      []labelJSON `json:"labels"`
	CreatedAt   time.Time   `json:"created_at"`
	ClosedAt    *time.Time  `json:"closed_at"`
	PullRequest *struct{}   `json:"pull_request"`
}

func (i issueJSON) issue() pullreq.Issue {
	is := pullreq.Issue{Number: i.Number, Title: i.Title, Body: i.Body, State: i.State, Author: i.User.Login, Labels: labelNames(i.Labels), CreatedAt: i.CreatedAt}
	if i.ClosedAt != nil {
		is.ClosedAt = *i.ClosedAt
	}
	return is
}

// CreateIssue は、issue を作る。ラベルは名前で渡す。ラベルは、先に EnsureLabel で作っておく。
func (a api) CreateIssue(ctx context.Context, repo, title, body string, labels []string) (pullreq.Issue, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Issue{}, err
	}
	if labels == nil {
		labels = []string{}
	}
	var is issueJSON
	err = a.w(ctx, repo, http.MethodPost, rp+"/issues", map[string]any{"title": title, "body": body, "labels": labels}, &is)
	return is.issue(), err
}

// ListIssues は、issue を作った順に返す。GitHub の issue の一覧は pull request も含むので、除く。
func (a api) ListIssues(ctx context.Context, repo string) ([]pullreq.Issue, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	is, err := list[issueJSON](ctx, a.c, ports.TokenFrom(ctx), rp+"/issues?state=all&sort=created&direction=asc")
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Issue, 0, len(is))
	for _, i := range is {
		if i.PullRequest != nil {
			continue
		}
		out = append(out, i.issue())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (a api) GetIssue(ctx context.Context, repo string, number int) (pullreq.Issue, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Issue{}, err
	}
	var is issueJSON
	err = a.w(ctx, repo, http.MethodGet, fmt.Sprintf("%s/issues/%d", rp, number), nil, &is)
	if statusOf(err) == http.StatusNotFound || (err == nil && is.PullRequest != nil) {
		return pullreq.Issue{}, ports.ErrNotFound
	}
	return is.issue(), err
}

func (a api) CloseIssue(ctx context.Context, repo string, number int) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, repo, http.MethodPatch, fmt.Sprintf("%s/issues/%d", rp, number), map[string]string{"state": "closed"}, nil)
}

// AttachmentsSupported は、false を返す。GitHub の API では、コメントにファイルを添付できない（design/adapters/README.md）。
func (a api) AttachmentsSupported() bool { return false }

func (a api) UploadAttachment(context.Context, string, int64, string, []byte) (string, error) {
	return "", ports.ErrAttachmentsUnsupported
}

func (a api) DownloadAttachment(context.Context, string, int64, string) ([]byte, error) {
	return nil, ports.ErrAttachmentsUnsupported
}

// EditIssueBody は、issue の本文を書き換える。
func (a api) EditIssueBody(ctx context.Context, repo string, number int, body string) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, repo, http.MethodPatch, fmt.Sprintf("%s/issues/%d", rp, number), map[string]string{"body": body}, nil)
}
