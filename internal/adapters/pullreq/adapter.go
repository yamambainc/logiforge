package pullreq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

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

// Adapter は、API から、GitForge のポートを実装する。repo の引数は gitforge_ref である。
// GitForge には、ctx の主体のトークン（ports.WithToken）で読み書きする。
type Adapter struct {
	Repos
	API API
	// Now は、今の時刻である。nil なら time.Now。
	Now func() time.Time
}

var _ ports.GitForge = (*Adapter)(nil)

func (a *Adapter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// titlePrefix は、pull request と issue のタイトルの接頭辞である。画面で見分けるためだけに付け、読まない。
const titlePrefix = "[Logiforge] "

// ラベル（design/adapters/pull-requests.md）。
const (
	LabelReviewWaiting = "logiforge/review-waiting"
	LabelFixWaiting    = "logiforge/fix-waiting"
	labelPrefix        = "logiforge/"
)

// waitingLabels は、レビュー待ちと修正待ちのラベルの色と説明である。
var waitingLabels = map[string]Label{
	LabelReviewWaiting: {Name: LabelReviewWaiting, Color: "#1d76db", Description: "Logiforge：レビュー待ち"},
	LabelFixWaiting:    {Name: LabelFixWaiting, Color: "#d93f0b", Description: "Logiforge：修正待ち"},
}

// 本文に書く、人が読む文の頭（design/adapters/texts.md）。バックエンドは、これを読まない。
const (
	promptPrefix = "プロンプトの版："
	closePrefix  = "閉じた理由："
)

// withSentence は、本文の最後に、人が読む文を 1 つ足す。文が空なら、本文をそのまま返す。
func withSentence(body, sentence string) string {
	body = strings.TrimRight(body, "\n")
	if sentence == "" {
		return body
	}
	if body == "" {
		return sentence
	}
	return body + "\n\n" + sentence
}

// nodeTitle は、人への依頼の issue のタイトルである。対象ノードの識別子で始め、そのあとに要約を書く（design/adapters/texts.md の
// 「人への依頼の issue」）。nodeOfTitle は、タイトルの頭から、対象ノードの識別子を読む。
func nodeTitle(node, summary string) string { return node + titleSeparator + summary }

const titleSeparator = " について："

func nodeOfTitle(title string) string {
	node, _, ok := strings.Cut(title, titleSeparator)
	if !ok || strings.ContainsAny(node, " \t") {
		return ""
	}
	return node
}

func number(id string) (int, error) {
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 || strconv.Itoa(n) != id {
		return 0, ports.ErrNotFound
	}
	return n, nil
}

// ---- apply request ----

func waitingOf(labels []string) ports.Waiting {
	r, f := slices.Contains(labels, LabelReviewWaiting), slices.Contains(labels, LabelFixWaiting)
	switch {
	case r && f:
		return ports.WaitingBoth
	case r:
		return ports.WaitingReview
	case f:
		return ports.WaitingFix
	}
	return ports.WaitingNone
}

// toApplyRequest は、pull request の一覧に入っているものだけから apply request を作る。
func toApplyRequest(p Pull) ports.ApplyRequest {
	ar := ports.ApplyRequest{ID: strconv.Itoa(p.Number), Number: p.Number, Branch: p.Head, Target: p.Base, Description: p.Body,
		HeadRevision: p.HeadSHA, Opener: p.Author, Waiting: waitingOf(p.Labels), CreatedAt: p.CreatedAt}
	switch {
	case p.Merged:
		ar.State, ar.MergedRevision = ports.StateMerged, p.MergeCommit
	case p.State == "closed":
		ar.State = ports.StateClosed
	default:
		ar.State = ports.StateOpen
	}
	return ar
}

func (a *Adapter) CreateApplyRequest(ctx context.Context, repo string, n ports.NewApplyRequest) (ports.ApplyRequest, error) {
	pulls, err := a.API.ListPulls(ctx, repo, "open")
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	for _, p := range pulls {
		if p.Head != n.Branch {
			continue
		}
		// 送り直し：同じ主体が作った open の pull request があれば、それを返す（design/api/writes.md の「同じ要求の繰り返し」）。
		if strings.EqualFold(p.Author, n.Opener) {
			return a.GetApplyRequest(ctx, repo, strconv.Itoa(p.Number))
		}
		return ports.ApplyRequest{}, ports.ErrBranchHasOpen
	}
	p, err := a.API.CreatePull(ctx, repo, n.Branch, titlePrefix+n.Branch, n.Description)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	var warnings []string
	if err := a.setWaiting(ctx, repo, p.Number, nil, ports.WaitingReview); err != nil {
		warnings = append(warnings, fmt.Sprintf("レビュー待ちのラベルを付けられない：%v", err))
	}
	ar, err := a.GetApplyRequest(ctx, repo, strconv.Itoa(p.Number))
	ar.Warnings = append(ar.Warnings, warnings...)
	return ar, err
}

// GetApplyRequest は、pull request を 1 件読む。コメントと、レビュー待ちと修正待ちのラベルを最後に付けた時刻も読む。
func (a *Adapter) GetApplyRequest(ctx context.Context, repo, id string) (ports.ApplyRequest, error) {
	num, err := number(id)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	p, err := a.API.GetPull(ctx, repo, num)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	ar := toApplyRequest(p)
	evs, err := a.API.ListEvents(ctx, repo, num)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	for _, e := range evs {
		if e.Kind != "labeled" {
			continue
		}
		switch {
		case e.Label == LabelReviewWaiting && e.CreatedAt.After(ar.ReviewWaitingAt):
			ar.ReviewWaitingAt = e.CreatedAt
		case e.Label == LabelFixWaiting:
			ar.FixWaitingLabeled = append(ar.FixWaitingLabeled, ports.LabelEvent{Actor: e.Actor, At: e.CreatedAt})
		}
	}
	cs, err := a.API.ListComments(ctx, repo, num)
	if err != nil {
		return ports.ApplyRequest{}, err
	}
	ar.Comments = make([]ports.Comment, 0, len(cs))
	for _, c := range cs {
		ar.Comments = append(ar.Comments, ports.Comment{Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	return ar, nil
}

// ListApplyRequests は、pull request の一覧だけから読む。pull request ごとに GitForge を呼ばない（design/backend/list-reads.md）。
func (a *Adapter) ListApplyRequests(ctx context.Context, repo, state string) ([]ports.ApplyRequest, error) {
	if state == "" {
		state = "all"
	}
	pulls, err := a.API.ListPulls(ctx, repo, state)
	if err != nil {
		return nil, err
	}
	out := make([]ports.ApplyRequest, 0, len(pulls))
	for _, p := range pulls {
		out = append(out, toApplyRequest(p))
	}
	return out, nil
}

func (a *Adapter) UpdateDescription(ctx context.Context, repo, id, description string) error {
	num, err := number(id)
	if err != nil {
		return err
	}
	return a.API.EditPullBody(ctx, repo, num, description)
}

func (a *Adapter) Merge(ctx context.Context, repo, id, headSHA, title, message string) (string, error) {
	num, err := number(id)
	if err != nil {
		return "", err
	}
	sha, err := a.API.MergePull(ctx, repo, num, headSHA, title, message)
	if err != nil {
		return "", err
	}
	// ラベルは、反映したら外す。外せなくても、一覧は merged の pull request のラベルを見ないので、反映は成功とする。
	_ = a.setWaiting(ctx, repo, num, nil, ports.WaitingNone)
	return sha, nil
}

func (a *Adapter) Close(ctx context.Context, repo, id, reason string) error {
	num, err := number(id)
	if err != nil {
		return err
	}
	p, err := a.API.GetPull(ctx, repo, num)
	if err != nil {
		return err
	}
	if p.State != "open" {
		return nil
	}
	if _, err := a.API.CreateComment(ctx, repo, num, closePrefix+oneLine(reason)); err != nil {
		return err
	}
	if err := a.API.ClosePull(ctx, repo, num); err != nil {
		return err
	}
	_ = a.setWaiting(ctx, repo, num, p.Labels, ports.WaitingNone)
	return nil
}

// oneLine は、値を 1 行にする。本文の行は 1 行ずつ読むからである。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (a *Adapter) RecordMergeFailure(ctx context.Context, repo, id string, f ports.MergeFailure, w ports.Waiting) error {
	num, err := number(id)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Logiforge：先頭のコミット %s を反映できなかった（%s）。", f.Revision, f.Reason)
	if strings.TrimSpace(f.Detail) != "" {
		text += "\n\n" + f.Detail
	}
	if _, err := a.API.CreateComment(ctx, repo, num, text); err != nil {
		return err
	}
	p, err := a.API.GetPull(ctx, repo, num)
	if err != nil {
		return err
	}
	// 既に付いていても、外して付け直す。付けた出来事が、失敗を書いた主体のものとして残るようにするため
	// （design/adapters/pull-requests.md の「ラベルがずれたとき」）。
	labels := p.Labels
	if want := waitingName[w]; want != "" && slices.Contains(labels, want) {
		if err := a.API.RemoveLabel(ctx, repo, num, want); err != nil {
			return err
		}
		labels = slices.DeleteFunc(slices.Clone(labels), func(l string) bool { return l == want })
	}
	if labels == nil {
		labels = []string{}
	}
	return a.setWaiting(ctx, repo, num, labels, w)
}

func (a *Adapter) SetWaiting(ctx context.Context, repo, id string, w ports.Waiting) error {
	num, err := number(id)
	if err != nil {
		return err
	}
	return a.setWaiting(ctx, repo, num, nil, w)
}

// setWaiting は、レビュー待ちか修正待ちかのラベルを 1 つだけにする。labels が nil なら、pull request を読んで今のラベルを知る。
func (a *Adapter) setWaiting(ctx context.Context, repo string, num int, labels []string, w ports.Waiting) error {
	if labels == nil {
		p, err := a.API.GetPull(ctx, repo, num)
		if err != nil {
			return err
		}
		labels = p.Labels
	}
	want := waitingName[w]
	if want != "" && !slices.Contains(labels, want) {
		if err := a.addLabel(ctx, repo, num, waitingLabels[want]); err != nil {
			return err
		}
	}
	for _, name := range []string{LabelReviewWaiting, LabelFixWaiting} {
		if name != want && slices.Contains(labels, name) {
			if err := a.API.RemoveLabel(ctx, repo, num, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// waitingName は、レビュー待ちか修正待ちかの、ラベルの名前である。
var waitingName = map[ports.Waiting]string{ports.WaitingReview: LabelReviewWaiting, ports.WaitingFix: LabelFixWaiting}

// addLabel は、ラベルがなければ作ってから付ける。GitHub は、ない名前を付けると既定の色で作るからである。
func (a *Adapter) addLabel(ctx context.Context, repo string, num int, l Label) error {
	if err := a.API.EnsureLabel(ctx, repo, l); err != nil {
		return err
	}
	return a.API.AddLabel(ctx, repo, num, l.Name)
}

// ---- レビュー ----

// AddReview は、pull request に review を出す。プロンプトの版があれば、本文の最後に、人が読む文で書く（design/adapters/texts.md の「review の本文」）。
func (a *Adapter) AddReview(ctx context.Context, repo, applyRequestID string, r ports.Review) (ports.Review, error) {
	num, err := number(applyRequestID)
	if err != nil {
		return ports.Review{}, err
	}
	prompt := ""
	if r.PromptRevision != "" && r.PromptRevision != "-" {
		prompt = promptPrefix + oneLine(r.PromptRevision)
	}
	body := withSentence(r.Body, prompt)
	got, err := a.API.CreateReview(ctx, repo, num, r.Kind, body, r.Revision)
	if err != nil {
		return ports.Review{}, err
	}
	return toReview(got), nil
}

func toReview(pr PullReview) ports.Review {
	return ports.Review{ID: strconv.FormatInt(pr.ID, 10), Kind: pr.Kind, Body: pr.Body, Revision: pr.CommitID, Reviewer: pr.Author, CreatedAt: pr.SubmittedAt}
}

// ListReviews は、数える review だけを、出した順に返す（design/adapters/pull-requests.md の「数える review」）。
// 1 件の apply request を読むときだけ呼ぶ。一覧では呼ばない。
func (a *Adapter) ListReviews(ctx context.Context, repo, applyRequestID string) (ports.Reviews, error) {
	num, err := number(applyRequestID)
	if err != nil {
		return ports.Reviews{}, err
	}
	rs, err := a.API.ListReviews(ctx, repo, num)
	if err != nil {
		return ports.Reviews{}, err
	}
	var out ports.Reviews
	for _, pr := range rs {
		if pr.Dismissed {
			continue
		}
		r := toReview(pr)
		// approve と request changes は、書く権限を持つ人のものだけを数える。
		// なぜ request changes も：public の repository では、書く権限のない人も review を出せ、反映を止められてしまうから。
		// comment は、反映の条件に効かないので、だれのものでも数える。
		if r.Kind == ports.ReviewApprove || r.Kind == ports.ReviewRequestChanges {
			// 書く権限を持つかを読めなければ、数えない（閉じる側に倒す）。
			if pr.CanPush == nil {
				out.Warnings = append(out.Warnings, fmt.Sprintf("review %d：%s に書く権限があるかを確かめられないので、この %s は数えない", pr.ID, pr.Author, r.Kind))
				continue
			}
			if !*pr.CanPush {
				out.Warnings = append(out.Warnings, fmt.Sprintf("review %d：%s は書く権限がないので、この %s は数えない", pr.ID, pr.Author, r.Kind))
				continue
			}
		}
		out.Items = append(out.Items, r)
	}
	return out, nil
}

// clockSlack は、merge のコミットの時刻と、pull request の updated_at を付ける時計のずれとして見込む時間である（design/backend/list-reads.md）。
const clockSlack = 5 * time.Minute

// MergedApplyRequest は、反映した pull request の一覧を、更新の新しい順に読み、merge のコミットが sha のものを探す。
// pull request の updated_at が committed より clockSlack 以上前になったら、読むのをやめる。
func (a *Adapter) MergedApplyRequest(ctx context.Context, repo, sha string, committed time.Time) (ports.ApplyRequest, error) {
	if now := a.now(); committed.IsZero() || committed.After(now) {
		committed = now
	}
	stop := committed.Add(-clockSlack)
	for page := 1; ; page++ {
		pulls, more, err := a.API.ClosedPullsPage(ctx, repo, page)
		if err != nil {
			return ports.ApplyRequest{}, err
		}
		done := false
		for _, p := range pulls {
			if p.Merged && p.MergeCommit == sha && p.Base == "main" {
				return toApplyRequest(p), nil
			}
			if p.UpdatedAt.Before(stop) {
				done = true
			}
		}
		if done || !more || len(pulls) == 0 {
			return ports.ApplyRequest{}, ports.ErrNotFound
		}
	}
}

// ---- 人への依頼 ----

var humanKinds = []ports.HumanRequestKind{ports.HumanRequestPVR, ports.HumanRequestDecision}

// dirLabel は、ディレクトリの repository の依頼に付けるラベルである（design/directory/README.md の「人への依頼」）。
func dirLabel(ref string) string {
	if _, dir := ports.SplitRef(ref); dir != "" {
		return labelPrefix + dir
	}
	return ""
}

// humanKindOf は、issue が、ref の repository の人への依頼なら、その種類を返す。
func humanKindOf(is Issue, ref string) (ports.HumanRequestKind, bool) {
	if d := dirLabel(ref); d != "" && !slices.Contains(is.Labels, d) {
		return "", false
	}
	for _, k := range humanKinds {
		if slices.Contains(is.Labels, labelPrefix+string(k)) {
			return k, true
		}
	}
	return "", false
}

// toHumanRequest は、issue の一覧に入っているものだけから、人への依頼を作る。回答は、閉じた時刻だけを入れる。
func toHumanRequest(is Issue, kind ports.HumanRequestKind) ports.HumanRequest {
	hr := ports.HumanRequest{ID: strconv.Itoa(is.Number), Number: is.Number, Kind: kind, TargetNode: nodeOfTitle(is.Title), Body: is.Body,
		Requester: is.Author, CreatedAt: is.CreatedAt}
	if hr.TargetNode == "" {
		hr.Warnings = append(hr.Warnings, fmt.Sprintf("issue #%d のタイトルが、対象ノードの識別子と「%s」で始まらない", is.Number, strings.TrimSpace(titleSeparator)))
	}
	if is.State == "closed" {
		hr.Answer = &ports.Answer{CreatedAt: is.ClosedAt}
	}
	return hr
}

// resendWindow は、人への依頼の起票の送り直しとみなす時間である（design/api/writes.md の「同じ要求の繰り返し」）。
const resendWindow = 10 * time.Minute

func (a *Adapter) CreateHumanRequest(ctx context.Context, repo string, n ports.NewHumanRequest) (ports.HumanRequest, error) {
	summary := "人への依頼"
	if first, _, _ := strings.Cut(strings.TrimSpace(n.Body), "\n"); first != "" {
		summary = truncate(first, 60)
	}
	title := nodeTitle(n.TargetNode, summary)
	body := strings.TrimRight(n.Body, "\n")
	// 送り直し：同じ主体が、同じタイトルと本文で、少し前に作った open の issue があれば、それを返す。
	issues, err := a.API.ListIssues(ctx, repo)
	if err != nil {
		return ports.HumanRequest{}, err
	}
	for _, is := range issues {
		if _, ok := humanKindOf(is, repo); ok && is.State == "open" && strings.EqualFold(is.Author, n.Requester) && is.Title == title &&
			strings.TrimRight(is.Body, "\n") == body && a.now().Sub(is.CreatedAt) < resendWindow {
			return a.GetHumanRequest(ctx, repo, strconv.Itoa(is.Number))
		}
	}
	labels := []string{labelPrefix + string(n.Kind)}
	if d := dirLabel(repo); d != "" {
		labels = append(labels, d)
	}
	for _, name := range labels {
		if err := a.API.EnsureLabel(ctx, repo, Label{Name: name, Color: "#fbca04", Description: "Logiforge：人への依頼"}); err != nil {
			return ports.HumanRequest{}, err
		}
	}
	is, err := a.API.CreateIssue(ctx, repo, title, body, labels)
	if err != nil {
		return ports.HumanRequest{}, err
	}
	return a.GetHumanRequest(ctx, repo, strconv.Itoa(is.Number))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// answered は、閉じた issue の回答のコメントである。添付は、コメントの添付である（名前 → GitForge の添付の識別子）。
type answered struct {
	comment Comment
	body    string
}

// readAnswer は、閉じた issue の回答を読む。回答は、issue を閉じた人が、閉じる前に書いた最後のコメントである（design/adapters/texts.md の「回答」）。
func (a *Adapter) readAnswer(ctx context.Context, repo string, is Issue) (*answered, string, error) {
	evs, err := a.API.ListEvents(ctx, repo, is.Number)
	if err != nil {
		return nil, "", err
	}
	closer := ""
	for _, e := range evs {
		if e.Kind == "closed" {
			closer = e.Actor
		}
	}
	comments, err := a.API.ListComments(ctx, repo, is.Number)
	if err != nil {
		return nil, "", err
	}
	var found *Comment
	for i, c := range comments {
		if strings.EqualFold(c.Author, closer) && (is.ClosedAt.IsZero() || !c.CreatedAt.After(is.ClosedAt.Add(time.Minute))) {
			found = &comments[i]
		}
	}
	if found == nil {
		return nil, closer, nil
	}
	return &answered{comment: *found, body: normalize(found.Body)}, closer, nil
}

// attachmentSHAs は、回答のコメントの添付を読み、名前ごとの sha256 を求める。添付の中身を読むのは、1 件の読みのときだけである。
func (a *Adapter) attachmentSHAs(ctx context.Context, repo string, c Comment) (map[string]string, error) {
	out := map[string]string{}
	for name, aid := range c.Attachments {
		data, err := a.API.DownloadAttachment(ctx, repo, c.ID, aid)
		if err != nil {
			return nil, err
		}
		out[name] = sha256Hex(data)
	}
	return out, nil
}

func (a *Adapter) getIssue(ctx context.Context, repo, id string) (Issue, ports.HumanRequestKind, error) {
	num, err := number(id)
	if err != nil {
		return Issue{}, "", err
	}
	is, err := a.API.GetIssue(ctx, repo, num)
	if err != nil {
		return Issue{}, "", err
	}
	kind, ok := humanKindOf(is, repo)
	if !ok {
		return Issue{}, "", ports.ErrNotFound
	}
	return is, kind, nil
}

func (a *Adapter) GetHumanRequest(ctx context.Context, repo, id string) (ports.HumanRequest, error) {
	is, kind, err := a.getIssue(ctx, repo, id)
	if err != nil {
		return ports.HumanRequest{}, err
	}
	hr := toHumanRequest(is, kind)
	if is.State != "closed" {
		return hr, nil
	}
	ans, closer, err := a.readAnswer(ctx, repo, is)
	if err != nil {
		return ports.HumanRequest{}, err
	}
	if ans == nil {
		hr.Warnings = append(hr.Warnings, "issue は閉じているが、閉じた人の回答のコメントがない")
		hr.Answer.AnsweredBy = closer
		return hr, nil
	}
	hr.Answer = &ports.Answer{Body: ans.body, AnsweredBy: ans.comment.Author, CreatedAt: is.ClosedAt}
	if hr.Answer.CreatedAt.IsZero() {
		hr.Answer.CreatedAt = ans.comment.CreatedAt
	}
	shas, err := a.attachmentSHAs(ctx, repo, ans.comment)
	if err != nil {
		return ports.HumanRequest{}, err
	}
	names := make([]string, 0, len(shas))
	for n := range shas {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		hr.Answer.Attachments = append(hr.Answer.Attachments, ports.Attachment{Name: name, SHA256: shas[name], ID: ans.comment.Attachments[name]})
	}
	return hr, nil
}

// ListHumanRequests は、issue の一覧だけから読む。issue ごとに GitForge を呼ばない（design/backend/list-reads.md）。
func (a *Adapter) ListHumanRequests(ctx context.Context, repo string) ([]ports.HumanRequest, error) {
	issues, err := a.API.ListIssues(ctx, repo)
	if err != nil {
		return nil, err
	}
	var out []ports.HumanRequest
	for _, is := range issues {
		if kind, ok := humanKindOf(is, repo); ok {
			out = append(out, toHumanRequest(is, kind))
		}
	}
	return out, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func normalize(body string) string {
	return strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
}

// Answer は、回答のコメントを書き、添付を上げ、issue を閉じる（design/backend/human-requests.md の「回答」）。
// 途中で止まったときは、同じ本文・回答者・添付で呼び直せば、書き直さずに続きから行う。違う回答がすでにあれば ErrAlreadyAnswered を返す。
func (a *Adapter) Answer(ctx context.Context, repo, id, body, answeredBy string, files []ports.NewAttachment) error {
	// 添付を持てない GitForge では、コメントを書く前に断る。
	if len(files) > 0 && !a.API.AttachmentsSupported() {
		return ports.ErrAttachmentsUnsupported
	}
	is, _, err := a.getIssue(ctx, repo, id)
	if err != nil {
		return err
	}
	var names []string
	shas := map[string]string{}
	for _, file := range files {
		if slices.Contains(names, file.Name) {
			return fmt.Errorf("pullreq: 添付の名前 %q が重なっている", file.Name)
		}
		names = append(names, file.Name)
		shas[file.Name] = sha256Hex(file.Content)
	}
	// same は、回答のコメントが、この要求と同じ回答かを返す。添付は、中身の sha256 で比べる（送り直しのときだけ読む）。
	same := func(ans *answered) (bool, error) {
		if ans == nil || ans.body != normalize(body) || !strings.EqualFold(ans.comment.Author, answeredBy) || len(ans.comment.Attachments) > len(shas) {
			return false, nil
		}
		for n := range ans.comment.Attachments {
			if _, ok := shas[n]; !ok {
				return false, nil
			}
		}
		got, err := a.attachmentSHAs(ctx, repo, ans.comment)
		if err != nil {
			return false, err
		}
		for n, s := range got {
			if shas[n] != s {
				return false, nil
			}
		}
		return true, nil
	}
	if is.State == "closed" {
		ans, _, err := a.readAnswer(ctx, repo, is)
		if err != nil {
			return err
		}
		ok, err := same(ans)
		if err != nil {
			return err
		}
		if !ok || len(ans.comment.Attachments) != len(shas) {
			return ports.ErrAlreadyAnswered
		}
		return nil
	}
	// コメントを書いたあと、閉じる前に止まった送り直しなら、書き直さない。添付は、まだ上がっていない分だけ上げる。
	comments, err := a.API.ListComments(ctx, repo, is.Number)
	if err != nil {
		return err
	}
	var ans *answered
	for _, c := range comments {
		if !strings.EqualFold(c.Author, answeredBy) || normalize(c.Body) != normalize(body) {
			continue
		}
		x := &answered{comment: c, body: normalize(c.Body)}
		ok, err := same(x)
		if err != nil {
			return err
		}
		if ok {
			ans = x
		}
	}
	if ans == nil {
		c, err := a.API.CreateComment(ctx, repo, is.Number, body)
		if err != nil {
			return err
		}
		ans = &answered{comment: c, body: normalize(c.Body)}
	}
	for _, file := range files {
		if ans.comment.Attachments[file.Name] != "" {
			continue
		}
		if _, err := a.API.UploadAttachment(ctx, repo, ans.comment.ID, file.Name, file.Content); err != nil {
			return err
		}
	}
	return a.API.CloseIssue(ctx, repo, is.Number)
}

// ReadAttachment は、回答の添付を sha256 で引いて読み、中身の sha256 を確かめる。
func (a *Adapter) ReadAttachment(ctx context.Context, repo, humanRequestID, sha string) ([]byte, error) {
	is, _, err := a.getIssue(ctx, repo, humanRequestID)
	if err != nil {
		return nil, err
	}
	if is.State != "closed" {
		return nil, ports.ErrNotFound
	}
	ans, _, err := a.readAnswer(ctx, repo, is)
	if err != nil {
		return nil, err
	}
	if ans == nil {
		return nil, ports.ErrNotFound
	}
	// 添付の sha256 は、中身から求める。名前の順に読み、合うものを返す。
	names := make([]string, 0, len(ans.comment.Attachments))
	for n := range ans.comment.Attachments {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := a.API.DownloadAttachment(ctx, repo, ans.comment.ID, ans.comment.Attachments[name])
		if err != nil {
			return nil, err
		}
		if sha256Hex(data) == sha {
			return data, nil
		}
	}
	return nil, ports.ErrNotFound
}
