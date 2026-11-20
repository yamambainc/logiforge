// Package pullreq は、GitForge のポートを、pull request、review、issue、ラベル、commit status で実装する、
// GitHub と Forgejo に共通の部分である（design/adapters/pull-requests.md）。GitForge ごとの REST の呼び方は、API インターフェースで差し替える。
package pullreq

import (
	"context"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

// API は、GitForge ごとの REST の呼び方である。repo は、gitforge_ref の git リポジトリの部分（`owner/name`）である。
// 一覧は、作った順（番号の順）に返す。
type API interface {
	CreatePull(ctx context.Context, repo, head, title, body string) (Pull, error)
	GetPull(ctx context.Context, repo string, number int) (Pull, error)
	// ListPulls は、state（open、closed、all）の pull request を、番号の順に返す。
	ListPulls(ctx context.Context, repo, state string) ([]Pull, error)
	// ClosedPullsPage は、closed の pull request を、更新の新しい順に、page（1 から）のページだけ返す。more は、次のページがあるか。
	ClosedPullsPage(ctx context.Context, repo string, page int) (pulls []Pull, more bool, err error)
	EditPullBody(ctx context.Context, repo string, number int, body string) error
	ClosePull(ctx context.Context, repo string, number int) error
	// MergePull は、squash merge する。head が headSHA でなければ ErrHeadMoved（ports）、衝突なら ErrConflict（ports）を返す。
	MergePull(ctx context.Context, repo string, number int, headSHA, title, message string) (string, error)

	// ListReviews は、pull request の、出された review を返す（pending は除く）。
	ListReviews(ctx context.Context, repo string, number int) ([]PullReview, error)
	// CreateReview は、review を出す。event は approve、request_changes、comment のどれか（ports.ReviewKind）。
	CreateReview(ctx context.Context, repo string, number int, event ports.ReviewKind, body, commitID string) (PullReview, error)
	// ReviewerCanPush は、review を書いた人が、repository に書く権限を持つかを返す（design/adapters/pull-requests.md の「数える review」）。
	ReviewerCanPush(ctx context.Context, repo string, r PullReview) (bool, error)

	// SetStatus は、コミットに commit status を立てる。ListStatuses は、そのコミットの commit status を、新しい順に返す。
	SetStatus(ctx context.Context, repo, sha string, s Status) error
	ListStatuses(ctx context.Context, repo, sha string) ([]Status, error)

	// EnsureLabel は、ラベルがなければ作る。AddLabel と RemoveLabel は、pull request か issue に付け外しする。
	EnsureLabel(ctx context.Context, repo string, l Label) error
	AddLabel(ctx context.Context, repo string, number int, name string) error
	RemoveLabel(ctx context.Context, repo string, number int, name string) error
	// ListEvents は、pull request か issue の出来事（ラベルの付け外し、close、reopen）を、古い順に返す。
	ListEvents(ctx context.Context, repo string, number int) ([]Event, error)

	// コメントは、pull request と issue で共通である（番号で指す）。
	CreateComment(ctx context.Context, repo string, number int, body string) (Comment, error)
	ListComments(ctx context.Context, repo string, number int) ([]Comment, error)

	CreateIssue(ctx context.Context, repo, title, body string, labels []string) (Issue, error)
	// ListIssues は、issue を番号の順に返す。pull request は含めない。
	ListIssues(ctx context.Context, repo string) ([]Issue, error)
	GetIssue(ctx context.Context, repo string, number int) (Issue, error)
	CloseIssue(ctx context.Context, repo string, number int) error
	// EditIssueBody は、issue の本文を書き換える。
	EditIssueBody(ctx context.Context, repo string, number int, body string) error

	// AttachmentsSupported は、コメントにファイルを添付できるかを返す。できなければ、回答の添付はリンクだけである。
	AttachmentsSupported() bool
	// UploadAttachment は、コメントにファイルを添付する。添付を持てない GitForge は ErrAttachmentsUnsupported を返す。
	UploadAttachment(ctx context.Context, repo string, commentID int64, name string, content []byte) (string, error)
	DownloadAttachment(ctx context.Context, repo string, commentID int64, attachmentID string) ([]byte, error)
}

// ErrAttachmentsUnsupported は、コメントにファイルを添付できない GitForge であることを表す。
var ErrAttachmentsUnsupported = ports.ErrAttachmentsUnsupported

// Pull は、pull request である。
type Pull struct {
	Number  int
	Title   string
	Body    string
	Head    string // ブランチの名前
	HeadSHA string
	State   string // open か closed
	Merged  bool
	// Base は、向き先のブランチの名前（pull request の base）である。
	Base string
	// MergeCommit は、merge したときの、merge のコミットの SHA である。merge していなければ空。
	MergeCommit string
	Author      string
	Labels      []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PullReview は、pull request の review である。
type PullReview struct {
	ID       int64
	Kind     ports.ReviewKind
	Body     string
	Author   string
	CommitID string
	// Dismissed は、dismiss されたか。
	Dismissed bool
	// Association は、GitHub の author_association。Official は、Forgejo の official（書く権限を持つ人の review か）。
	Association string
	Official    bool
	SubmittedAt time.Time
}

// Status は、commit status である。
type Status struct {
	Context     string
	State       string // success、failure、pending、error
	Description string
	Creator     string
	CreatedAt   time.Time
}

// Label は、ラベルである。
type Label struct {
	Name        string
	Color       string // #rrggbb
	Description string
}

// Event は、pull request か issue の出来事である。
type Event struct {
	// Kind は、labeled、unlabeled、closed、reopened のどれか。
	Kind      string
	Label     string
	Actor     string
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

// Issue は、issue である。pull request は含めない。
type Issue struct {
	Number    int
	Title     string
	Body      string
	State     string
	Author    string
	Labels    []string
	CreatedAt time.Time
	ClosedAt  time.Time
}
