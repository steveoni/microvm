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
}

var Client *asynq.Client

func InitClient(redisAddr string) error {
	Client = asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return nil
}

func EnqueueScript(scriptID string) (*asynq.TaskInfo, error) {
	payload, err := json.Marshal(RunScriptPayload{ScriptID: scriptID})
	if err != nil {
		return nil, err
	}

	task := asynq.NewTask(TypeRunScript, payload)
	return Client.Enqueue(task)
}




func NewServer(redisAddr string) *asynq.Server {
	return asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{
		Concurrency: 1,
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

			jobID := uuid.NewString()
			scriptPath := filepath.Join("scripts", payload.ScriptID+".sh")
			logPath := filepath.Join("logs", jobID+".log")

			startedAt := time.Now().Format(time.RFC3339)
			_ = db.InsertJob(db.Job{
				ID:        jobID,
				ScriptID:  payload.ScriptID,
				Status:    "running",
				LogPath:   logPath,
				StartedAt: startedAt,
			})

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
