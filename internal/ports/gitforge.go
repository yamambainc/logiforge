// Package ports は、バックエンドのポート（design/backend/ports/）の Go のインターフェースである。
// アダプタは、このインターフェースを実装する。バックエンドは、アダプタを知らない。
package ports

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ポートの誤り。アダプタは、これを errors.Is で見分けられる形で返す。
var (
	ErrNotFound      = errors.New("not found")
	ErrMainExists    = errors.New("main がすでにある")
	ErrBranchHasOpen = errors.New("そのブランチに open の apply request がすでにある")
	ErrHeadMoved     = errors.New("pull request の head が、指定した SHA でない")
	ErrConflict      = errors.New("git の衝突で merge できない")
	// ErrMergeForbidden は、merge を呼んだ主体に、merge の権限がないことを表す（design/backend/merge.md の手順 4）。
	ErrMergeForbidden  = errors.New("この主体には、merge の権限がない")
	ErrAlreadyAnswered = errors.New("すでに回答がある")
	ErrUnauthenticated = errors.New("この主体は、Logiforge の主体でない")
	// ErrNotRegistered は、repository の名前が、サーバの設定に登録されていないことを表す（GitHub を使う構成）。
	ErrNotRegistered    = errors.New("repository の名前が、サーバの設定に登録されていない")
	ErrAttachmentTamper = errors.New("添付の sha256 が、回答のコメントに書いたものと合わない")
	// ErrAttachmentsUnsupported は、回答の添付をファイルで受け付けない GitForge であることを表す（添付はリンクだけ）。
	ErrAttachmentsUnsupported = errors.New("この GitForge は、回答の添付をファイルで受け付けない")
)

// Repository は、repository の名前と、GitForge の側の参照の組である。
type Repository struct {
	Name string
	// GitForgeRef は、GitForge の側の参照である（例：owner/name）。ディレクトリの repository では、
	// git リポジトリの参照とディレクトリのパスを : でつなぐ（例：owner/name:logiforge/cache-strategy。design/directory/README.md）。
	GitForgeRef string
}

// Directory は、ディレクトリの repository のパスを返す。git リポジトリ全体の repository では空。
func (r Repository) Directory() string { _, d := SplitRef(r.GitForgeRef); return d }

// SplitRef は、gitforge_ref を、git リポジトリの参照と、ディレクトリのパスに分ける。ディレクトリのない参照では、パスは空。
func SplitRef(ref string) (git, dir string) {
	git, dir, _ = strings.Cut(ref, ":")
	return git, dir
}

// File は、repository を用意するときに、最初のコミットに入れるファイルである。
type File struct {
	Path    string
	Content []byte
}

// State は、apply request の状態である。
type State string

const (
	StateOpen   State = "open"
	StateMerged State = "merged"
	StateClosed State = "closed"
)

// ApplyRequest は、GitForge から読んだ apply request（pull request）である（design/adapters/pull-requests.md）。
// head のブランチが消えた open の apply request を closed として扱うのは、ブランチの履歴を読むバックエンドである。
type ApplyRequest struct {
	// ID は、pull request の番号を 10 進の文字列にしたものである。
	ID     string
	Number int
	Branch string
	// Target は、向き先のブランチ（pull request の base）である。
	Target       string
	Description  string
	HeadRevision string
	State        State
	// Opener は、apply request を作った主体（pull request の作者）である。
	Opener string
	// Waiting は、pull request のラベルが表す、レビュー待ちか修正待ちか（design/adapters/pull-requests.md の「レビュー待ちと修正待ちのラベル」）。
	Waiting Waiting
	// ReviewWaitingAt は、レビュー待ちのラベルを最後に付けた時刻である。1 件を読むときだけ入る（ずれを直すのに使う）。
	ReviewWaitingAt time.Time
	// Kind は、直すための apply request の種類（repair.revert か repair.accept）。ふつうの apply request では空。
	Kind string
	// CloseReason と CloseSession は、close のコメントの値である。1 件を読むときだけ入る。
	CloseReason  string
	CloseSession string
	// MergeFailures は、head のコミットの、反映の失敗の commit status である（古い順）。1 件を読むときだけ入る。
	MergeFailures []MergeFailure
	CreatedAt     time.Time
	// MergedRevision は、反映したときの merge のコミットの SHA である。反映していなければ空。
	MergedRevision string
	// Warnings は、読むときに見つけたことの警告である。
	Warnings []string
}

