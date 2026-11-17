package github

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

// api は、pullreq.API を GitHub で実装する。ctx の主体のトークン（ports.WithToken）で呼ぶ。
type api struct{ *forge }

var _ pullreq.API = api{}

type user struct {
	Login string `json:"login"`
}

type pullJSON struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	Merged    bool      `json:"merged"`
	MergedAt  *string   `json:"merged_at"`
	Mergeable *bool     `json:"mergeable"`
	MergeSHA  string    `json:"merge_commit_sha"`
	User      user      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
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
		State: p.State, Merged: p.merged(), Base: p.Base.Ref, Author: p.User.Login, CreatedAt: p.CreatedAt}
	if pr.Merged {
		pr.MergeCommit = p.MergeSHA
	}
	return pr
}

// RepoID は、GitHub の repository の node_id を返す。名前を変えても、owner を移しても変わらない。
func (a api) RepoID(ctx context.Context, repo string) (string, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return "", err
	}
	var r struct {
		NodeID string `json:"node_id"`
	}
	if err := a.w(ctx, repo, http.MethodGet, rp, nil, &r); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return "", ports.ErrNotFound
		}
		return "", err
	}
	return "github:" + r.NodeID, nil
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

func (a api) ListPulls(ctx context.Context, repo string) ([]pullreq.Pull, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	tok := ports.TokenFrom(ctx)
	ps, err := list[pullJSON](ctx, a.c, tok, rp+"/pulls?state=all&sort=created&direction=asc")
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
	UpdatedAt time.Time `json:"updated_at"`
	IssueURL  string    `json:"issue_url"`
}

func (c commentJSON) comment() pullreq.Comment {
	out := pullreq.Comment{ID: c.ID, Body: c.Body, Author: c.User.Login, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if i := strings.LastIndex(c.IssueURL, "/"); i >= 0 {
		out.IssueNumber, _ = strconv.Atoi(c.IssueURL[i+1:])
	}
	return out
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
	out := c.comment()
	out.IssueNumber = number
	return out, nil
}

func (a api) ListComments(ctx context.Context, repo string, number int) ([]pullreq.Comment, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	tok := ports.TokenFrom(ctx)
	cs, err := list[commentJSON](ctx, a.c, tok, fmt.Sprintf("%s/issues/%d/comments", rp, number))
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
	tok := ports.TokenFrom(ctx)
	cs, err := list[commentJSON](ctx, a.c, tok, rp+"/issues/comments?sort=created&direction=asc")
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.comment())
	}
	return out, nil
}

// MergedPull は、sha を merge のコミットにした、merge 済みで向き先が main の pull request を返す（design/backend/verify.md の「GitForge の API」）。
// GET /commits/{sha}/pulls は、そのコミットを含む pull request を返す。merge のコミットが sha のものだけを選ぶ。
func (a api) MergedPull(ctx context.Context, repo, sha string) (pullreq.Pull, bool, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return pullreq.Pull{}, false, err
	}
	ps, err := list[pullJSON](ctx, a.c, ports.TokenFrom(ctx), rp+"/commits/"+url.PathEscape(sha)+"/pulls")
	if err != nil {
		if s := statusOf(err); s == http.StatusNotFound || s == http.StatusUnprocessableEntity {
			return pullreq.Pull{}, false, nil
		}
		return pullreq.Pull{}, false, err
	}
	for _, p := range ps {
		if p.merged() && p.MergeSHA == sha && p.Base.Ref == "main" {
			return p.pull(), true, nil
		}
	}
	return pullreq.Pull{}, false, nil
}

type issueJSON struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	User        user      `json:"user"`
	CreatedAt   time.Time `json:"created_at"`
	PullRequest *struct{} `json:"pull_request"`
}

func (i issueJSON) issue() pullreq.Issue {
	return pullreq.Issue{Number: i.Number, Title: i.Title, Body: i.Body, State: i.State, Author: i.User.Login, CreatedAt: i.CreatedAt}
}

// CreateIssue は、issue を作る。ラベルは名前で渡す。GitHub は、ないラベルを作って付ける。ラベルは表示のためだけに付ける。
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
	tok := ports.TokenFrom(ctx)
	is, err := list[issueJSON](ctx, a.c, tok, rp+"/issues?state=all&sort=created&direction=asc")
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
