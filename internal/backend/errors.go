package backend

import (
	"errors"
	"fmt"

	"github.com/yamambainc/logiforge/internal/ports"
)

// Code は、エラーの種類である（design/api/openapi.yaml の Problem.code）。
type Code string

const (
	CodeValidationFailed        Code = "validation_failed"
	CodeUnauthenticated         Code = "unauthenticated"
	CodeForbidden               Code = "forbidden"
	CodeNotFound                Code = "not_found"
	CodeNotOpen                 Code = "not_open"
	CodeCommitConflict          Code = "commit_conflict"
	CodeOverlayConflict         Code = "overlay_conflict"
	CodeOverlayUnsupported      Code = "overlay_unsupported"
	CodeTooLarge                Code = "too_large"
	CodeRepositoryExists        Code = "repository_exists"
	CodeUnavailable             Code = "unavailable"
	CodeSnapshotPending         Code = "snapshot_pending"
	CodeRepositoryNotRegistered Code = "repository_not_registered"
	// CodeSetupRequired は、初回のセットアップが終わっていないことである（design/api/endpoints/setup.md）。
	CodeSetupRequired Code = "setup_required"
	// CodeSetupDone は、セットアップが終わったあとに、セットアップの API を呼んだことである。
	CodeSetupDone Code = "setup_done"
	// CodeSetupInProgress は、セットアップが、ほかのブラウザで進んでいることである。
	CodeSetupInProgress Code = "setup_in_progress"
	// 検査に通らない approve と merge の理由（design/backend/merge.md）。
	CodeStaleTarget        Code = "stale_target"
	CodeConflict           Code = "conflict"
	CodeInvariantViolation Code = "invariant_violation"
	// CodeNotApproved は、merge しようとした apply request の、最後の決めるレビューが、先頭のコミットへの approve でないことである。
	CodeNotApproved Code = "not_approved"
	// CodeMergeForbidden は、GitForge が、呼んだ主体の merge を断ったことである。
	CodeMergeForbidden Code = "merge_forbidden"
	// CodeInternal は、バックエンドの内部の誤りである。詳しいことは、ログにだけ書く。
	CodeInternal Code = "internal"
)

// Error は、API の利用者に返すエラーである。
type Error struct {
	Code   Code
	Detail string
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Detail }

func fail(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// AsError は、err を API のエラーにする。ポートの誤りは、対応する種類にする。それ以外は nil を返す（内部の誤り）。
func AsError(err error) *Error {
	var e *Error
	switch {
	case errors.As(err, &e):
		return e
	case errors.Is(err, ports.ErrNotFound):
		return &Error{Code: CodeNotFound, Detail: "対象がない"}
	case errors.Is(err, ports.ErrUnauthenticated):
		return &Error{Code: CodeUnauthenticated, Detail: "トークンがない、正しくない、または Logiforge の主体でない"}
	case errors.Is(err, ports.ErrForbidden):
		return &Error{Code: CodeForbidden, Detail: "GitForge が、この主体の操作を断った（権限が足りない）"}
	case errors.Is(err, ports.ErrUnavailable):
		return &Error{Code: CodeUnavailable, Detail: "GitForge か、その git を読めない。少し待って送り直す"}
	}
	return nil
}
