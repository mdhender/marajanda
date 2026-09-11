// Copyright (c) 2026 Michael D Henderson.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

const apiJSONContentType = "application/json"

// API error codes are part of the versioned wire contract. Handlers choose the
// narrowest code that describes a failure; the status says its HTTP class and
// the code lets a client respond without matching prose.
const (
	apiCodeInvalidJSON          = "invalid_json"
	apiCodeInvalidRequest       = "invalid_request"
	apiCodeUnsupportedMediaType = "unsupported_media_type"
	apiCodeCredentialsRejected  = "credentials_rejected"
	apiCodeAccountInactive      = "account_inactive"
	apiCodeAuthenticationNeeded = "authentication_required"
	apiCodeCredentialsConflict  = "credentials_conflict"
	apiCodeForbidden            = "forbidden"
	apiCodeFactionNotConfigured = "faction_not_configured"
	apiCodeFactionInactive      = "faction_inactive"
	apiCodeEntityNotFound       = "entity_not_found"
	apiCodeOrderNotFound        = "order_not_found"
	apiCodeTurnClosed           = "turn_closed"
	apiCodeTurnNotFound         = "turn_not_found"
	apiCodePreconditionFailed   = "precondition_failed"
	apiCodeNoOrigin             = "no_origin"
	apiCodeOrderRefused         = "order_refused"
	apiCodeOrderLimit           = "order_limit"
	apiCodeInternal             = "internal_error"
)

// hasAPIJSONContentType reports whether a request body declares the API's JSON
// media type. Parameters such as charset are allowed by the media-type grammar;
// JSON itself remains UTF-8.
func hasAPIJSONContentType(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == apiJSONContentType
}

// decodeAPIJSON reads exactly one JSON value. API requests are closed structs:
// an unknown field is more likely a misspelling or a client/server version
// mismatch than data the server can safely ignore.
func decodeAPIJSON(r io.Reader, dst any) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode JSON: request contains more than one value")
		}
		return fmt.Errorf("decode JSON after first value: %w", err)
	}
	return nil
}

// writeAPIJSON writes one complete JSON response. Marshaling happens before
// the status is committed so an unsupported value cannot leave a successful
// response with a partial body.
func writeAPIJSON(w http.ResponseWriter, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	body = append(body, '\n')
	w.Header().Set("Content-Type", apiJSONContentType)
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	return nil
}

// writeAPIError writes the one error envelope used by every versioned API
// route. Error responses are deliberately small: internal errors are logged by
// their eventual handler, not disclosed through Message.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	_ = writeAPIJSON(w, status, apiErrorEnvelope{
		Error: apiError{Code: code, Message: message},
	})
}

// writeAPINoContent commits a response without a representation or a media
// type. It exists to stop a successful DELETE from accidentally becoming JSON
// containing null or an empty object.
func writeAPINoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
