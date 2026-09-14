// Package handlers is the only package in the user module that speaks gin.
// Handlers are thin: read input, call one service method, write the envelope.
package handlers

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/internal/middleware"
	userdto "github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/response"
)

// UserService is what the handler needs from the service layer.
type UserService interface {
	Introspect(ctx context.Context, token string) (*userdto.IntrospectResponse, bool, error)
}

// UserHandler serves /users.
type UserHandler struct{ svc UserService }

// NewUserHandler builds the handler.
func NewUserHandler(svc UserService) *UserHandler { return &UserHandler{svc: svc} }

// Introspect validates a bearer token and returns the identity report.
//
//	@Summary		Introspect a bearer token
//	@Description	Validates the token against Zitadel (cached in Redis) and returns the identity report. A suspended account still returns the report alongside ACCOUNT_SUSPENDED.
//	@Tags			users
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.Envelope{data=userdto.IntrospectResponse,meta=response.Meta}
//	@Failure		401	{object}	response.Envelope{error=response.ErrorBody}	"TOKEN_MISSING, TOKEN_INVALID, TOKEN_INACTIVE, ISSUER_MISMATCH"
//	@Failure		403	{object}	response.Envelope{data=userdto.IntrospectResponse,error=response.ErrorBody}	"ACCOUNT_SUSPENDED"
//	@Failure		429	{object}	response.Envelope{error=response.ErrorBody}	"RATE_LIMITED"
//	@Failure		503	{object}	response.Envelope{error=response.ErrorBody}	"ZITADEL_UNAVAILABLE"
//	@Failure		500	{object}	response.Envelope{error=response.ErrorBody}	"INTERNAL_ERROR"
//	@Router			/users/introspect [post]
func (h *UserHandler) Introspect(c *gin.Context) {
	token := middleware.BearerToken(c.GetHeader("Authorization"))
	if token == "" {
		response.Fail(c, response.ErrTokenMissing)
		return
	}
	report, cached, err := h.svc.Introspect(c.Request.Context(), token)
	if err != nil {
		if report != nil && errors.Is(err, response.ErrAccountSuspended) {
			response.FailWithData(c, err, report)
			return
		}
		response.Fail(c, err)
		return
	}
	response.Cached(c, report, cached)
}
