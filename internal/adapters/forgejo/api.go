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

type pullJSON struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	// Mergeable は、Forgejo が merge できるかを確かめた結果。
	Mergeable      bool      `json:"mergeable"`
	MergeCommitSHA string    `json:"merge_commit_sha"`
	User           user      `json:"user"`
	CreatedAt      time.Time `json:"created_at"`
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
		State: p.State, Merged: p.Merged, Base: p.Base.Ref, Author: p.User.Login, CreatedAt: p.CreatedAt}
	if p.Merged {
		pr.MergeCommit = p.MergeCommitSHA
	}
	return pr
}

// RepoID は、Forgejo の repository の id を返す。名前を変えても、organization を移しても変わらない。
func (a api) RepoID(ctx context.Context, repo string) (string, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	var r struct {
		ID int64 `json:"id"`
	}
	if err := a.w(ctx, http.MethodGet, rp, nil, &r); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return "", ports.ErrNotFound
		}
		return "", err
	}
	return "forgejo:" + strconv.FormatInt(r.ID, 10), nil
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

func (a api) ListPulls(ctx context.Context, repo string) ([]pullreq.Pull, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	ps, err := list[pullJSON](ctx, a.client, ports.TokenFrom(ctx), rp+"/pulls?state=all&sort=oldest")
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
	ID             int64     `json:"id"`
	Body           string    `json:"body"`
	User           user      `json:"user"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	IssueURL       string    `json:"issue_url"`
	PullRequestURL string    `json:"pull_request_url"`
	Assets         []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"assets"`
}

func (c commentJSON) comment() pullreq.Comment {
	out := pullreq.Comment{ID: c.ID, Body: c.Body, Author: c.User.Login, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	u := c.IssueURL
	if u == "" {
		u = c.PullRequestURL
	}
	if i := strings.LastIndex(u, "/"); i >= 0 {
		out.IssueNumber, _ = strconv.Atoi(u[i+1:])
	}
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
	out := c.comment()
	out.IssueNumber = number
	return out, nil
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
		x := c.comment()
		x.IssueNumber = number
		out = append(out, x)
	}
	return out, nil
}

func (a api) ListRepoComments(ctx context.Context, repo string) ([]pullreq.Comment, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	cs, err := list[commentJSON](ctx, a.client, ports.TokenFrom(ctx), rp+"/issues/comments")
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.comment())
	}
	return out, nil
}

type issueJSON struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	User      user      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

func (i issueJSON) issue() pullreq.Issue {
	return pullreq.Issue{Number: i.Number, Title: i.Title, Body: i.Body, State: i.State, Author: i.User.Login, CreatedAt: i.CreatedAt}
}

// labelIDs は、ラベルの名前から識別子を求める。なければ作る。ラベルは表示のためだけに付ける。
func (a api) labelIDs(ctx context.Context, rp string, names []string) ([]int64, error) {
	type labelJSON struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	have, err := list[labelJSON](ctx, a.client, ports.TokenFrom(ctx), rp+"/labels")
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
			var l labelJSON
			if err := a.w(ctx, http.MethodPost, rp+"/labels", map[string]string{"name": n, "color": "#5319e7"}, &l); err != nil {
				return nil, err
			}
			ids = append(ids, l.ID)
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

// MergedPull は、sha を merge のコミットにした、向き先が main の pull request を返す（design/backend/verify.md の「GitForge の API」）。
// GET /repos/{owner}/{repo}/commits/{sha}/pull は、そのコミットを merge した pull request を返し、なければ 404 を返す。
func (a api) MergedPull(ctx context.Context, repo, sha string) (pullreq.Pull, bool, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Pull{}, false, err
	}
	var p pullJSON
	if err := a.do(ctx, ports.TokenFrom(ctx), http.MethodGet, rp+"/commits/"+url.PathEscape(sha)+"/pull", nil, &p); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return pullreq.Pull{}, false, nil
		}
		return pullreq.Pull{}, false, err
	}
	if !p.Merged || p.MergeCommitSHA != sha || p.Base.Ref != "main" {
		return pullreq.Pull{}, false, nil
	}
	return p.pull(), true, nil
}
