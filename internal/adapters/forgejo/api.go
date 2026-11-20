package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
)

// api は、pullreq.API を Forgejo で実装する。ctx の主体のトークン（ports.WithToken）で呼ぶ。
type api struct{ *client }

var _ pullreq.API = api{}

type user struct {
	Login string `json:"login"`
}

type labelJSON struct {
	ID   int64  `json:"id"`
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
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	// Mergeable は、Forgejo が merge できるかを確かめた結果。
	Mergeable      bool        `json:"mergeable"`
	MergeCommitSHA string      `json:"merge_commit_sha"`
	User           user        `json:"user"`
	Labels         []labelJSON `json:"labels"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Head           struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p pullJSON) pull() pullreq.Pull {
	pr := pullreq.Pull{Number: p.Number, Title: p.Title, Body: p.Body, Head: p.Head.Ref, HeadSHA: p.Head.SHA,
		State: p.State, Merged: p.Merged, Base: p.Base.Ref, Author: p.User.Login, Labels: labelNames(p.Labels),
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	if p.Merged {
		pr.MergeCommit = p.MergeCommitSHA
	}
	return pr
}

func (a api) w(ctx context.Context, method, path string, in, out any) error {
	return a.do(ctx, ports.TokenFrom(ctx), method, path, in, out)
}

func (a api) CreatePull(ctx context.Context, repo, head, title, body string) (pullreq.Pull, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Pull{}, err
	}
	var p pullJSON
	err = a.w(ctx, http.MethodPost, rp+"/pulls", map[string]string{"head": head, "base": "main", "title": title, "body": body}, &p)
	if statusOf(err) == http.StatusConflict {
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
	err = a.w(ctx, http.MethodGet, fmt.Sprintf("%s/pulls/%d", rp, number), nil, &p)
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
	ps, err := list[pullJSON](ctx, a.client, ports.TokenFrom(ctx), rp+"/pulls?state="+url.QueryEscape(state)+"&sort=oldest")
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
const closedPageSize = 50

func (a api) ClosedPullsPage(ctx context.Context, repo string, page int) ([]pullreq.Pull, bool, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, false, err
	}
	var ps []pullJSON
	path := fmt.Sprintf("%s/pulls?state=closed&sort=recentupdate&page=%d&limit=%d", rp, page, closedPageSize)
	if err := a.w(ctx, http.MethodGet, path, nil, &ps); err != nil {
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
	return a.w(ctx, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", rp, number), map[string]string{"body": body}, nil)
}

func (a api) ClosePull(ctx context.Context, repo string, number int) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", rp, number), map[string]string{"state": "closed"}, nil)
}

// MergePull は、squash merge する。Forgejo は、merge できるかを非同期に確かめるので、確かめ終わるまで待って呼び直す。
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
	body := map[string]any{"Do": "squash", "head_commit_id": headSHA, "MergeTitleField": title, "MergeMessageField": message}
	for {
		err := a.w(ctx, http.MethodPost, fmt.Sprintf("%s/pulls/%d/merge", rp, number), body, nil)
		if err == nil {
			p, err := a.getPull(ctx, repo, number)
			return p.MergeCommitSHA, err
		}
		// 主体に merge の権限がなければ、403 か、405 の "User not allowed to merge PR" を返す（Forgejo 17 で確かめた）。
		// 待っても変わらないので、すぐに返す。
		if s := statusOf(err); s == http.StatusForbidden || (s == http.StatusMethodNotAllowed && strings.Contains(strings.ToLower(errMessage(err)), "not allowed to merge")) {
			return "", fmt.Errorf("%w（Forgejo：%v）", ports.ErrMergeForbidden, err)
		}
		switch statusOf(err) {
		case http.StatusConflict:
			// head_commit_id が今の head と違う（"head out of date"）。
			return "", ports.ErrHeadMoved
		case http.StatusMethodNotAllowed, http.StatusInternalServerError:
			// 405：Forgejo が、merge できるかをまだ確かめている。500：衝突のある merge を試みて失敗した（Forgejo 17 で、ときどき起きる）。
			// どちらも、pull request を読み直して決める。
			p, gerr := a.getPull(ctx, repo, number)
			if gerr != nil {
				return "", gerr
			}
			if p.Merged {
				if p.Head.SHA == headSHA {
					return p.MergeCommitSHA, nil
				}
				return "", ports.ErrHeadMoved
			}
			if p.Head.SHA != headSHA {
				return "", ports.ErrHeadMoved
			}
			if time.Now().After(deadline) {
				// Forgejo は、衝突と「まだ確かめている」を、どちらも mergeable: false で表す。待っても merge できないものを衝突とみなし、
				// Forgejo のメッセージを添える。
				if !p.Mergeable && p.State == "open" {
					return "", fmt.Errorf("%w（Forgejo：%v）", ports.ErrConflict, err)
				}
				return "", err
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(300 * time.Millisecond):
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
	Assets    []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"assets"`
}

