#!/bin/bash
set -e

WORK_DIR=$(mktemp -d)
OUTPUT_DIR="/home/steveoni/Documents/personal/microvm/vm/images"

echo "Creating working directory: $WORK_DIR"
echo "Output directory will be: $OUTPUT_DIR"

# Download pre-built kernel known to work with Firecracker
echo "Downloading pre-built Firecracker-compatible kernel..."
wget https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin -O $WORK_DIR/vmlinux
chmod +x $WORK_DIR/vmlinux

# Create minimal rootfs
cd $WORK_DIR
echo "Creating rootfs structure..."
mkdir -p rootfs/{bin,sbin,etc,proc,sys,dev,tmp,usr/local/lib,usr/local/bin}

# Download a pre-built static binary for BusyBox
cd $WORK_DIR
wget https://busybox.net/downloads/binaries/1.35.0-x86_64-linux-musl/busybox -O rootfs/bin/busybox
chmod +x rootfs/bin/busybox
cd rootfs
for dir in bin sbin usr/bin usr/sbin; do
    mkdir -p $dir
done
for applet in $(./bin/busybox --list); do
    for dir in bin sbin usr/bin usr/sbin; do
        if [ -d "$dir" ]; then
            ln -sf /bin/busybox $dir/$applet 2>/dev/null || true
        fi
    done
done

# Install minimal Python using a truly static build
cd $WORK_DIR
echo "Installing minimal Python (static build)..."

# Install a working Python (guaranteed solution)
mkdir -p rootfs/bin

# Download a truly static Python build with no dependencies
wget https://github.com/indygreg/python-build-standalone/releases/download/20240107/cpython-3.11.7+20240107-x86_64-unknown-linux-musl-install_only.tar.gz -O python.tar.gz
mkdir -p python
tar -xzf python.tar.gz -C python

# Find and copy the actual Python binary (not config scripts)
echo "Looking for Python binary..."
PYTHON_BIN=$(find python -type f -executable -name "python3.*" ! -name "*-config" | head -1)
if [ -n "$PYTHON_BIN" ]; then
    echo "Found Python binary at $PYTHON_BIN"
    cp $PYTHON_BIN rootfs/bin/python3.11
    chmod +x rootfs/bin/python3.11
    
    # Create direct symlinks to the actual binary
    ln -sf python3.11 rootfs/bin/python3
    ln -sf python3.11 rootfs/bin/python
else
    echo "ERROR: Python binary not found!"
    exit 1
fi

# Verify installation
echo "Verifying Python installation:"
find rootfs/bin -name "python*"

# Copy required Python libraries
echo "Copying Python libraries..."
mkdir -p rootfs/usr/lib
cp -a $(find python -name "libpython3.11.so*" 2>/dev/null) rootfs/usr/lib/ || true

# If using a static build, add the Python standard library
mkdir -p rootfs/usr/lib/python3.11
cp -a $(find python -name "lib-dynload" 2>/dev/null) rootfs/usr/lib/python3.11/ || true
cp -a $(find python -path "*/lib/python3.11" -type d 2>/dev/null) rootfs/usr/lib/ || true

# Fix Python standard library paths
echo "Setting up Python environment..."
mkdir -p rootfs/install/lib
cp -a $(find python -path "*/lib/python3.11" -type d) rootfs/install/lib/ || true

