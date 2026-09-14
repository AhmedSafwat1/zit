// Package response defines the single JSON envelope used by every endpoint
// and the typed AppError catalog handlers translate into HTTP statuses.
package response

import (
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/4jawaly/identity-gateway/internal/reqctx"
)

// Envelope is the shape of every response body. The request id travels in
// the X-Request-ID header and in the logs, not in the body.
type Envelope struct {
	Success bool       `json:"success"`
	Data    any        `json:"data,omitempty"`
	Meta    *Meta      `json:"meta,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

// ErrorBody is the error half of the envelope.
type ErrorBody struct {
	Code    string         `json:"code" example:"TOKEN_INACTIVE"`
	Message string         `json:"message" example:"token is expired or revoked"`
	Details map[string]any `json:"details,omitempty"`
	// Debug is present only for 5xx responses when APP_DEBUG=true. Never enable in production.
	Debug *DebugInfo `json:"debug,omitempty"`
}

// DebugInfo exposes the internal cause of a failure to the client in debug mode.
type DebugInfo struct {
	Cause string `json:"cause" example:"dial tcp: connection refused"`
	Stack string `json:"stack,omitempty"`
}

// Meta carries pagination and cache information.
type Meta struct {
	Page       int   `json:"page,omitempty" example:"1"`
	PerPage    int   `json:"per_page,omitempty" example:"20"`
	Total      int64 `json:"total,omitempty" example:"120"`
	TotalPages int   `json:"total_pages,omitempty" example:"6"`
	Cached     *bool `json:"cached,omitempty" example:"true"`
}

var debug atomic.Bool

// SetDebug toggles echoing internal causes into error responses. Called once
// at boot from the router; never per request.
func SetDebug(on bool) { debug.Store(on) }

// DebugEnabled reports the current debug mode.
func DebugEnabled() bool { return debug.Load() }

// OK writes a 200 envelope.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Success: true, Data: data})
}

// Created writes a 201 envelope.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{Success: true, Data: data})
}

// Cached writes a 200 envelope with meta.cached set.
func Cached(c *gin.Context, data any, cached bool) {
	c.JSON(http.StatusOK, Envelope{Success: true, Data: data, Meta: &Meta{Cached: &cached}})
}

// Paginated writes a 200 envelope with pagination meta.
func Paginated(c *gin.Context, data any, page, perPage int, total int64) {
	totalPages := 0
	if perPage > 0 {
		totalPages = int((total + int64(perPage) - 1) / int64(perPage))
	}
	c.JSON(http.StatusOK, Envelope{
		Success: true, Data: data,
		Meta: &Meta{Page: page, PerPage: perPage, Total: total, TotalPages: totalPages},
	})
}

// Fail writes an error envelope. An *AppError anywhere in err's chain supplies
// status and code; anything else becomes INTERNAL_ERROR 500. Every 5xx is
// logged at Error level with its cause and request id. The cause reaches
// the client only when debug mode is on.
func Fail(c *gin.Context, err error) {
	failWith(c, err, nil, "")
}

// FailWithData writes an error envelope that still carries data — used when a
// report is available but the account is not allowed in (ACCOUNT_SUSPENDED).
func FailWithData(c *gin.Context, err error, data any) {
	failWith(c, err, data, "")
}

// FailPanic is Fail for the recovery middleware: the stack is logged and,
// in debug mode, echoed to the client.
func FailPanic(c *gin.Context, err error, stack string) {
	failWith(c, err, nil, stack)
}

func failWith(c *gin.Context, err error, data any, stack string) {
	ae, ok := AsAppError(err)
	if !ok {
		ae = ErrInternal
	}
	if ae.Status >= http.StatusInternalServerError {
		ev := zerolog.Ctx(c.Request.Context()).Error().
			Err(err).
			Str("code", ae.Code).
			Str("request_id", reqctx.RequestIDFrom(c.Request.Context())).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path)
		if stack != "" {
			ev = ev.Str("stack", stack)
		}
		ev.Msg("internal server error")
	}
	body := &ErrorBody{Code: ae.Code, Message: ae.Message, Details: ae.Details}
	if DebugEnabled() && err != nil && ae.Status >= http.StatusInternalServerError {
		body.Debug = &DebugInfo{Cause: err.Error(), Stack: stack}
	}
	c.AbortWithStatusJSON(ae.Status, Envelope{Data: data, Error: body})
}
