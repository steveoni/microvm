package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/steveoni/microvm/db"
	"github.com/steveoni/microvm/runner"
)

const (
	TypeRunScript = "script:run"
)

type RunScriptPayload struct {
	ScriptID string
	JobID    string // Add this field
}

var Client *asynq.Client

func InitClient(redisAddr string) error {
	Client = asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return nil
}

func EnqueueScript(scriptID string) (*asynq.TaskInfo, error) {
    jobID := uuid.NewString()
    payload, err := json.Marshal(RunScriptPayload{
        ScriptID: scriptID,
        JobID:    jobID,
    })
    if err != nil {
        return nil, err
    }

    // Store job reference BEFORE enqueuing
    startedAt := time.Now().Format(time.RFC3339)
	err = db.InsertJob(db.Job{
		ID:        jobID,
		ScriptID:  scriptID,
		Status:    "pending",
		LogPath:   filepath.Join("logs", jobID+".log"),
		StartedAt: startedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create job record: %w", err)
	}
    
    // Only enqueue after successful database insert
    task := asynq.NewTask(TypeRunScript, payload)
    info, err := Client.Enqueue(task)
    if err != nil {
        return nil, err
    }
    
    // Return the job ID we generated (not the task ID)
    info.ID = jobID
    return info, nil
}



func NewServer(redisAddr string) *asynq.Server {
	return asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{
		Concurrency: 1,
		 Queues: map[string]int{
			"default": 10,
		},
		// Enable more verbose logging
		LogLevel: asynq.DebugLevel,
		StrictPriority: true, 
		
	})
}

func Handler() asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		switch t.Type() {
		case TypeRunScript:
			var payload RunScriptPayload
			if err := json.Unmarshal(t.Payload(), &payload); err != nil {
				return err
			}

			jobID := payload.JobID
			scriptPath := filepath.Join("scripts", payload.ScriptID+".sh")
			logPath := filepath.Join("logs", jobID+".log")

			db.UpdateJobStatus(jobID, "running", "")

			cfg := runner.VMConfig{
				KernelImagePath: "vm/images/vmlinux",
    			RootFSPath:      "vm/images/rootfs.ext4",
				ScriptPath:      scriptPath,
				LogPath:         logPath,
				MemSizeMB:       128,
				CPUs:            1,
			}
			err := runner.RunInVM(ctx, cfg)
			status := "success"
			if err != nil {
				status = "failed"
			}
			return db.UpdateJobStatus(jobID, status, time.Now().Format(time.RFC3339))
		default:
			return fmt.Errorf("unknown task type: %s", t.Type())
		}
	})
}
