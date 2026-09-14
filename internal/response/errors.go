package response

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
)

// AppError is the only error type handlers translate into an HTTP status.
// The package-level sentinels below are the catalog; never mutate them —
// use With to derive a copy carrying a cause and details.
type AppError struct {
	Code    string
	Status  int
	Message string
	Details map[string]any
	cause   error
}

// Error implements error.
func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the cause to errors.Is / errors.As.
func (e *AppError) Unwrap() error { return e.cause }

// Is makes every derived copy match its catalog sentinel by code.
func (e *AppError) Is(target error) bool {
	t, ok := target.(*AppError)
	return ok && t.Code == e.Code
}

// With returns a copy of e with the cause and details attached. The receiver
// is never mutated, so it is safe to call on the shared sentinels.
func (e *AppError) With(cause error, details map[string]any) *AppError {
	cp := *e
	cp.cause = cause
	if details != nil {
		cp.Details = maps.Clone(details)
	}
	return &cp
}

// Cause returns the wrapped cause, if any.
func (e *AppError) Cause() error { return e.cause }

func newErr(code string, status int, msg string) *AppError {
	return &AppError{Code: code, Status: status, Message: msg}
}

// Catalog. Codes are part of the public API contract and must stay stable.
var (
	ErrTokenMissing       = newErr("TOKEN_MISSING", http.StatusUnauthorized, "bearer token is missing")
	ErrTokenInvalid       = newErr("TOKEN_INVALID", http.StatusUnauthorized, "bearer token is malformed")
	ErrTokenInactive      = newErr("TOKEN_INACTIVE", http.StatusUnauthorized, "token is expired or revoked")
	ErrIssuerMismatch     = newErr("ISSUER_MISMATCH", http.StatusUnauthorized, "token was issued by an unexpected issuer")
	ErrAccountSuspended   = newErr("ACCOUNT_SUSPENDED", http.StatusForbidden, "account is not active")
	ErrForbidden          = newErr("FORBIDDEN", http.StatusForbidden, "insufficient permissions")
	ErrValidation         = newErr("VALIDATION_ERROR", http.StatusUnprocessableEntity, "request validation failed")
	ErrNotFound           = newErr("NOT_FOUND", http.StatusNotFound, "resource not found")
	ErrConflict           = newErr("CONFLICT", http.StatusConflict, "resource conflict")
	ErrRateLimited        = newErr("RATE_LIMITED", http.StatusTooManyRequests, "too many requests")
	ErrZitadelUnavailable = newErr("ZITADEL_UNAVAILABLE", http.StatusServiceUnavailable, "identity provider is unavailable")
	ErrInternal           = newErr("INTERNAL_ERROR", http.StatusInternalServerError, "internal error")
)

// AsAppError extracts an *AppError from err's chain.
func AsAppError(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