// Waiting は、レビュー待ちか修正待ちかのラベルである。
type Waiting string

const (
	// WaitingNone は、どちらのラベルもないこと。一覧では、レビュー待ちとみなす。
	WaitingNone   Waiting = ""
	WaitingReview Waiting = "review"
	WaitingFix    Waiting = "fix"
	// WaitingBoth は、2 つとも付いていること。一覧では、修正待ちとみなす。
	WaitingBoth Waiting = "both"
)

// MergeFailure は、反映の失敗（head のコミットの commit status）である。
type MergeFailure struct {
	// Revision は、反映しようとした先頭のコミット。
	Revision string
	// Reason は、stale_target、invariant_violation、conflict、snapshot_pending のどれか。
	Reason string
	Detail string
	At     time.Time
}

// NewApplyRequest は、apply request を作るときに渡すものである。識別子は、GitForge が pull request の番号で決める。
type NewApplyRequest struct {
	// Kind は、直すための apply request の種類（repair.revert か repair.accept）。ふつうの apply request では空。
	Kind        string
	Branch      string
	Description string
	Opener      string
}

// ReviewKind は、レビューの種類である。
type ReviewKind string

const (
	ReviewApprove        ReviewKind = "approve"
	ReviewRequestChanges ReviewKind = "request_changes"
	ReviewComment        ReviewKind = "comment"
)

// Review は、レビュー（pull request の review）である。ID は、GitForge の review の識別子である。
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

// Reviews は、apply request のレビューの一覧である。
type Reviews struct {
	// Items は、数えたレビュー（design/adapters/pull-requests.md の「数える review」）を、出した順に並べたもの。
	Items    []Review
	Warnings []string
}

// HumanRequestKind は、人への依頼の種類である。
type HumanRequestKind string

const (
	HumanRequestPVR      HumanRequestKind = "pvr"
	HumanRequestDecision HumanRequestKind = "decision"
)

// HumanRequest は、人への依頼（issue）である。ID は、issue の番号を 10 進の文字列にしたものである。
type HumanRequest struct {
	ID         string
	Number     int
	Kind       HumanRequestKind
	TargetNode string
	Body       string
	Requester  string
	Session    string
	CreatedAt  time.Time
	// Answer は、回答。まだなければ nil。一覧では、本文と添付は空で、時刻（issue を閉じた時刻）だけが入る。
	Answer   *Answer
	Warnings []string
}

