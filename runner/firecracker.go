package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

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
    // Get absolute paths
    kernelPath, err := filepath.Abs(cfg.KernelImagePath)
    if err != nil {
        return fmt.Errorf("failed to get absolute path for kernel: %w", err)
    }
    rootfsPath, err := filepath.Abs(cfg.RootFSPath)
    if err != nil {
        return fmt.Errorf("failed to get absolute path for rootfs: %w", err)
    }
    scriptPath, err := filepath.Abs(cfg.ScriptPath)
    if err != nil {
        return fmt.Errorf("failed to get absolute path for script: %w", err)
    }
    logPath, err := filepath.Abs(cfg.LogPath)
    if err != nil {
        return fmt.Errorf("failed to get absolute path for log: %w", err)
    }

    // Create log directory if it doesn't exist
    logDir := filepath.Dir(logPath)
    if err := os.MkdirAll(logDir, 0755); err != nil {
        return fmt.Errorf("failed to create log directory: %w", err)
    }

    // Create a unique directory for all VM-related files
    vmID := uuid.New().String()
    vmDir := filepath.Join(os.TempDir(), fmt.Sprintf("fcvm-%s", vmID))
    if err := os.MkdirAll(vmDir, 0755); err != nil {
        return fmt.Errorf("failed to create VM directory: %w", err)
    }
    defer os.RemoveAll(vmDir) // Clean up ALL VM files on exit
    
    // Create socket in VM directory
    socketPath := filepath.Join(vmDir, "firecracker.sock")
    
    // Set paths for FIFO files - DO NOT CREATE THEM
    fifoPath := filepath.Join(vmDir, "console.fifo")
    metricsPath := filepath.Join(vmDir, "metrics.fifo")
    
    // Open log file
    logFile, err := os.Create(logPath)
    if err != nil {
        return fmt.Errorf("failed to create log file: %w", err)
    }
    defer logFile.Close() // Safe to defer now as we'll read AFTER VM stops
    
    // Logger setup
    logger := logrus.New()
    logger.SetOutput(logFile)
    logrusEntry := logrus.NewEntry(logger)
    logrusEntry.Info("Starting VM process for script:", scriptPath)
    
    // Setup drives
    drives := []models.Drive{
        {
            DriveID:      firecracker.String("rootfs"),
            PathOnHost:   firecracker.String(rootfsPath),
            IsRootDevice: firecracker.Bool(true),
            IsReadOnly:   firecracker.Bool(false),
        },
    }

    // Create script drive
    scriptDrive := filepath.Join(os.TempDir(), fmt.Sprintf("script-%s.ext4", vmID))
    err = createExt4ImageWithScript(scriptPath, scriptDrive)
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

    // Create VM configuration - LET FIRECRACKER CREATE THE FIFO
    fcCfg := firecracker.Config{
        SocketPath:      socketPath,
        KernelImagePath: kernelPath,
        Drives:          drives,
        MachineCfg: models.MachineConfiguration{
            VcpuCount:  firecracker.Int64(cfg.CPUs),
            MemSizeMib: firecracker.Int64(cfg.MemSizeMB),
        },
        JailerCfg: nil,
        NetworkInterfaces: []firecracker.NetworkInterface{},
        LogFifo: fifoPath,
        MetricsFifo: metricsPath,
        LogLevel: "Debug",
        KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off init=/init",
    }
    
    // Create the VM
    vm, err := firecracker.NewMachine(ctx, fcCfg, machineOpts...)
    if err != nil {
        return fmt.Errorf("failed to create VM: %w", err)
    }
    
    // Start the VM FIRST - this creates the FIFO
    logrusEntry.Info("Starting VM...")
    if err := vm.Start(ctx); err != nil {
        return fmt.Errorf("failed to start VM: %w", err)
    }
    
    // AFTER VM starts, read from the FIFO in a goroutine
    done := make(chan struct{})
    go func() {
        defer close(done)
        
        // Give Firecracker a moment to create the FIFO
        time.Sleep(100 * time.Millisecond)
        
        // Open FIFO for reading
        fifo, err := os.Open(fifoPath)
        if err != nil {
            logrusEntry.Errorf("Failed to open FIFO: %v", err)
            return
        }
        defer fifo.Close()
        
        // Write header and copy output
        logFile.WriteString("\n\n===== VM CONSOLE OUTPUT =====\n\n")
        buffer := make([]byte, 4096)
        for {
            n, err := fifo.Read(buffer)
            if n > 0 {
                logFile.Write(buffer[:n])
                logFile.Sync() // Flush immediately
            }
            if err != nil {
                break
            }
        }
        logrusEntry.Info("Finished reading VM output")
    }()
    
    // Wait for VM execution
    logrusEntry.Info("VM started, waiting for execution to complete...")
    time.Sleep(30 * time.Second)
    
    // Stop the VM
    logrusEntry.Info("Stopping VM...")
    if err := vm.StopVMM(); err != nil {
        logrusEntry.Warnf("Error stopping VM: %v", err)
    }
    
    // Wait for output collection to finish
    select {
    case <-done:
        logrusEntry.Info("Console output captured")
    case <-time.After(2 * time.Second):
        logrusEntry.Warn("Timed out waiting for console output")
    }
    
    // Explicitly flush log file
    logFile.Sync()
    
    return nil
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

    // Mount the filesystem
    cmd = exec.Command("sudo", "mount", "-o", "loop", imagePath, mnt)
    if err := cmd.Run(); err != nil {
        return err
    }
    defer exec.Command("sudo", "umount", mnt).Run()

    // Copy directly to the root of the ext4 image - REMOVE THE NESTED DIRECTORY
    cmd = exec.Command("sudo", "cp", destScriptPath, filepath.Join(mnt, scriptName))
    return cmd.Run()
}