package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/juliagao3/metadata-api/internal/storage"
)

func createApplication(w http.ResponseWriter, r *http.Request, db database) {
	var input storage.Application
	if !decodeJSON(w, r, &input, "title", "version", "company", "website", "source", "license", "description", "maintainer_id") {
		return
	}
	fields := validateApplication(input)
	if input.MaintainerID <= 0 {
		fields["maintainer_id"] = "must be a positive signed 64-bit integer"
	}
	if len(fields) != 0 {
		writeValidationError(w, r, fields)
		return
	}
	application, err := db.CreateApplication(r.Context(), input)
	if err != nil {
		if errors.Is(err, storage.ErrForeignKey) {
			writeValidationError(w, r, map[string]string{"maintainer_id": "must reference an existing maintainer"})
			return
		}
		writeStorageError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/applications/%d", application.ID))
	writeJSON(w, r, http.StatusCreated, application)
}

func getApplication(w http.ResponseWriter, r *http.Request, db database) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	application, err := db.GetApplication(r.Context(), id)
	if err != nil {
		writeStorageError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, application)
}

func listApplications(w http.ResponseWriter, r *http.Request, db database) {
	applications, err := db.ListApplications(r.Context())
	if err != nil {
		writeStorageError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, applications)
}

func updateApplication(w http.ResponseWriter, r *http.Request, db database) {
	id, ok := resourceID(w, r)
	if !ok {
		return
	}
	var input storage.Application
	if !decodeJSON(w, r, &input, "title", "version", "company", "website", "source", "license", "description") {
		return
	}
	if fields := validateApplication(input); len(fields) != 0 {
		writeValidationError(w, r, fields)
		return
	}
	application, err := db.UpdateApplication(r.Context(), id, input)
	if err != nil {
		writeStorageError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, application)
}

func validateApplication(input storage.Application) map[string]string {
	fields := make(map[string]string)
	for _, field := range []struct {
		name     string
		value    string
		max      int
		required bool
	}{
		{"title", input.Title, 200, true},
		{"version", input.Version, 100, true},
		{"company", input.Company, 200, false},
		{"website", input.Website, 2048, false},
		{"source", input.Source, 2048, false},
		{"license", input.License, 100, false},
		{"description", input.Description, 10000, false},
	} {
		if field.required && strings.TrimSpace(field.value) == "" {
			fields[field.name] = "must not be blank"
		} else if utf8.RuneCountInString(field.value) > field.max {
			fields[field.name] = fmt.Sprintf("must not exceed %d characters", field.max)
		}
	}
	return fields
}
