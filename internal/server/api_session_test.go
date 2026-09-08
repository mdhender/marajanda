// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/marajanda/internal/datastore"
	"github.com/mdhender/marajanda/internal/game"
)

func TestAPICreateSessionIssuesOneTokenForCookieAndBearer(t *testing.T) {
	store := &testStore{
		faction: datastore.Faction{Name: "The Wayfarers", Race: game.RaceHuman, Active: true},
		found:   true,
	}
	handler := newHandler(func(_ context.Context, email, passphrase string) (datastore.Account, bool, error) {
		if email != "player@example.com" || passphrase != "good.luck" {
			t.Fatalf("Authenticate(%q, %q)", email, passphrase)
		}
		return datastore.Account{
			Email: "player@example.com", Handle: "wanderer", Role: "player", Active: true,
			Seated: true, Origin: hexg.NewHex(2, -1),
		}, true, nil
	}, store)

	created := apiRequest(handler, http.MethodPost, "/api/v1/sessions",
		`{"email":" PLAYER@EXAMPLE.COM ","passphrase":"good.luck"}`, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var session apiSession
	decodeAPIResponse(t, created, &session)
	if session.Account.Email != "player@example.com" || session.Account.Origin == nil ||
		session.Account.Origin.Q != 2 || session.Account.Origin.R != -1 || !session.Account.FactionConfigured {
		t.Fatalf("account = %#v", session.Account)
	}
	token, err := base64.RawURLEncoding.DecodeString(session.Token)
	if err != nil || len(token) != datastore.SessionTokenBytes {
		t.Fatalf("token = %q: %v", session.Token, err)
	}
	cookies := created.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || cookie.Value != session.Token || !cookie.HttpOnly || !cookie.Secure ||
		cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 0 {
		t.Fatalf("cookie = %#v", cookie)
	}

	bearer := apiRequest(handler, http.MethodGet, "/api/v1/account", "", map[string]string{
		"Authorization": "Bearer " + session.Token,
	})
	if bearer.Code != http.StatusOK {
		t.Fatalf("bearer status = %d, body = %s", bearer.Code, bearer.Body.String())
	}
	cookieRequest := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	cookieRequest.AddCookie(cookie)
	cookieResponse := httptest.NewRecorder()
	handler.ServeHTTP(cookieResponse, cookieRequest)
	if cookieResponse.Code != http.StatusOK || cookieResponse.Body.String() != bearer.Body.String() {
		t.Fatalf("cookie response = %d %s, bearer response = %d %s",
			cookieResponse.Code, cookieResponse.Body.String(), bearer.Code, bearer.Body.String())
	}
}

func TestAPICreateSessionRejectsCredentialsAndInvalidRequests(t *testing.T) {
	store := &testStore{}
	handler := newHandler(func(_ context.Context, _, passphrase string) (datastore.Account, bool, error) {
		if passphrase == "inactive" {
			return datastore.Account{}, false, fmt.Errorf("%w: player@example.com", datastore.ErrAccountInactive)
		}
		return datastore.Account{}, false, nil
	}, store)

	for _, test := range []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{name: "wrong credentials", contentType: apiJSONContentType, body: `{"email":"player@example.com","passphrase":"wrong"}`, wantStatus: http.StatusUnauthorized, wantCode: apiCodeCredentialsRejected},
		{name: "invalid email", contentType: apiJSONContentType, body: `{"email":"not-an-email","passphrase":"wrong"}`, wantStatus: http.StatusUnauthorized, wantCode: apiCodeCredentialsRejected},
		{name: "inactive account", contentType: apiJSONContentType, body: `{"email":"player@example.com","passphrase":"inactive"}`, wantStatus: http.StatusForbidden, wantCode: apiCodeAccountInactive},
		{name: "missing passphrase", contentType: apiJSONContentType, body: `{"email":"player@example.com","passphrase":""}`, wantStatus: http.StatusBadRequest, wantCode: apiCodeInvalidRequest},
		{name: "unknown property", contentType: apiJSONContentType, body: `{"email":"player@example.com","passphrase":"wrong","extra":true}`, wantStatus: http.StatusBadRequest, wantCode: apiCodeInvalidJSON},
		{name: "trailing JSON", contentType: apiJSONContentType, body: `{"email":"player@example.com","passphrase":"wrong"}{}`, wantStatus: http.StatusBadRequest, wantCode: apiCodeInvalidJSON},
		{name: "wrong content type", contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: apiCodeUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertAPIError(t, response, test.wantStatus, test.wantCode)
			if len(response.Result().Cookies()) != 0 || len(store.sessions) != 0 {
				t.Fatal("rejected session request issued a credential")
			}
		})
	}
}

func TestAPICrossOriginRejectionIsJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", apiJSONContentType)
	request.Header.Set("Origin", "https://hostile.example")
	response := httptest.NewRecorder()
	newHandler(nil, nil).ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusForbidden, apiCodeForbidden)
}

func TestAPISessionStorageFailuresDoNotAuthenticate(t *testing.T) {
	store := &testStore{sessionErr: errors.New("session storage unavailable")}
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Active: true}, true, nil
	}, store)
	created := apiRequest(handler, http.MethodPost, "/api/v1/sessions",
		`{"email":"player@example.com","passphrase":"good.luck"}`, nil)
	assertAPIError(t, created, http.StatusInternalServerError, apiCodeInternal)
	if len(created.Result().Cookies()) != 0 {
		t.Fatal("failed session storage issued a cookie")
	}

	token := base64.RawURLEncoding.EncodeToString(make([]byte, datastore.SessionTokenBytes))
	resolved := apiRequest(handler, http.MethodGet, "/api/v1/account", "", map[string]string{
		"Authorization": "Bearer " + token,
	})
	assertAPIError(t, resolved, http.StatusInternalServerError, apiCodeInternal)
}

func TestAPIAuthenticationRejectsMalformedAndConflictingCredentials(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, datastore.SessionTokenBytes))
	for _, test := range []struct {
		name       string
		headers    []string
		cookies    []string
		wantStatus int
		wantCode   string
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized, wantCode: apiCodeAuthenticationNeeded},
		{name: "wrong scheme", headers: []string{"Basic " + token}, wantStatus: http.StatusUnauthorized, wantCode: apiCodeAuthenticationNeeded},
		{name: "missing token", headers: []string{"Bearer"}, wantStatus: http.StatusUnauthorized, wantCode: apiCodeAuthenticationNeeded},
		{name: "short token", headers: []string{"Bearer c2hvcnQ"}, wantStatus: http.StatusUnauthorized, wantCode: apiCodeAuthenticationNeeded},
		{name: "token followed by text", headers: []string{"Bearer " + token + " more"}, wantStatus: http.StatusUnauthorized, wantCode: apiCodeAuthenticationNeeded},
		{name: "multiple authorization fields", headers: []string{"Bearer " + token, "Bearer " + token}, wantStatus: http.StatusUnauthorized, wantCode: apiCodeAuthenticationNeeded},
		{name: "multiple cookies", cookies: []string{token, token}, wantStatus: http.StatusUnauthorized, wantCode: apiCodeAuthenticationNeeded},
		{name: "cookie and bearer", headers: []string{"Bearer " + token}, cookies: []string{token}, wantStatus: http.StatusBadRequest, wantCode: apiCodeCredentialsConflict},
		{name: "malformed cookie and bearer", headers: []string{"Bearer " + token}, cookies: []string{"bad"}, wantStatus: http.StatusBadRequest, wantCode: apiCodeCredentialsConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
			for _, header := range test.headers {
				request.Header.Add("Authorization", header)
			}
			for _, cookie := range test.cookies {
				request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
			}
			response := httptest.NewRecorder()
			newHandler(nil, &testStore{}).ServeHTTP(response, request)
			assertAPIError(t, response, test.wantStatus, test.wantCode)
			if response.Header().Get("Location") != "" || strings.Contains(response.Body.String(), "<html") {
				t.Fatalf("API authentication failure returned browser response: %#v", response.Result())
			}
		})
	}
}

func TestAPIDeleteSessionRevokesPresentedTokenAndExpiresCookie(t *testing.T) {
	store := &testStore{faction: datastore.Faction{Name: "Marajanda", Race: game.RaceHuman, Active: true}, found: true}
	handler := newHandler(func(context.Context, string, string) (datastore.Account, bool, error) {
		return datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin", Active: true, Seated: true}, true, nil
	}, store)
	created := apiRequest(handler, http.MethodPost, "/api/v1/sessions",
		`{"email":"admin@example.com","passphrase":"good.luck"}`, nil)
	var session apiSession
	decodeAPIResponse(t, created, &session)

	deleted := apiRequest(handler, http.MethodDelete, "/api/v1/session", "", map[string]string{
		"Authorization": "Bearer " + session.Token,
	})
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 || deleted.Header().Get("Content-Type") != "" {
		t.Fatalf("delete response = %d %q %q", deleted.Code, deleted.Header().Get("Content-Type"), deleted.Body.String())
	}
	cookies := deleted.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].MaxAge != -1 {
		t.Fatalf("expired cookies = %#v", cookies)
	}
	replay := apiRequest(handler, http.MethodGet, "/api/v1/account", "", map[string]string{
		"Authorization": "Bearer " + session.Token,
	})
	assertAPIError(t, replay, http.StatusUnauthorized, apiCodeAuthenticationNeeded)
}