// NewHumanRequest は、人への依頼を起票するときに渡すものである。識別子は、GitForge が issue の番号で決める。
type NewHumanRequest struct {
	// IdempotencyKey は、送り直しを見分けるキー（design/api/writes.md の「同じ要求の繰り返し」）。なければ空。
	IdempotencyKey string
	Kind           HumanRequestKind
	TargetNode     string
	Body           string
	Requester      string
	Session        string
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
	// Archived は、git リポジトリが読むだけ（archive）かを返す（design/directory/migrate.md）。
	Archived(ctx context.Context, gitForgeRef string) (bool, error)

	CreateApplyRequest(ctx context.Context, repo string, ar NewApplyRequest) (ApplyRequest, error)
	GetApplyRequest(ctx context.Context, repo, id string) (ApplyRequest, error)
	// ListApplyRequests は、state（open、closed、all。空なら all）の apply request を、pull request の一覧だけから読む。
	// ラベル（Waiting）は入るが、close のコメント、反映の失敗、ReviewWaitingAt は入らない。
	ListApplyRequests(ctx context.Context, repo, state string) ([]ApplyRequest, error)
	UpdateDescription(ctx context.Context, repo, id, description string) error
	// Merge は、head の SHA を指定して squash merge する。ErrHeadMoved か ErrConflict を返しうる。
	// 戻り値は、反映のコミットの SHA である。
	Merge(ctx context.Context, repo, id, headSHA, title, message string) (string, error)
	Close(ctx context.Context, repo, id, reason, session string) error
	// RecordMergeFailure は、head のコミットに反映の失敗の commit status を立て、理由を pull request のコメントに書く。
	RecordMergeFailure(ctx context.Context, repo, id string, f MergeFailure) error
	// SetWaiting は、レビュー待ちか修正待ちかのラベルを付け替える。WaitingNone なら、両方を外す。
	SetWaiting(ctx context.Context, repo, id string, w Waiting) error

	// AddReview は、pull request に review を出し、出した review を返す。
	AddReview(ctx context.Context, repo, applyRequestID string, r Review) (Review, error)
	ListReviews(ctx context.Context, repo, applyRequestID string) (Reviews, error)
	// MergedApplyRequest は、sha を merge のコミットにした、向き先が main の pull request の apply request を返す（design/backend/verify/README.md）。
	// committed は、sha の committer date で、反映した pull request の一覧を読むのをやめる位置に使う（design/backend/list-reads.md）。
	// そういう pull request がなければ ErrNotFound。
	MergedApplyRequest(ctx context.Context, repo, sha string, committed time.Time) (ApplyRequest, error)

	CreateHumanRequest(ctx context.Context, repo string, hr NewHumanRequest) (HumanRequest, error)
	GetHumanRequest(ctx context.Context, repo, id string) (HumanRequest, error)
	ListHumanRequests(ctx context.Context, repo string) ([]HumanRequest, error)
	// Answer は、回答のコメントを書き、issue を閉じる。すでに違う回答があれば ErrAlreadyAnswered を返す。
	Answer(ctx context.Context, repo, id, body, answeredBy string, files []NewAttachment) error
	// ReadAttachment は、添付の中身を読み、回答のコメントに書いた sha256 と合うかを確かめる。
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
	// WebUI は、トークンが、WebUI のサインインで得たもの（Logiforge のアプリケーションのもの）かである
	// （design/settings/users.md の「トークンの出どころ」）。CLI のトークンでは false。
	WebUI bool
}

// Authenticator は、認証のポートである（design/backend/ports/authentication.md）。
type Authenticator interface {
	// Authenticate は、トークンの主体を返す。Logiforge の主体でなければ ErrUnauthenticated を返す。
	Authenticate(ctx context.Context, token string) (Principal, error)
	// CanRead は、Authenticate を通った主体が、repository を読めるかを返す。
	CanRead(ctx context.Context, token, repo string) (bool, error)
	// CanWrite は、主体が、repository に Write 以上の権限を持つかを返す（design/settings/users.md の「repository ごとの権限」）。
	// 主体が読めない git リポジトリでは、false を返す。
	CanWrite(ctx context.Context, token, repo string) (bool, error)
	// CanCreate は、主体が repository を用意できるかを返す。git リポジトリがあれば CanWrite と同じ。
	// まだない git リポジトリ（Forgejo で作る前）では、作れるかを返す。
	CanCreate(ctx context.Context, token, repo string) (bool, error)
}

type freshKey struct{}

// WithFresh は、権限のキャッシュを使わずに問い合わせることを、ctx に付ける。設定を変える要求で使う
// （design/settings/users.md の「権限のキャッシュ」）。
func WithFresh(ctx context.Context) context.Context { return context.WithValue(ctx, freshKey{}, true) }

// Fresh は、ctx が、キャッシュを使わない問い合わせかを返す。
func Fresh(ctx context.Context) bool { v, _ := ctx.Value(freshKey{}).(bool); return v }

