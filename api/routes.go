package api

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

func NewRouter() http.Handler {
	r := chi.NewRouter()

	r.Post("/scripts", UploadScript)
	r.Post("/scripts/{id}/run", RunScript)
	r.Get("/jobs/{id}", GetJobStatusHandler)
	r.Get("/jobs/{id}/logs", GetJobLogHandler)

	return r
}
