// Package ports は、バックエンドのポート（design/backend/ports/）の Go のインターフェースである。
// アダプタは、このインターフェースを実装する。バックエンドは、アダプタを知らない。
package ports

import (
	"context"
	"errors"
	"time"
)

// ポートの誤り。アダプタは、これを errors.Is で見分けられる形で返す。
var (
	ErrNotFound         = errors.New("not found")
	ErrMainExists       = errors.New("main がすでにある")
	ErrBranchHasOpen    = errors.New("そのブランチに open の提案がすでにある")
	ErrHeadMoved        = errors.New("pull request の head が、指定した SHA でない")
	ErrConflict         = errors.New("git の衝突で merge できない")
	ErrAlreadyAnswered  = errors.New("すでに回答がある")
	ErrUnauthenticated  = errors.New("この主体は、Logiforge の主体でない")
	ErrAttachmentTamper = errors.New("添付の sha256 が、記録と合わない")
)

// Repository は、repository の名前と、GitForge の側の参照の組である。
type Repository struct {
	Name        string
	GitForgeRef string
}

// File は、repository を用意するときに、最初のコミットに入れるファイルである。
type File struct {
	Path    string
	Content []byte
}

// State は、提案の状態である。
type State string

const (
	StateOpen   State = "open"
	StateMerged State = "merged"
	StateClosed State = "closed"
)

// ApplyRequest は、GitForge から読んだ提案である。
// head のブランチが消えた open の提案を closed として扱うのは、ブランチの履歴を読むバックエンドである。
type ApplyRequest struct {
	ID     string
	Number int
	Branch string
	// Target は、向き先のブランチ（pull request の base）である。
	Target        string
	Description   string
	FirstRevision string
	HeadRevision  string
	State         State
	// Opener は、提案を作った主体である（バックエンドを通した記録の値）。
	Opener string
	// Kind は、直すための apply request の種類（repair.revert か repair.accept）。ふつうの apply request では空。
	Kind         string
	CloseReason  string
	CloseSession string
	// MergeFailures は、反映の失敗の記録である（古い順）。
	MergeFailures []MergeFailure
	CreatedAt     time.Time
	// MergedRevision は、反映したときの merge のコミットの SHA である。反映していなければ空。
	MergedRevision string
	// Warnings は、数えなかった記録（MAC が合わない、など）である。
	Warnings []string
}

// MergeFailure は、反映の失敗の記録である。
type MergeFailure struct {
	// Revision は、反映しようとした先頭のコミット。
	Revision string
	// Reason は、stale_target、invariant_violation、conflict、snapshot_pending のどれか。
	Reason string
	Detail string
}

// NewApplyRequest は、提案を作るときに渡すものである。
type NewApplyRequest struct {
	ID string
	// Kind は、直すための apply request の種類（repair.revert か repair.accept）。ふつうの apply request では空。
	Kind          string
	Branch        string
	Description   string
	FirstRevision string
	Opener        string
}

// ReviewKind は、レビューの種類である。
type ReviewKind string

const (
	ReviewApprove        ReviewKind = "approve"
	ReviewRequestChanges ReviewKind = "request_changes"
	ReviewComment        ReviewKind = "comment"
)

// Review は、レビューである。
type Review struct {
	ID             string
	Kind           ReviewKind
	Body           string
	Revision       string
	Reviewer       string
	Session        string
	PromptRevision string
	CreatedAt      time.Time
}

// Reviews は、提案のレビューの一覧である。
type Reviews struct {
	// Items は、数えたレビューを、記録した順に並べたもの。
	Items []Review
	// ChainBroken は、レビューの鎖が切れていること（数えないレビューがある、など）である。切れていれば、反映しない。
	ChainBroken bool
	Warnings    []string
}

// HumanRequestKind は、人への依頼の種類である。
type HumanRequestKind string

const (
	HumanRequestPVR      HumanRequestKind = "pvr"
	HumanRequestDecision HumanRequestKind = "decision"
)