// tokenKey は、GitForge を呼ぶ主体のトークンを、ctx で渡す鍵である。
type tokenKey struct{}

// WithToken は、GitForge を呼ぶ主体のトークンを ctx に入れる。アダプタは、GitForge に書くときも読むときも、このトークンを使う
// （design/adapters/pull-requests.md：Logiforge は、書くための自分のアカウントを持たない）。
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

// TokenFrom は、ctx の主体のトークンを返す。なければ空。
func TokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(tokenKey{}).(string)
	return t
}

// MergeVerdict は、main のコミットが反映によるものかの判定である（design/backend/verify/README.md）。
type MergeVerdict struct {
	// Pending は、まだ決められないこと（C を merge した pull request が見つからない、など）。次の見回りでやり直す。
	Pending bool
	// MissingHead は、pull request の先頭のコミットのスナップショットがないとき、その SHA である。作ってから、やり直す。
	MissingHead string
	// Authorized は、反映によるものか。
	Authorized bool
	// ApplyRequest は、反映した apply request の識別子、Kind はその種類（直すための apply request なら repair.revert か repair.accept）。
	ApplyRequest string
	Kind         string
	// Detail は、反映によるものでないときの、満たさなかった条件である。
	Detail string
}

// CheckDirectories は、ディレクトリの repository の登録を確かめる（design/directory/README.md の「登録」）。
//   - パスは、/ で区切った相対のパスで、空の部分、`.`、`..` を含まない。
//   - 同じ git リポジトリの、ほかの登録したディレクトリの中にも外にも重ならない。
//   - 名前は重ならない。
func CheckDirectories(rs []Repository) error {
	names := map[string]bool{}
	byGit := map[string][]string{}
	whole := map[string]bool{}
	for _, r := range rs {
		if r.Name == "" || names[r.Name] {
			return fmt.Errorf("repository の名前が空か、重なっている: %q", r.Name)
		}
		names[r.Name] = true
		git, dir := SplitRef(r.GitForgeRef)
		// GitHub は owner と名前の大文字と小文字を区別しないので、小文字にそろえて比べる。
		key := strings.ToLower(git)
		if dir == "" {
			whole[key] = true
			continue
		}
		for _, part := range strings.Split(dir, "/") {
			if part == "" || part == "." || part == ".." {
				return fmt.Errorf("ディレクトリのパスが正しくない: %q", dir)
			}
		}
		for _, other := range byGit[key] {
			if other == dir || strings.HasPrefix(dir, other+"/") || strings.HasPrefix(other, dir+"/") {
				return fmt.Errorf("ディレクトリ %q と %q が、同じ git リポジトリ %s の中で重なる", other, dir, git)
			}
		}
		byGit[key] = append(byGit[key], dir)
	}
	// 同じ git リポジトリを、全体の repository とディレクトリの repository の両方には登録しない。
	// なぜ：全体の repository は、ディレクトリの repository の反映を、反映によらないものとして壊れた状態になるから。
	for key := range byGit {
		if whole[key] {
			return fmt.Errorf("git リポジトリ %s が、全体の repository とディレクトリの repository の両方に登録されている", key)
		}
	}
	return nil
}

// ParseDirectories は、「名前=owner/name:パス,…」の形の登録を読む（ディレクトリの repository）。
func ParseDirectories(v string) ([]Repository, error) {
	var out []Repository
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, ref, ok := strings.Cut(part, "=")
		git, dir := SplitRef(ref)
		owner, repo, ok2 := strings.Cut(git, "/")
		if !ok || !ok2 || owner == "" || repo == "" || strings.Contains(repo, "/") || dir == "" {
			return nil, fmt.Errorf("ディレクトリの repository は 名前=owner/name:パス の形で書く: %q", part)
		}
		out = append(out, Repository{Name: name, GitForgeRef: ref})
	}
	return out, CheckDirectories(out)
}
