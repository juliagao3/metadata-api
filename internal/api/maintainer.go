package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/juliagao3/metadata-api/internal/storage"
)

type maintainerInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func createMaintainer(w http.ResponseWriter, r *http.Request, db database) {
	var input maintainerInput
	if !decodeJSON(w, r, &input, "name", "email") {
		return
	}
	if fields := validateMaintainer(input); len(fields) != 0 {
		writeJSON(w, r, http.StatusUnprocessableEntity, errorResponse{Error: errorDetail{
			Code:    "validation_failed",
			Message: "Request validation failed.",
			Fields:  fields,
		}})
		return
	}

	maintainer, err := db.CreateMaintainer(r.Context(), input.Name, input.Email)
	if err != nil {
		log.Printf("create maintainer failed: %v", err)
		if errors.Is(err, storage.ErrUnavailable) {
			writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable.")
		} else {
			writeError(w, r, http.StatusInternalServerError, "internal_error", "Unable to create maintainer.")
		}
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/maintainers/%d", maintainer.ID))
	writeJSON(w, r, http.StatusCreated, maintainer)
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
