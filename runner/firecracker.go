package runner

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/google/uuid"
)

// VMConfig defines how each Firecracker microVM should be configured
type VMConfig struct {
	KernelImagePath string
	RootFSPath      string
	ScriptPath      string
	LogPath         string
	MemSizeMB       int64
	CPUs            int64
}

func RunInVM(ctx context.Context, cfg VMConfig) error {
	logFile, err := os.Create(cfg.LogPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer logFile.Close()

	vmID := uuid.New().String()
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("firecracker-%s.sock", vmID))

	// Create a proper logrus logger
    logger := logrus.New()
    logger.SetOutput(logFile)
    logrusEntry := logrus.NewEntry(logger)

	// Correct Drive definition
    drives := []models.Drive{
        {
            DriveID:      firecracker.String("rootfs"),
            PathOnHost:   firecracker.String(cfg.RootFSPath),
            IsRootDevice: firecracker.Bool(true),
            IsReadOnly:   firecracker.Bool(false),
        },
    }

	scriptDrive := filepath.Join(os.TempDir(), fmt.Sprintf("script-%s.ext4", vmID))
	err = createExt4ImageWithScript(cfg.ScriptPath, scriptDrive)
	if err != nil {
		return fmt.Errorf("failed to create script drive: %w", err)
	}
	defer os.Remove(scriptDrive)

	drives = append(drives, models.Drive{
        DriveID:      firecracker.String("script"),
        PathOnHost:   firecracker.String(scriptDrive),
        IsRootDevice: firecracker.Bool(false),
        IsReadOnly:   firecracker.Bool(true),
    })

	machineOpts := []firecracker.Opt{
        firecracker.WithLogger(logrusEntry),
    }


	// Create proper machine config
    fcCfg := firecracker.Config{
		SocketPath:      socketPath,
		KernelImagePath: cfg.KernelImagePath,
		Drives:          drives,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(cfg.CPUs),
			MemSizeMib: firecracker.Int64(cfg.MemSizeMB),
		},
		// Disable networking by not configuring any network interfaces
		NetworkInterfaces: []firecracker.NetworkInterface{},
		JailerCfg: &firecracker.JailerConfig{
			UID: firecracker.Int(1000),
			GID: firecracker.Int(1000),
		},
	}

	vm, err := firecracker.NewMachine(ctx, fcCfg, machineOpts...)
    if err != nil {
        return fmt.Errorf("failed to create VM: %w", err)
    }

    if err := vm.Start(ctx); err != nil {
        return fmt.Errorf("failed to start VM: %w", err)
    }

	

	time.Sleep(10 * time.Second) // allow VM to finish boot + run script

	return vm.StopVMM()
}

func createExt4ImageWithScript(scriptPath, imagePath string) error {
	tmpDir := filepath.Join(os.TempDir(), "vm-script")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	scriptName := filepath.Base(scriptPath)
	destScriptPath := filepath.Join(tmpDir, scriptName)
	input, err := os.ReadFile(scriptPath)
	if err != nil {
		return err
	}
	err = os.WriteFile(destScriptPath, input, 0755)
	if err != nil {
		return err
	}

	size := "10M"
	cmd := exec.Command("truncate", "-s", size, imagePath)
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("mkfs.ext4", "-F", imagePath)
	if err := cmd.Run(); err != nil {
		return err
	}

	mnt := filepath.Join(os.TempDir(), "mnt-script")
	os.MkdirAll(mnt, 0755)
	defer os.RemoveAll(mnt)

	cmd = exec.Command("sudo", "mount", imagePath, mnt)
	if err := cmd.Run(); err != nil {
		return err
	}
	defer exec.Command("sudo", "umount", mnt).Run()

	cmd = exec.Command("sudo", "cp", destScriptPath, filepath.Join(mnt, scriptName))
	return cmd.Run()
}
