package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

type Job struct {
	ID         string
	ScriptID   string
	Status     string
	LogPath    string
	StartedAt  string
	FinishedAt string
}

var DB *sql.DB

func InitDB(path string) error {
	var err error
	DB, err = sql.Open("sqlite3", path)
	if err != nil {
		return err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		script_id TEXT,
		status TEXT,
		log_path TEXT,
		started_at TEXT,
		finished_at TEXT
	);
	`
	_, err = DB.Exec(schema)
	return err
}

func InsertJob(j Job) error {
	_, err := DB.Exec(
		"INSERT INTO jobs (id, script_id, status, log_path, started_at) VALUES (?, ?, ?, ?, ?)",
		j.ID, j.ScriptID, j.Status, j.LogPath, j.StartedAt,
	)
	return err
}

func UpdateJobStatus(id string, status string, finishedAt string) error {
	_, err := DB.Exec(
		"UPDATE jobs SET status = ?, finished_at = ? WHERE id = ?",
		status, finishedAt, id,
	)
	return err
}

func GetJobByID(id string) (*Job, error) {
	row := DB.QueryRow("SELECT id, script_id, status, log_path, started_at, finished_at FROM jobs WHERE id = ?", id)
	var job Job
	err := row.Scan(&job.ID, &job.ScriptID, &job.Status, &job.LogPath, &job.StartedAt, &job.FinishedAt)
	if err != nil {
		return nil, err
	}
	return &job, nil
}