// HumanRequest は、人への依頼である。
type HumanRequest struct {
	ID         string
	Number     int
	Kind       HumanRequestKind
	TargetNode string
	Body       string
	Requester  string
	Session    string
	CreatedAt  time.Time
	// Answer は、回答。まだなければ nil。
	Answer   *Answer
	Warnings []string
}

// NewHumanRequest は、人への依頼を起票するときに渡すものである。
type NewHumanRequest struct {
	ID         string
	Kind       HumanRequestKind
	TargetNode string
	Body       string
	Requester  string
	Session    string
}

// Answer は、人への依頼の回答である。
type Answer struct {
	Body        string
	AnsweredBy  string
	Attachments []Attachment
	CreatedAt   time.Time
}

// Attachment は、回答の添付である。
type Attachment struct {
	Name   string
	SHA256 string
	// ID は、GitForge の側の添付の識別子。添付を持てない GitForge では空。
	ID string
}

// NewAttachment は、回答に添えるファイルである。
type NewAttachment struct {
	Name    string
	Content []byte
}

// GitForge は、GitForge のポートである（design/backend/ports/gitforge.md）。
type GitForge interface {
	ListRepositories(ctx context.Context) ([]Repository, error)
	GitForgeRef(ctx context.Context, name string) (string, error)
	// GitURL は、gitforge_ref から、クライアントが clone と push に使う git の URL を返す。
	GitURL(gitForgeRef string) string
	// MainHead は、GitForge の今の main の SHA を返す。反映の前に、worker が追いついているかを確かめるのに使う。
	MainHead(ctx context.Context, gitForgeRef string) (string, error)
	// SetupRepository は、repository を用意する。用意が途中で止まっていれば、続きから行う。
	// main がすでにあり、用意が終わっていれば、ErrMainExists を返す。
	SetupRepository(ctx context.Context, gitForgeRef string, files []File, message string) error

	CreateApplyRequest(ctx context.Context, repo string, ar NewApplyRequest) (ApplyRequest, error)
	GetApplyRequest(ctx context.Context, repo, id string) (ApplyRequest, error)
	ListApplyRequests(ctx context.Context, repo string) ([]ApplyRequest, error)
	UpdateDescription(ctx context.Context, repo, id, description string) error
	// Merge は、head の SHA を指定して squash merge する。ErrHeadMoved か ErrConflict を返しうる。
	// 戻り値は、反映のコミットの SHA である。
	Merge(ctx context.Context, repo, id, headSHA, title, message string) (string, error)
	Close(ctx context.Context, repo, id, reason, session string) error
	RecordMergeFailure(ctx context.Context, repo, id string, f MergeFailure) error

	AddReview(ctx context.Context, repo, applyRequestID string, r Review) error
	ListReviews(ctx context.Context, repo, applyRequestID string) (Reviews, error)

	CreateHumanRequest(ctx context.Context, repo string, hr NewHumanRequest) (HumanRequest, error)
	GetHumanRequest(ctx context.Context, repo, id string) (HumanRequest, error)
	ListHumanRequests(ctx context.Context, repo string) ([]HumanRequest, error)
	// Answer は、回答を記録する。すでに回答があれば ErrAlreadyAnswered を返す。
	Answer(ctx context.Context, repo, id, body, answeredBy string, files []NewAttachment) error
	// ReadAttachment は、添付の中身を読み、記録の sha256 と合うかを確かめる。
	ReadAttachment(ctx context.Context, repo, humanRequestID, sha256 string) ([]byte, error)
}

// PrincipalKind は、主体の種類である。
type PrincipalKind string

const (
	PrincipalUser   PrincipalKind = "user"
	PrincipalClient PrincipalKind = "client"
)

// Principal は、API の要求を送った主体である。
type Principal struct {
	Login string
	Kind  PrincipalKind
}

// Authenticator は、認証のポートである（design/backend/ports/authentication.md）。
type Authenticator interface {
	// Authenticate は、トークンの主体を返す。Logiforge の主体でなければ ErrUnauthenticated を返す。
	Authenticate(ctx context.Context, token string) (Principal, error)
	// CanRead は、Authenticate を通った主体が、repository を読めるかを返す。
	CanRead(ctx context.Context, token, repo string) (bool, error)
}
