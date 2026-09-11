// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHasAPIJSONContentType(t *testing.T) {
	for _, test := range []struct {
		contentType string
		want        bool
	}{
		{contentType: "application/json", want: true},
		{contentType: "application/json; charset=utf-8", want: true},
		{contentType: "", want: false},
		{contentType: "text/plain", want: false},
		{contentType: "application/problem+json", want: false},
		{contentType: "not a media type", want: false},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/example", nil)
		request.Header.Set("Content-Type", test.contentType)
		if got := hasAPIJSONContentType(request); got != test.want {
			t.Errorf("hasAPIJSONContentType(%q) = %t, want %t", test.contentType, got, test.want)
		}
	}
}

func TestDecodeAPIJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want apiCreateSessionRequest
	}{
		{
			name: "one object",
			body: `{"email":"player@example.com","passphrase":"good.luck"}`,
			want: apiCreateSessionRequest{Email: "player@example.com", Passphrase: "good.luck"},
		},
		{
			name: "surrounding whitespace",
			body: " \n\t{\"email\":\"player@example.com\",\"passphrase\":\"good.luck\"}\n ",
			want: apiCreateSessionRequest{Email: "player@example.com", Passphrase: "good.luck"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got apiCreateSessionRequest
			if err := decodeAPIJSON(strings.NewReader(test.body), &got); err != nil {
				t.Fatalf("decodeAPIJSON() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("decodeAPIJSON() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDecodeAPIJSONRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "malformed", body: `{"email":`},
		{name: "unknown field", body: `{"email":"player@example.com","passphrase":"good.luck","password":"wrong name"}`},
		{name: "second object", body: `{"email":"player@example.com","passphrase":"good.luck"} {}`},
		{name: "trailing token", body: `{"email":"player@example.com","passphrase":"good.luck"} false`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request apiCreateSessionRequest
			if err := decodeAPIJSON(strings.NewReader(test.body), &request); err == nil {
				t.Fatal("decodeAPIJSON() error = nil, want error")
			}
		})
	}
}

func TestWriteAPIJSON(t *testing.T) {
	response := httptest.NewRecorder()
	if err := writeAPIJSON(response, http.StatusCreated, apiTurn{Turn: 4}); err != nil {
		t.Fatalf("writeAPIJSON() error = %v", err)
	}
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
	if got := response.Header().Get("Content-Type"); got != apiJSONContentType {
		t.Fatalf("Content-Type = %q, want %q", got, apiJSONContentType)
	}
	if got, want := response.Body.String(), "{\"turn\":4}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestWriteAPIJSONDoesNotCommitAnUnencodableValue(t *testing.T) {
	response := httptest.NewRecorder()
	if err := writeAPIJSON(response, http.StatusOK, math.Inf(1)); err == nil {
		t.Fatal("writeAPIJSON() error = nil, want error")
	}
	if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "" {
		t.Fatalf("response = status %d, type %q, body %q; want untouched recorder", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestWriteAPIErrorUsesOneEnvelopeForCommonStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusUnprocessableEntity,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			response := httptest.NewRecorder()
			writeAPIError(response, status, apiCodeInvalidRequest, "The request was not accepted.")
			if response.Code != status {
				t.Fatalf("status = %d, want %d", response.Code, status)
			}
			if got := response.Header().Get("Content-Type"); got != apiJSONContentType {
				t.Fatalf("Content-Type = %q, want %q", got, apiJSONContentType)
			}
			var got apiErrorEnvelope
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			want := apiErrorEnvelope{Error: apiError{Code: apiCodeInvalidRequest, Message: "The request was not accepted."}}
			if got != want {
				t.Fatalf("body = %#v, want %#v", got, want)
			}
		})
	}
}

func TestWriteAPINoContentHasNoRepresentation(t *testing.T) {
	response := httptest.NewRecorder()
	writeAPINoContent(response)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "" {
		t.Fatalf("Content-Type = %q, want empty", got)
	}
}

func TestAPIDTOJSONShapes(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "unseated account keeps null origin",
			value: apiAccount{Email: "player@example.com", Handle: "player", Role: "player", Active: true},
			want:  `{"email":"player@example.com","handle":"player","role":"player","active":true,"seated":false,"origin":null,"factionConfigured":false}`,
		},
		{
			name:  "player game omits seeds",
			value: apiGame{CurrentTurn: 3, Width: 255, Height: 127},
			want:  `{"currentTurn":3,"width":255,"height":127}`,
		},
		{
			name: "unpriced order keeps null cost and exhaustion",
			value: apiOrderEstimate{
				End:    apiCoordinate{Q: 2, R: -1},
				Orders: []apiOrderCost{{Sequence: 1, Kind: "move", From: apiCoordinate{Q: 2, R: -1}, Target: apiCoordinate{Q: 2, R: -1}, To: apiCoordinate{Q: 2, R: -1}}},
			},
			want: `{"allowance":0,"committed":0,"total":0,"residue":0,"overspend":0,"exhaustsAt":null,"end":{"q":2,"r":-1},"orders":[{"sequence":1,"kind":"move","cost":null,"running":0,"exhausts":false,"warning":null,"from":{"q":2,"r":-1},"target":{"q":2,"r":-1},"to":{"q":2,"r":-1}}]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("marshal DTO: %v", err)
			}
			if got := string(encoded); got != test.want {
				t.Fatalf("JSON = %s, want %s", got, test.want)
			}
		})
	}
}
