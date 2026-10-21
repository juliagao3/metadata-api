package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/juliagao3/metadata-api/internal/storage"
)

type maintainerInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func createMaintainer(w http.ResponseWriter, r *http.Request, db database) {
	input, ok := decodeMaintainerInput(w, r)
	if !ok {
		return
	}

	maintainer, err := db.CreateMaintainer(r.Context(), input.Name, input.Email)
	if err != nil {
		writeStorageError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/maintainers/%d", maintainer.ID))
	writeJSON(w, r, http.StatusCreated, maintainer)
}

func getMaintainer(w http.ResponseWriter, r *http.Request, db database) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	maintainer, err := db.GetMaintainer(r.Context(), id)
	if err != nil {
		writeStorageError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, maintainer)
}

func listMaintainers(w http.ResponseWriter, r *http.Request, db database) {
	maintainers, err := db.ListMaintainers(r.Context())
	if err != nil {
		writeStorageError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, maintainers)
}

func updateMaintainer(w http.ResponseWriter, r *http.Request, db database) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	input, ok := decodeMaintainerInput(w, r)
	if !ok {
		return
	}
	maintainer, err := db.UpdateMaintainer(r.Context(), id, input.Name, input.Email)
	if err != nil {
		writeStorageError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, maintainer)
}

func deleteMaintainer(w http.ResponseWriter, r *http.Request, db database) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	if err := db.DeleteMaintainer(r.Context(), id); err != nil {
		writeStorageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeMaintainerInput(w http.ResponseWriter, r *http.Request) (maintainerInput, bool) {
	var input maintainerInput
	if !decodeJSON(w, r, &input, "name", "email") {
		return input, false
	}
	if fields := validateMaintainer(input); len(fields) != 0 {
		writeJSON(w, r, http.StatusUnprocessableEntity, errorResponse{Error: errorDetail{
			Code:    "validation_failed",
			Message: "Request validation failed.",
			Fields:  fields,
		}})
		return input, false
	}
	return input, true
}

func validateMaintainer(input maintainerInput) map[string]string {
	fields := make(map[string]string)
	if strings.TrimSpace(input.Name) == "" {
		fields["name"] = "must not be blank"
	} else if utf8.RuneCountInString(input.Name) > 200 {
		fields["name"] = "must not exceed 200 characters"
	}
	if strings.TrimSpace(input.Email) == "" {
		fields["email"] = "is required"
	} else if utf8.RuneCountInString(input.Email) > 254 {
		fields["email"] = "must not exceed 254 characters"
	} else {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Name != "" || strings.TrimSpace(input.Email) != input.Email ||
			strings.HasPrefix(input.Email, "<") || strings.HasSuffix(input.Email, ">") {
			fields["email"] = "must be a plain email address without a display name or surrounding whitespace"
		}
	}
	return fields
}

func writeStorageError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, storage.ErrMaintainerInUse) {
		writeError(w, r, http.StatusConflict, "maintainer_in_use", "Delete this maintainer's applications first.")
		return
	}
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "Requested resource does not exist.")
		return
	}
	log.Printf("storage operation failed: %v", err)
	if errors.Is(err, storage.ErrUnavailable) {
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable.")
	} else {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "Database operation failed.")
	}
}

func resourceID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	text := r.PathValue("id")
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil || id <= 0 || strings.IndexFunc(text, func(c rune) bool {
		return c < '0' || c > '9'
	}) != -1 {
		writeError(w, r, http.StatusBadRequest, "invalid_request", "ID must be a positive signed 64-bit integer written using decimal digits.")
		return 0, false
	}
	return id, true
}