# Ensure core modules are present (especially encodings)
if [ -d "$(find python -path "*/lib/python3.11" -type d | head -1)" ]; then
    PYLIB=$(find python -path "*/lib/python3.11" -type d | head -1)
    cp -a $PYLIB/* rootfs/install/lib/python3.11/ || true
    
    # Explicitly verify critical modules
    if [ -d "$PYLIB/encodings" ]; then
        echo "Found encodings module, copying to correct location"
        mkdir -p rootfs/install/lib/python3.11/encodings/
        cp -a $PYLIB/encodings/* rootfs/install/lib/python3.11/encodings/
    fi
fi

# Create a Python startup script that sets paths correctly
cat > rootfs/bin/python-wrapper <<'EOF'
#!/bin/sh
export PYTHONHOME=/install
exec /bin/python3.11 "$@"
EOF
chmod +x rootfs/bin/python-wrapper

# Update symlinks to use the wrapper
ln -sf /bin/python-wrapper rootfs/bin/python
ln -sf /bin/python-wrapper rootfs/bin/python3

# Create ld.so.conf to ensure libraries are found
echo "/usr/local/lib" > rootfs/etc/ld.so.conf

# Init script to mount and execute user script
echo "Creating init script..."
cat > $WORK_DIR/rootfs/init <<EOF
#!/bin/sh
export PATH=/bin:/sbin:/usr/bin:/usr/sbin:/usr/local/bin
export LD_LIBRARY_PATH=/usr/local/lib
export PYTHONPATH=/mnt/script

# Mount essential filesystems
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev

echo "MicroVM init starting..."

# Setup networking if eth0 exists
if ip link show eth0 >/dev/null 2>&1; then
    echo "Setting up networking..."
    # Configure eth0 (should already have IP from kernel boot args)
    ip link set eth0 up
    ip addr show eth0
    
    # Configure DNS
    echo "nameserver 8.8.8.8" > /etc/resolv.conf
    echo "nameserver 1.1.1.1" >> /etc/resolv.conf
    
    # Test connectivity
    echo "Testing network connectivity..."
    ping -c 1 -w 2 8.8.8.8 || echo "Cannot reach DNS server"
fi

# Enhanced network diagnostics
echo "Running network diagnostics..."
echo "IP configuration:"
ip addr show
echo "Routing table:"
ip route
echo "Testing DNS server connectivity:"
ping -c 1 8.8.8.8 || echo "Cannot ping Google DNS"
echo "Testing DNS resolution:"
nslookup google.com || echo "DNS resolution failed"
echo "Testing DNS resolution with busybox:"
busybox nslookup google.com || echo "Busybox DNS resolution failed"


# Mount script drive
mkdir -p /mnt/script
mount /dev/vdb /mnt/script
if [ \$? -ne 0 ]; then
    echo "ERROR: Failed to mount script drive!"
    sleep 5
    poweroff -f
fi

# Debug output
echo "Script drive mounted, contents:"
ls -la /mnt/script
echo "System information:"
uname -a
echo "Available binaries:"
ls -la /usr/local/bin /bin | grep python

# Network status if available
if ip link show eth0 >/dev/null 2>&1; then
    echo "Network configuration:"
    ip addr show
    echo "DNS configuration:"
    cat /etc/resolv.conf
fi

echo "Library path:"
echo \$LD_LIBRARY_PATH

# Execute the script based on file extension
echo "===== SCRIPT EXECUTION START ====="
echo "Python version: \$(python3 --version 2>&1 || echo 'Not available')"
echo "PATH: \$PATH"

for script in /mnt/script/*; do
    if [ -f "\$script" ]; then
        case "\${script##*.}" in
            py)
                echo "Running Python script: \$script"
                if command -v python3 > /dev/null; then
                    python3 "\$script" 2>&1
                    EXIT_CODE=\$?
                else
                    echo "ERROR: Python is not properly installed in this VM"
                    EXIT_CODE=127
                fi
                ;;
            sh|bash)
                echo "Running shell script: \$script"
                sh "\$script" 2>&1
                EXIT_CODE=\$?
                ;;
            *)
                echo "Running as shell script: \$script"
                sh "\$script" 2>&1
                EXIT_CODE=\$?
                ;;
        esac
    fi
done
echo "===== SCRIPT EXECUTION END (EXIT CODE: \$EXIT_CODE) ====="

# Power off the VM when done
sync
echo "Powering off VM..."
poweroff -f
EOF

# Make sure init is executable
chmod +x $WORK_DIR/rootfs/init

# Create an ext4 filesystem
echo "Creating filesystem image..."
dd if=/dev/zero of=$WORK_DIR/rootfs.ext4 bs=1M count=600
mkfs.ext4 -F $WORK_DIR/rootfs.ext4

# Mount and copy files - be careful with permissions
mkdir -p /tmp/rootfs_mount
sudo mount -o loop $WORK_DIR/rootfs.ext4 /tmp/rootfs_mount
sudo cp -a $WORK_DIR/rootfs/* /tmp/rootfs_mount/
# Ensure proper permissions inside the filesystem
sudo chmod -R 755 /tmp/rootfs_mount/bin /tmp/rootfs_mount/sbin /tmp/rootfs_mount/usr
sudo chmod 755 /tmp/rootfs_mount/init
sudo umount /tmp/rootfs_mount

# Copy output files to the project directory
echo "Copying output files to project directory..."
# Create output directory with proper permissions
sudo mkdir -p $OUTPUT_DIR
sudo chmod 755 $OUTPUT_DIR
# Copy the files with sudo
sudo cp $WORK_DIR/vmlinux $OUTPUT_DIR/
sudo cp $WORK_DIR/rootfs.ext4 $OUTPUT_DIR/
# Set proper ownership and permissions
sudo chown $(whoami):$(whoami) $OUTPUT_DIR/*
sudo chmod 666 $OUTPUT_DIR/rootfs.ext4
sudo chmod 644 $OUTPUT_DIR/vmlinux

# Clean up
echo "Cleaning up..."
rm -rf $WORK_DIR
echo "VM image build complete! Images saved to $OUTPUT_DIR"