func TestAPICurrentAccountFieldsForBothRoles(t *testing.T) {
	for _, test := range []struct {
		name    string
		account datastore.Account
		store   *testStore
		want    apiAccount
	}{
		{
			name: "configured player",
			account: datastore.Account{Email: "player@example.com", Handle: "wanderer", Role: "player", Active: true,
				Seated: true, Origin: hexg.NewHex(7, -16)},
			store: &testStore{faction: datastore.Faction{Name: "Wayfarers", Race: game.RaceElf, Active: true}, found: true},
			want: apiAccount{Email: "player@example.com", Handle: "wanderer", Role: "player", Active: true,
				Seated: true, Origin: &apiCoordinate{Q: 7, R: -16}, FactionConfigured: true},
		},
		{
			name:    "unconfigured player",
			account: datastore.Account{Email: "new@example.com", Handle: "newcomer", Role: "player", Active: true},
			store:   &testStore{},
			want:    apiAccount{Email: "new@example.com", Handle: "newcomer", Role: "player", Active: true},
		},
		{
			name: "main admin",
			account: datastore.Account{Email: "main@example.com", Handle: "marajanda", Role: "admin", Active: true,
				Seated: true},
			store: &testStore{faction: datastore.Faction{Name: "Marajanda", Race: game.RaceHuman, Active: true}, found: true},
			want: apiAccount{Email: "main@example.com", Handle: "marajanda", Role: "admin", Active: true,
				Seated: true, Origin: &apiCoordinate{}, FactionConfigured: true},
		},
		{
			name:    "assistant admin",
			account: datastore.Account{Email: "admin@example.com", Handle: "keeper", Role: "admin", Active: false, Seated: true},
			store:   &testStore{},
			want:    apiAccount{Email: "admin@example.com", Handle: "keeper", Role: "admin", Active: false, Seated: true, Origin: &apiCoordinate{}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := make([]byte, datastore.SessionTokenBytes)
			token[0] = 42
			test.store.sessionAccount = test.account
			test.store.sessions = map[string]bool{string(token): true}
			handler := newHandler(nil, test.store)
			response := apiRequest(handler, http.MethodGet, "/api/v1/account", "", map[string]string{
				"Authorization": "bearer " + base64.RawURLEncoding.EncodeToString(token),
			})
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var got apiAccount
			decodeAPIResponse(t, response, &got)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("account = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRequireAPIRoleReturnsJSONForbiddenAndPassesContext(t *testing.T) {
	token := make([]byte, datastore.SessionTokenBytes)
	store := &testStore{
		sessionAccount: datastore.Account{Email: "player@example.com", Role: "player"},
		sessions:       map[string]bool{string(token): true},
	}
	app := &application{store: store}
	called := false
	handler := app.requireAPIRole("admin", func(http.ResponseWriter, *http.Request) { called = true })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin-example", nil)
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(token))
	response := httptest.NewRecorder()
	handler(response, request)
	assertAPIError(t, response, http.StatusForbidden, apiCodeForbidden)
	if called {
		t.Fatal("role-protected handler was called")
	}

	var got apiAuthentication
	allowed := app.requireAPIRole("player", func(_ http.ResponseWriter, r *http.Request) {
		got = apiAuthenticationFromContext(r.Context())
	})
	allowedResponse := httptest.NewRecorder()
	allowed(allowedResponse, request)
	if got.Account.Email != "player@example.com" || string(got.Token) != string(token) {
		t.Fatalf("context authentication = %#v", got)
	}
}

func apiRequest(handler http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", apiJSONContentType)
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeAPIResponse(t *testing.T, response *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	if response.Header().Get("Content-Type") != apiJSONContentType {
		t.Fatalf("Content-Type = %q, want %q", response.Header().Get("Content-Type"), apiJSONContentType)
	}
	var got apiErrorEnvelope
	decodeAPIResponse(t, response, &got)
	if got.Error.Code != code || got.Error.Message == "" {
		t.Fatalf("error = %#v, want code %q and a message", got.Error, code)
	}
}
