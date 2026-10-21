package api

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"
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
		writeValidationError(w, r, fields)
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
