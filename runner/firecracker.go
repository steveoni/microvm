package runner

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	EnableNetwork bool
}

// setupNetworking creates and configures a TAP device for VM networking
func setupNetworking(vmID string, logger *logrus.Entry) (string, error) {
    tapName := fmt.Sprintf("fc-tap-%s", vmID[:8])
    
    // Create TAP device
    cmd := exec.Command("sudo", "ip", "tuntap", "add", tapName, "mode", "tap")
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("failed to create TAP device: %w", err)
    }
    
    // Set TAP device up
    cmd = exec.Command("sudo", "ip", "link", "set", tapName, "up")
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("failed to set TAP device up: %w", err)
    }
    
    // Find default interface
    defIface, err := getDefaultInterface()
    if err != nil {
        logger.Warnf("Failed to get default interface: %v", err)
        defIface = "eth0" // Fallback
    }
    logger.Infof("Using %s as default interface", defIface)
    
    // Create bridge if it doesn't exist
    bridgeName := "fcbr0"
    cmd = exec.Command("sudo", "ip", "link", "show", bridgeName)
    if err := cmd.Run(); err != nil {
        // Bridge doesn't exist, create it
        cmd = exec.Command("sudo", "ip", "link", "add", bridgeName, "type", "bridge")
        if err := cmd.Run(); err != nil {
            logger.Warnf("Failed to create bridge: %v", err)
        }
        
        // Set bridge up
        cmd = exec.Command("sudo", "ip", "link", "set", bridgeName, "up")
        if err := cmd.Run(); err != nil {
            logger.Warnf("Failed to set bridge up: %v", err)
        }
        
        // Configure bridge IP
        cmd = exec.Command("sudo", "ip", "addr", "add", "192.168.100.1/24", "dev", bridgeName)
        if err := cmd.Run(); err != nil {
            logger.Warnf("Failed to set bridge IP: %v", err)
        }
    }
    
    // Add TAP to bridge
    cmd = exec.Command("sudo", "ip", "link", "set", tapName, "master", bridgeName)
    if err := cmd.Run(); err != nil {
        logger.Warnf("Failed to add TAP to bridge: %v", err)
    }
    
    // Enable IP forwarding
    cmd = exec.Command("sudo", "sysctl", "-w", "net.ipv4.ip_forward=1")
    if err := cmd.Run(); err != nil {
        logger.Warnf("Failed to enable IP forwarding: %v", err)
    }
    
    // Check if MASQUERADE rule already exists
    cmd = exec.Command("sudo", "iptables", "-t", "nat", "-C", "POSTROUTING", 
                     "-s", "192.168.100.0/24", "-o", defIface, "-j", "MASQUERADE")
    if err := cmd.Run(); err != nil {
        // Rule doesn't exist, add it
        cmd = exec.Command("sudo", "iptables", "-t", "nat", "-A", "POSTROUTING", 
                        "-s", "192.168.100.0/24", "-o", defIface, "-j", "MASQUERADE")
        if err := cmd.Run(); err != nil {
            logger.Warnf("Failed to add MASQUERADE rule: %v", err)
        }
    }
    
    // Allow outgoing traffic from TAP/bridge
    cmd = exec.Command("sudo", "iptables", "-C", "FORWARD",
                     "-i", bridgeName, "-o", defIface, "-j", "ACCEPT")
    if err := cmd.Run(); err != nil {
        cmd = exec.Command("sudo", "iptables", "-A", "FORWARD",
                         "-i", bridgeName, "-o", defIface, "-j", "ACCEPT")
        if err := cmd.Run(); err != nil {
            logger.Warnf("Failed to add outgoing FORWARD rule: %v", err)
        }
    }
    
    // Allow established/related traffic back
    cmd = exec.Command("sudo", "iptables", "-C", "FORWARD",
                     "-i", defIface, "-o", bridgeName, "-m", "state",
                     "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")
    if err := cmd.Run(); err != nil {
        cmd = exec.Command("sudo", "iptables", "-A", "FORWARD",
                         "-i", defIface, "-o", bridgeName, "-m", "state",
                         "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")
        if err := cmd.Run(); err != nil {
            logger.Warnf("Failed to add incoming FORWARD rule: %v", err)
        }
    }
    
    logger.Infof("Network bridge %s setup with TAP %s connected to %s", 
                bridgeName, tapName, defIface)
    
    return tapName, nil
}

