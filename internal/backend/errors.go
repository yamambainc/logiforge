package backend

import (
	"errors"
	"fmt"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
)

// Code は、エラーの種類である（design/api/openapi.yaml の Problem.code）。
type Code string

const (
	CodeValidationFailed        Code = "validation_failed"
	CodeUnauthenticated         Code = "unauthenticated"
	CodeForbidden               Code = "forbidden"
	CodeSessionRequired         Code = "session_required"
	CodeNotFound                Code = "not_found"
	CodeSameSession             Code = "same_session"
	CodeNotOpen                 Code = "not_open"
	CodeVersionConflict         Code = "version_conflict"
	CodeTooLarge                Code = "too_large"
	CodeRepositoryExists        Code = "repository_exists"
	CodeUnavailable             Code = "unavailable"
	CodeSnapshotPending         Code = "snapshot_pending"
	CodeRepositoryNotRegistered Code = "repository_not_registered"
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
	case errors.Is(err, ports.ErrUnavailable):
		return &Error{Code: CodeUnavailable, Detail: "worker に頼めない。少し待って送り直す"}
	}
	return nil
}
