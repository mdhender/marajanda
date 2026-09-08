// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/mdhender/marajanda/internal/datastore"
)

var errAPICredentialsConflict = errors.New("cookie and bearer credentials were both supplied")

type apiAuthentication struct {
	Account datastore.Account
	Token   []byte
}

type apiAuthenticationContextKey struct{}

func (app *application) createAPISession(w http.ResponseWriter, r *http.Request) {
	if !hasAPIJSONContentType(r) {
		writeAPIError(w, http.StatusUnsupportedMediaType, apiCodeUnsupportedMediaType, "Content-Type must be application/json.")
		return
	}

	var request apiCreateSessionRequest
	if err := decodeAPIJSON(r.Body, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidJSON, "The request body must contain one valid session object.")
		return
	}
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	if request.Email == "" || request.Passphrase == "" {
		writeAPIError(w, http.StatusBadRequest, apiCodeInvalidRequest, "Email and passphrase are required.")
		return
	}
	if !isEmail(request.Email) {
		writeAPIError(w, http.StatusUnauthorized, apiCodeCredentialsRejected, "Those credentials were not accepted.")
		return
	}
	if app.authenticate == nil {
		writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
		return
	}
	account, ok, err := app.authenticate(r.Context(), request.Email, request.Passphrase)
	if err != nil {
		if errors.Is(err, datastore.ErrAccountInactive) {
			writeAPIError(w, http.StatusForbidden, apiCodeAccountInactive, "That account is not active.")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
		return
	}
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, apiCodeCredentialsRejected, "Those credentials were not accepted.")
		return
	}

	responseAccount, err := app.apiAccount(r.Context(), account)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
		return
	}
	token, err := app.createSession(r.Context(), account)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
		return
	}
	setSessionCookie(w, token)
	_ = writeAPIJSON(w, http.StatusCreated, apiSession{
		Token:   base64.RawURLEncoding.EncodeToString(token),
		Account: responseAccount,
	})
}

func (app *application) deleteAPISession(w http.ResponseWriter, r *http.Request) {
	authentication := apiAuthenticationFromContext(r.Context())
	if err := app.store.DeleteSession(r.Context(), authentication.Token); err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
		return
	}
	expireSessionCookie(w)
	writeAPINoContent(w)
}

func (app *application) getAPIAccount(w http.ResponseWriter, r *http.Request) {
	account, err := app.apiAccount(r.Context(), apiAuthenticationFromContext(r.Context()).Account)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
		return
	}
	_ = writeAPIJSON(w, http.StatusOK, account)
}

func (app *application) apiAccount(ctx context.Context, account datastore.Account) (apiAccount, error) {
	if app.store == nil {
		return apiAccount{}, errors.New("session store is not configured")
	}
	faction, found, err := app.store.Faction(ctx, account.Email)
	if err != nil {
		return apiAccount{}, err
	}
	response := apiAccount{
		Email:             account.Email,
		Handle:            account.Handle,
		Role:              account.Role,
		Active:            account.Active,
		Seated:            account.Seated,
		FactionConfigured: found && faction.Configured(),
	}
	if account.Seated {
		response.Origin = &apiCoordinate{Q: account.Origin.Q(), R: account.Origin.R()}
	}
	return response, nil
}

// requireAPIAuthentication resolves exactly one cookie or bearer credential
// and makes both its current account and raw token available to the domain
// handler. Unlike the browser wrapper, it always answers in JSON and never
// redirects.
func (app *application) requireAPIAuthentication(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := apiCredential(r)
		if errors.Is(err, errAPICredentialsConflict) {
			writeAPIError(w, http.StatusBadRequest, apiCodeCredentialsConflict, "Supply either a session cookie or a bearer token, not both.")
			return
		}
		if err != nil || app.store == nil {
			writeAPIError(w, http.StatusUnauthorized, apiCodeAuthenticationNeeded, "A valid session is required.")
			return
		}
		account, found, err := app.store.ResolveSession(r.Context(), token)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, apiCodeInternal, "The server could not complete the request.")
			return
		}
		if !found {
			writeAPIError(w, http.StatusUnauthorized, apiCodeAuthenticationNeeded, "A valid session is required.")
			return
		}
		ctx := context.WithValue(r.Context(), apiAuthenticationContextKey{}, apiAuthentication{Account: account, Token: token})
		next(w, r.WithContext(ctx))
	}
}

// requireAPIRole is the reusable API authorization boundary. It deliberately
// wraps API authentication rather than the browser role gate, whose redirects
// are part of the HTML contract.
func (app *application) requireAPIRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return app.requireAPIAuthentication(func(w http.ResponseWriter, r *http.Request) {
		if apiAuthenticationFromContext(r.Context()).Account.Role != role {
			writeAPIError(w, http.StatusForbidden, apiCodeForbidden, "This account cannot use that resource.")
			return
		}
		next(w, r)
	})
}

func apiAuthenticationFromContext(ctx context.Context) apiAuthentication {
	authentication, _ := ctx.Value(apiAuthenticationContextKey{}).(apiAuthentication)
	return authentication
}

func apiCredential(r *http.Request) ([]byte, error) {
	var cookieValues []string
	for _, cookie := range r.Cookies() {
		if cookie.Name == sessionCookieName {
			cookieValues = append(cookieValues, cookie.Value)
		}
	}
	authorizations := r.Header.Values("Authorization")
	if len(cookieValues) != 0 && len(authorizations) != 0 {
		return nil, errAPICredentialsConflict
	}
	if len(cookieValues) == 1 {
		return decodeSessionToken(cookieValues[0])
	}
	if len(cookieValues) > 1 || len(authorizations) != 1 {
		return nil, errors.New("missing or malformed API credential")
	}

	value := authorizations[0]
	space := strings.IndexByte(value, ' ')
	if space == -1 || !strings.EqualFold(value[:space], "Bearer") {
		return nil, errors.New("malformed bearer credential")
	}
	value = strings.TrimLeft(value[space:], " ")
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return nil, errors.New("malformed bearer credential")
	}
	return decodeSessionToken(value)
}