// cleanupNetworking removes network resources
func cleanupNetworking(tapName string, logger *logrus.Entry) {
    if tapName == "" {
        return
    }
    
    // Remove TAP device from bridge
    cmd := exec.Command("sudo", "ip", "link", "set", tapName, "nomaster")
    if err := cmd.Run(); err != nil {
        logger.Warnf("Failed to remove TAP from bridge: %v", err)
    }
    
    // Delete TAP device
    cmd = exec.Command("sudo", "ip", "link", "delete", tapName)
    if err := cmd.Run(); err != nil {
        logger.Warnf("Failed to delete TAP device: %v", err)
    } else {
        logger.Infof("Deleted TAP device %s", tapName)
    }
    
}

// getDefaultInterface finds the default network interface
func getDefaultInterface() (string, error) {
    // Get the interface with default route
    cmd := exec.Command("sh", "-c", "ip route | grep default | cut -d ' ' -f 5")
    output, err := cmd.Output()
    if err != nil {
        return "", err
    }
    
    ifName := strings.TrimSpace(string(output))
    if ifName == "" {
        return "", fmt.Errorf("no default interface found")
    }
    
    return ifName, nil
}

// generateRandomMac creates a random MAC address for the VM interface
func generateRandomMac() string {
    buf := make([]byte, 6)
    _, err := rand.Read(buf)
    if err != nil {
        // Fallback in case of error
        return "02:00:00:00:00:01"
    }
    
    // Ensure unicast and locally administered
    buf[0] = (buf[0] | 2) & 0xfe
    return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
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

	// Setup networking if enabled
    var tapName string
    if cfg.EnableNetwork {
        logrusEntry.Info("Setting up networking for VM...")
        var err error
        tapName, err = setupNetworking(vmID, logrusEntry)
        if err != nil {
            logrusEntry.Warnf("Failed to setup networking: %v", err)
        } else {
            defer cleanupNetworking(tapName, logrusEntry)
        }
    }

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

	// Configure networking interfaces if enabled
    var networkInterfaces []firecracker.NetworkInterface
    kernelArgs := "console=ttyS0 reboot=k panic=1 pci=off init=/init"
    
    if cfg.EnableNetwork && tapName != "" {
        // Generate MAC address for guest
        guestMac := generateRandomMac()
        
        // Add network interface config
        networkInterfaces = append(networkInterfaces, firecracker.NetworkInterface{
            StaticConfiguration: &firecracker.StaticNetworkConfiguration{
                MacAddress:  guestMac,
                HostDevName: tapName,
            },
        })
        
        // Modify kernel args to include network config
        // Configure static IP for predictability
        kernelArgs += " ip=192.168.100.2::192.168.100.1:255.255.255.0::eth0:off"
        logrusEntry.Infof("Network interface configured with MAC %s on TAP device %s", 
                           guestMac, tapName)
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
		JailerCfg:         nil,
		NetworkInterfaces: networkInterfaces,
		LogFifo:           fifoPath,
		MetricsFifo:       metricsPath,
		LogLevel:          "Debug",
		KernelArgs:        kernelArgs,
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
	if err:= logFile.Sync(); err != nil {
        logrusEntry.Errorf("Failed to flush log file: %v", err)
    }

	return nil
}

func createExt4ImageWithScript(scriptPath, imagePath string) error {
	tmpDir := filepath.Join(os.TempDir(), "vm-script")
	 if err := os.MkdirAll(tmpDir, 0755); err != nil {
        return fmt.Errorf("failed to create temp directory: %w", err)
    }
	defer func() {
        if err := os.RemoveAll(tmpDir); err != nil {
            // Just log this error since we're in a defer
            fmt.Fprintf(os.Stderr, "warning: failed to remove temp directory: %v\n", err)
        }
    }()

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
	if err := os.MkdirAll(mnt, 0755); err != nil {
        return fmt.Errorf("failed to create mount directory: %w", err)
    }
    defer func() {
        if err := os.RemoveAll(mnt); err != nil {
            fmt.Fprintf(os.Stderr, "warning: failed to remove mount directory: %v\n", err)
        }
    }()

	// Mount the filesystem
	cmd = exec.Command("sudo", "mount", "-o", "loop", imagePath, mnt)
	if err := cmd.Run(); err != nil {
		return err
	}
	defer func() {
        if err := exec.Command("sudo", "umount", mnt).Run(); err != nil {
            fmt.Fprintf(os.Stderr, "warning: failed to unmount directory: %v\n", err)
        }
    }()


	// Copy directly to the root of the ext4 image - REMOVE THE NESTED DIRECTORY
	cmd = exec.Command("sudo", "cp", destScriptPath, filepath.Join(mnt, scriptName))
	return cmd.Run()
}