func (c commentJSON) comment(number int) pullreq.Comment {
	out := pullreq.Comment{ID: c.ID, IssueNumber: number, Body: c.Body, Author: c.User.Login, CreatedAt: c.CreatedAt}
	if len(c.Assets) > 0 {
		out.Attachments = map[string]string{}
		for _, as := range c.Assets {
			out.Attachments[as.Name] = strconv.FormatInt(as.ID, 10)
		}
	}
	return out
}

func (a api) CreateComment(ctx context.Context, repo string, number int, body string) (pullreq.Comment, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Comment{}, err
	}
	var c commentJSON
	if err := a.w(ctx, http.MethodPost, fmt.Sprintf("%s/issues/%d/comments", rp, number), map[string]string{"body": body}, &c); err != nil {
		return pullreq.Comment{}, err
	}
	return c.comment(number), nil
}

func (a api) ListComments(ctx context.Context, repo string, number int) ([]pullreq.Comment, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	cs, err := list[commentJSON](ctx, a.client, ports.TokenFrom(ctx), fmt.Sprintf("%s/issues/%d/comments", rp, number))
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.comment(number))
	}
	return out, nil
}

// reviewState は、Forgejo の review の state と、ports.ReviewKind の対応である。
var reviewState = map[string]ports.ReviewKind{"APPROVED": ports.ReviewApprove, "REQUEST_CHANGES": ports.ReviewRequestChanges, "COMMENT": ports.ReviewComment}

var reviewEvent = map[ports.ReviewKind]string{ports.ReviewApprove: "APPROVED", ports.ReviewRequestChanges: "REQUEST_CHANGES", ports.ReviewComment: "COMMENT"}

type reviewJSON struct {
	ID          int64     `json:"id"`
	User        *user     `json:"user"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	CommitID    string    `json:"commit_id"`
	Dismissed   bool      `json:"dismissed"`
	Official    bool      `json:"official"`
	SubmittedAt time.Time `json:"submitted_at"`
}

func (r reviewJSON) review() (pullreq.PullReview, bool) {
	k, ok := reviewState[r.State]
	if !ok || r.User == nil {
		return pullreq.PullReview{}, false
	}
	return pullreq.PullReview{ID: r.ID, Kind: k, Author: r.User.Login, Body: r.Body, CommitID: r.CommitID, Dismissed: r.Dismissed,
		Official: r.Official, SubmittedAt: r.SubmittedAt}, true
}

func (a api) ListReviews(ctx context.Context, repo string, number int) ([]pullreq.PullReview, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	rs, err := list[reviewJSON](ctx, a.client, ports.TokenFrom(ctx), fmt.Sprintf("%s/pulls/%d/reviews", rp, number))
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
	err = a.w(ctx, http.MethodPost, fmt.Sprintf("%s/pulls/%d/reviews", rp, number),
		map[string]string{"event": reviewEvent[event], "body": body, "commit_id": commitID}, &r)
	if err != nil {
		return pullreq.PullReview{}, err
	}
	x, _ := r.review()
	return x, nil
}

// ReviewerCanPush は、approve の review の official を返す。Forgejo は、書く権限を持つ人の、いちばん新しい approve と
// request changes を official にする。ほかの人の権限を読む API（collaborators/{user}/permission）は、repository の admin しか使えないので、使わない。
func (a api) ReviewerCanPush(_ context.Context, _ string, r pullreq.PullReview) (bool, error) {
	return r.Official, nil
}

type statusJSON struct {
	Context     string    `json:"context"`
	Status      string    `json:"status"`
	Description string    `json:"description"`
	Creator     user      `json:"creator"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a api) SetStatus(ctx context.Context, repo, sha string, s pullreq.Status) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, http.MethodPost, rp+"/statuses/"+url.PathEscape(sha),
		map[string]string{"state": s.State, "context": s.Context, "description": s.Description}, nil)
}

