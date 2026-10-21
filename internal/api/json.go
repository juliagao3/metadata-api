package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"slices"
	"unicode/utf8"
)

const maxRequestBytes = 64 * 1024

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, allowedFields ...string) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		writeDecodeError(w, r, err)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeDecodeError(w, r, err)
		return false
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil || !utf8.Valid(raw) {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Body must be a JSON object.")
		return false
	}
	for field, value := range fields {
		if !slices.Contains(allowedFields, field) || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			writeError(w, r, http.StatusBadRequest, "invalid_request", "Body contains an unknown field or null value.")
			return false
		}
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "Body contains an invalid field type.")
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "Body exceeds 65,536 bytes.")
		return
	}
	writeError(w, r, http.StatusBadRequest, "invalid_request", "Body must contain exactly one valid JSON object.")
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, r, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
