// Package handlers is the only package in the user module that speaks gin.
// Handlers are thin: read input, call one service method, write the envelope.
package handlers

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/4jawaly/identity-gateway/internal/middleware"
	"github.com/4jawaly/identity-gateway/internal/modules/user/dto"
	"github.com/4jawaly/identity-gateway/internal/reqctx"
	"github.com/4jawaly/identity-gateway/internal/response"
)

// UserService is what the handler needs from the service layer.
type UserService interface {
	Introspect(ctx context.Context, token string) (*dto.IntrospectResponse, bool, error)
	GetUser(ctx context.Context, sub string) (*dto.UserResponse, bool, error)
	ListUsers(ctx context.Context, q dto.ListUsersQuery) ([]dto.UserResponse, int64, error)
	ListSubAccounts(ctx context.Context, sub string) ([]dto.UserResponse, error)
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
//	@Success		200	{object}	response.Envelope{data=dto.IntrospectResponse,meta=response.Meta}
//	@Failure		401	{object}	response.Envelope{error=response.ErrorBody}								"TOKEN_MISSING, TOKEN_INVALID, TOKEN_INACTIVE, ISSUER_MISMATCH"
//	@Failure		403	{object}	response.Envelope{data=dto.IntrospectResponse,error=response.ErrorBody}	"ACCOUNT_SUSPENDED"
//	@Failure		429	{object}	response.Envelope{error=response.ErrorBody}								"RATE_LIMITED"
//	@Failure		503	{object}	response.Envelope{error=response.ErrorBody}								"ZITADEL_UNAVAILABLE"
//	@Failure		500	{object}	response.Envelope{error=response.ErrorBody}								"INTERNAL_ERROR"
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

// Me returns the caller's stored user.
//
//	@Summary		Current user
//	@Description	Returns the synced user row of the authenticated caller.
//	@Tags			users
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	response.Envelope{data=dto.UserResponse,meta=response.Meta}
//	@Failure		401	{object}	response.Envelope{error=response.ErrorBody}	"TOKEN_MISSING, TOKEN_INVALID, TOKEN_INACTIVE, ISSUER_MISMATCH"
//	@Failure		403	{object}	response.Envelope{error=response.ErrorBody}	"ACCOUNT_SUSPENDED"
//	@Failure		404	{object}	response.Envelope{error=response.ErrorBody}	"NOT_FOUND (not synced yet)"
//	@Failure		503	{object}	response.Envelope{error=response.ErrorBody}	"ZITADEL_UNAVAILABLE"
//	@Failure		500	{object}	response.Envelope{error=response.ErrorBody}	"INTERNAL_ERROR"
//	@Router			/users/me [get]
func (h *UserHandler) Me(c *gin.Context) {
	caller, ok := reqctx.AuthUserFrom(c.Request.Context())
	if !ok {
		response.Fail(c, response.ErrTokenMissing)
		return
	}
	u, cached, err := h.svc.GetUser(c.Request.Context(), caller.Sub)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Cached(c, u, cached)
}

// List pages through users (admin only).
//
//	@Summary		List users
//	@Description	Paginated list of synced users. Requires the ADMIN role.
//	@Tags			users
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query		int	false	"Page number"		default(1)	minimum(1)
//	@Param			per_page	query		int	false	"Items per page"	default(20)	minimum(1)	maximum(100)
//	@Success		200			{object}	response.Envelope{data=[]dto.UserResponse,meta=response.Meta}
//	@Failure		401			{object}	response.Envelope{error=response.ErrorBody}	"TOKEN_MISSING, TOKEN_INVALID, TOKEN_INACTIVE, ISSUER_MISMATCH"
//	@Failure		403			{object}	response.Envelope{error=response.ErrorBody}	"FORBIDDEN, ACCOUNT_SUSPENDED"
//	@Failure		422			{object}	response.Envelope{error=response.ErrorBody}	"VALIDATION_ERROR"
//	@Failure		503			{object}	response.Envelope{error=response.ErrorBody}	"ZITADEL_UNAVAILABLE"
//	@Failure		500			{object}	response.Envelope{error=response.ErrorBody}	"INTERNAL_ERROR"
//	@Router			/users [get]
func (h *UserHandler) List(c *gin.Context) {
	var q dto.ListUsersQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Fail(c, response.ErrValidation.With(err, map[string]any{"reason": err.Error()}))
		return
	}
	users, total, err := h.svc.ListUsers(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Paginated(c, users, q.Page, q.PerPage, total)
}

// Get returns one user by sub (admin only).
//
//	@Summary		Get a user
//	@Description	Returns one synced user by Zitadel subject. Requires the ADMIN role.
//	@Tags			users
//	@Produce		json
//	@Security		BearerAuth
//	@Param			sub	path		string	true	"Zitadel subject"	example(355646255235338042)
//	@Success		200	{object}	response.Envelope{data=dto.UserResponse,meta=response.Meta}
//	@Failure		401	{object}	response.Envelope{error=response.ErrorBody}	"TOKEN_MISSING, TOKEN_INVALID, TOKEN_INACTIVE, ISSUER_MISMATCH"
//	@Failure		403	{object}	response.Envelope{error=response.ErrorBody}	"FORBIDDEN, ACCOUNT_SUSPENDED"
//	@Failure		404	{object}	response.Envelope{error=response.ErrorBody}	"NOT_FOUND"
//	@Failure		503	{object}	response.Envelope{error=response.ErrorBody}	"ZITADEL_UNAVAILABLE"
//	@Failure		500	{object}	response.Envelope{error=response.ErrorBody}	"INTERNAL_ERROR"
//	@Router			/users/{sub} [get]
func (h *UserHandler) Get(c *gin.Context) {
	u, cached, err := h.svc.GetUser(c.Request.Context(), c.Param("sub"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Cached(c, u, cached)
}

// SubAccounts lists the children of a user.
//
//	@Summary		List sub-accounts
//	@Description	Returns the users that declare {sub} as one of their main accounts.
//	@Tags			users
//	@Produce		json
//	@Security		BearerAuth
//	@Param			sub	path		string	true	"Zitadel subject of the parent"	example(355646368934527195)
//	@Success		200	{object}	response.Envelope{data=[]dto.UserResponse}
//	@Failure		401	{object}	response.Envelope{error=response.ErrorBody}	"TOKEN_MISSING, TOKEN_INVALID, TOKEN_INACTIVE, ISSUER_MISMATCH"
//	@Failure		403	{object}	response.Envelope{error=response.ErrorBody}	"ACCOUNT_SUSPENDED"
//	@Failure		404	{object}	response.Envelope{error=response.ErrorBody}	"NOT_FOUND"
//	@Failure		503	{object}	response.Envelope{error=response.ErrorBody}	"ZITADEL_UNAVAILABLE"
//	@Failure		500	{object}	response.Envelope{error=response.ErrorBody}	"INTERNAL_ERROR"
//	@Router			/users/{sub}/sub-accounts [get]
func (h *UserHandler) SubAccounts(c *gin.Context) {
	users, err := h.svc.ListSubAccounts(c.Request.Context(), c.Param("sub"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, users)
}