func (a api) ListStatuses(ctx context.Context, repo, sha string) ([]pullreq.Status, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	ss, err := list[statusJSON](ctx, a.client, ports.TokenFrom(ctx), rp+"/commits/"+url.PathEscape(sha)+"/statuses")
	if statusOf(err) == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Status, 0, len(ss))
	for _, s := range ss {
		out = append(out, pullreq.Status{Context: s.Context, State: s.Status, Description: s.Description, Creator: s.Creator.Login, CreatedAt: s.CreatedAt})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (a api) labels(ctx context.Context, rp string) ([]labelJSON, error) {
	return list[labelJSON](ctx, a.client, ports.TokenFrom(ctx), rp+"/labels")
}

// EnsureLabel は、ラベルがなければ作る。
func (a api) EnsureLabel(ctx context.Context, repo string, l pullreq.Label) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	have, err := a.labels(ctx, rp)
	if err != nil {
		return err
	}
	for _, x := range have {
		if x.Name == l.Name {
			return nil
		}
	}
	return a.w(ctx, http.MethodPost, rp+"/labels", map[string]string{"name": l.Name, "color": l.Color, "description": l.Description}, nil)
}

// AddLabel は、ラベルを名前で付ける。
func (a api) AddLabel(ctx context.Context, repo string, number int, name string) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, http.MethodPost, fmt.Sprintf("%s/issues/%d/labels", rp, number), map[string][]string{"labels": {name}}, nil)
}

// RemoveLabel は、ラベルを外す。Forgejo は、識別子で外すので、ラベルの一覧から引く。
func (a api) RemoveLabel(ctx context.Context, repo string, number int, name string) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	have, err := a.labels(ctx, rp)
	if err != nil {
		return err
	}
	for _, x := range have {
		if x.Name != name {
			continue
		}
		err := a.w(ctx, http.MethodDelete, fmt.Sprintf("%s/issues/%d/labels/%d", rp, number, x.ID), nil, nil)
		if statusOf(err) == http.StatusNotFound {
			return nil
		}
		return err
	}
	return nil
}

