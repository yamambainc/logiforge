// Package pullreq は、GitForge のポートを、pull request と issue とコメントで実装する、GitHub と Forgejo に共通の部分である
// （design/adapters/pull-requests.md）。GitForge ごとの REST の呼び方は、API インターフェースで差し替える。
package pullreq

import (
	"context"
	"errors"
	"time"
)

// API は、GitForge ごとの REST の呼び方である。repo は、gitforge_ref（`owner/name`）である。
// 一覧は、作った順（番号の順）に返す。
type API interface {
	// WriterLogin は、書き込み主体のログイン名である。
	WriterLogin() string

	CreatePull(ctx context.Context, repo, head, title, body string) (Pull, error)
	GetPull(ctx context.Context, repo string, number int) (Pull, error)
	ListPulls(ctx context.Context, repo string) ([]Pull, error)
	EditPullBody(ctx context.Context, repo string, number int, body string) error
	ClosePull(ctx context.Context, repo string, number int) error
	// MergePull は、squash merge する。head が headSHA でなければ ErrHeadMoved（ports）、衝突なら ErrConflict（ports）を返す。
	MergePull(ctx context.Context, repo string, number int, headSHA, title, message string) (string, error)

	// コメントは、pull request と issue で共通である（番号で指す）。
	CreateComment(ctx context.Context, repo string, number int, body string) (Comment, error)
	ListComments(ctx context.Context, repo string, number int) ([]Comment, error)
	ListRepoComments(ctx context.Context, repo string) ([]Comment, error)

	CreateReview(ctx context.Context, repo string, number int, commitSHA, body string) error
	ListReviews(ctx context.Context, repo string, number int) ([]PullReview, error)

	CreateIssue(ctx context.Context, repo, title, body string, labels []string) (Issue, error)
	ListIssues(ctx context.Context, repo string) ([]Issue, error)
	GetIssue(ctx context.Context, repo string, number int) (Issue, error)
	CloseIssue(ctx context.Context, repo string, number int) error

	// UploadAttachment は、コメントにファイルを添付する。添付を持てない GitForge は ErrAttachmentsUnsupported を返す。
	UploadAttachment(ctx context.Context, repo string, commentID int64, name string, content []byte) (string, error)
	DownloadAttachment(ctx context.Context, repo string, commentID int64, attachmentID string) ([]byte, error)
}

// ErrAttachmentsUnsupported は、コメントにファイルを添付できない GitForge であることを表す。
var ErrAttachmentsUnsupported = errors.New("この GitForge は、コメントにファイルを添付できない")

// Pull は、pull request である。
type Pull struct {
	Number    int
	Title     string
	Body      string
	Head      string // ブランチの名前
	HeadSHA   string
	State     string // open か closed
	Merged    bool
	Author    string
	CreatedAt time.Time
}

// Comment は、pull request か issue のコメントである。
type Comment struct {
	ID          int64
	IssueNumber int
	Body        string
	Author      string
	CreatedAt   time.Time
	// Attachments は、コメントの添付の名前から、GitForge の側の識別子を引く。
	Attachments map[string]string
}

// PullReview は、pull request のレビューである。
type PullReview struct {
	Body      string
	CommitSHA string
	Author    string
	CreatedAt time.Time
}

// Issue は、issue である。pull request は含めない。
type Issue struct {
	Number    int
	Title     string
	Body      string
	State     string
	Author    string
	CreatedAt time.Time
}
