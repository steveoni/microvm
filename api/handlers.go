package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/steveoni/microvm/db"
	"github.com/steveoni/microvm/jobs"
)

type UploadResponse struct {
	ScriptID string `json:"script_id"`
}

func UploadScript(w http.ResponseWriter, r *http.Request) {
    file, _, err := r.FormFile("script")
    if err != nil {
        http.Error(w, "invalid file upload", http.StatusBadRequest)
        return
    }
    defer file.Close()

    scriptID := uuid.NewString()
    scriptPath := filepath.Join("scripts", scriptID+".sh")

    if err := os.MkdirAll("scripts", 0755); err != nil {
        http.Error(w, "failed to prepare storage", http.StatusInternalServerError)
        return
    }

    out, err := os.Create(scriptPath)
    if err != nil {
        http.Error(w, "failed to save script", http.StatusInternalServerError)
        return
    }
    defer func() {
        if closeErr := out.Close(); closeErr != nil {
            // Log the error or handle it appropriately
            // For now, we'll ignore it as the main operation succeeded
        }
    }()

    if _, err := io.Copy(out, file); err != nil {
        http.Error(w, "failed to write script", http.StatusInternalServerError)
        // Clean up the partially written file
        os.Remove(scriptPath)
        return
    }

    resp := UploadResponse{ScriptID: scriptID}
    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        http.Error(w, "failed to encode response", http.StatusInternalServerError)
        return
    }
}

func RunScript(w http.ResponseWriter, r *http.Request) {
    scriptID := chi.URLParam(r, "id")
    scriptPath := filepath.Join("scripts", scriptID+".sh")

    if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
        http.Error(w, "script not found", http.StatusNotFound)
        return
    }

    info, err := jobs.EnqueueScript(scriptID)
    if err != nil {
        http.Error(w, "failed to enqueue job", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(map[string]interface{}{
        "job_id": info.ID,
    }); err != nil {
        http.Error(w, "failed to encode response", http.StatusInternalServerError)
        return
    }
}


func GetJobStatusHandler(w http.ResponseWriter, r *http.Request) {
    jobID := chi.URLParam(r, "id")

    job, err := db.GetJobByID(jobID)
    if err != nil {
        http.Error(w, "job not found", http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(job); err != nil {
        http.Error(w, "failed to encode job data", http.StatusInternalServerError)
        return
    }
}

func GetJobLogHandler(w http.ResponseWriter, r *http.Request) {
    jobID := chi.URLParam(r, "id")

    logPath := filepath.Join("logs", jobID+".log")
    content, err := os.ReadFile(logPath)
    if err != nil {
        http.Error(w, "log not found", http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "text/plain")
    if _, err := w.Write(content); err != nil {
        http.Error(w, "failed to write log content", http.StatusInternalServerError)
        return
    }
}