type timelineJSON struct {
	Type      string     `json:"type"`
	Body      string     `json:"body"`
	User      *user      `json:"user"`
	Label     *labelJSON `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
}

// ListEvents は、issue の timeline（GET /issues/{index}/timeline）を読む。pull request も issue として読める。
// ラベルの出来事は、本文が "1" なら付けた、それ以外なら外した、である。
func (a api) ListEvents(ctx context.Context, repo string, number int) ([]pullreq.Event, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	ts, err := list[timelineJSON](ctx, a.client, ports.TokenFrom(ctx), fmt.Sprintf("%s/issues/%d/timeline", rp, number))
	if err != nil {
		return nil, err
	}
	var out []pullreq.Event
	for _, t := range ts {
		e := pullreq.Event{CreatedAt: t.CreatedAt}
		if t.User != nil {
			e.Actor = t.User.Login
		}
		switch t.Type {
		case "label":
			if t.Label == nil {
				continue
			}
			e.Kind, e.Label = "unlabeled", t.Label.Name
			if t.Body == "1" {
				e.Kind = "labeled"
			}
		case "close":
			e.Kind = "closed"
		case "reopen":
			e.Kind = "reopened"
		default:
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

type issueJSON struct {
	Number    int         `json:"number"`
	Title     string      `json:"title"`
	Body      string      `json:"body"`
	State     string      `json:"state"`
	User      user        `json:"user"`
	Labels    []labelJSON `json:"labels"`
	CreatedAt time.Time   `json:"created_at"`
	ClosedAt  *time.Time  `json:"closed_at"`
}

func (i issueJSON) issue() pullreq.Issue {
	is := pullreq.Issue{Number: i.Number, Title: i.Title, Body: i.Body, State: i.State, Author: i.User.Login, Labels: labelNames(i.Labels), CreatedAt: i.CreatedAt}
	if i.ClosedAt != nil {
		is.ClosedAt = *i.ClosedAt
	}
	return is
}

// labelIDs は、ラベルの名前から識別子を求める。ラベルは、先に EnsureLabel で作っておく。
func (a api) labelIDs(ctx context.Context, rp string, names []string) ([]int64, error) {
	have, err := a.labels(ctx, rp)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, n := range names {
		found := false
		for _, l := range have {
			if l.Name == n {
				ids, found = append(ids, l.ID), true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("forgejo: ラベル %q がない", n)
		}
	}
	return ids, nil
}

func (a api) CreateIssue(ctx context.Context, repo, title, body string, labels []string) (pullreq.Issue, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Issue{}, err
	}
	ids, err := a.labelIDs(ctx, rp, labels)
	if err != nil {
		return pullreq.Issue{}, err
	}
	var is issueJSON
	err = a.w(ctx, http.MethodPost, rp+"/issues", map[string]any{"title": title, "body": body, "labels": ids}, &is)
	return is.issue(), err
}

func (a api) ListIssues(ctx context.Context, repo string) ([]pullreq.Issue, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	is, err := list[issueJSON](ctx, a.client, ports.TokenFrom(ctx), rp+"/issues?state=all&type=issues")
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Issue, 0, len(is))
	for _, i := range is {
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
	err = a.w(ctx, http.MethodGet, fmt.Sprintf("%s/issues/%d", rp, number), nil, &is)
	if statusOf(err) == http.StatusNotFound {
		return pullreq.Issue{}, ports.ErrNotFound
	}
	return is.issue(), err
}

func (a api) CloseIssue(ctx context.Context, repo string, number int) error {
	rp, err := repoPath(repo)
	if err != nil {
		return err
	}
	return a.w(ctx, http.MethodPatch, fmt.Sprintf("%s/issues/%d", rp, number), map[string]string{"state": "closed"}, nil)
}

// AttachmentsSupported は、true を返す。Forgejo は、コメントにファイルを添付できる。
func (a api) AttachmentsSupported() bool { return true }

func (a api) UploadAttachment(ctx context.Context, repo string, commentID int64, name string, content []byte) (string, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	var as struct {
		ID int64 `json:"id"`
	}
	if err := a.upload(ctx, ports.TokenFrom(ctx), fmt.Sprintf("%s/issues/comments/%d/assets", rp, commentID), "attachment", name, content, &as); err != nil {
		return "", err
	}
	return strconv.FormatInt(as.ID, 10), nil
}

func (a api) DownloadAttachment(ctx context.Context, repo string, commentID int64, attachmentID string) ([]byte, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	var as struct {
		URL string `json:"browser_download_url"`
	}
	if err := a.w(ctx, http.MethodGet, fmt.Sprintf("%s/issues/comments/%d/assets/%s", rp, commentID, attachmentID), nil, &as); err != nil {
		return nil, err
	}
	return a.download(ctx, ports.TokenFrom(ctx), as.URL)
}
