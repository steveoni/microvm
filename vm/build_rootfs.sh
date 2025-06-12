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
mkdir -p rootfs/{bin,sbin,etc,proc,sys,dev,tmp}

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

# Init script to mount and execute user script
echo "Creating init script..."
cat > $WORK_DIR/rootfs/init <<EOF
#!/bin/sh
export PATH=/bin:/sbin:/usr/bin:/usr/sbin

# Mount essential filesystems
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev

echo "MicroVM init starting..."

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
chmod +x /mnt/script/*.sh

# Execute the script and capture output
echo "===== SCRIPT EXECUTION START =====" 
/mnt/script/*.sh > /dev/ttyS0 2>&1
EXIT_CODE=\$?
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
dd if=/dev/zero of=$WORK_DIR/rootfs.ext4 bs=1M count=50
mkfs.ext4 -F $WORK_DIR/rootfs.ext4
mkdir -p /tmp/rootfs_mount
mount $WORK_DIR/rootfs.ext4 /tmp/rootfs_mount
cp -r $WORK_DIR/rootfs/* /tmp/rootfs_mount/
umount /tmp/rootfs_mount

# Copy output files to the project directory
echo "Copying output files to project directory..."
mkdir -p $OUTPUT_DIR
cp $WORK_DIR/vmlinux $OUTPUT_DIR/
cp $WORK_DIR/rootfs.ext4 $OUTPUT_DIR/

# Clean up
echo "Cleaning up..."
rm -rf $WORK_DIR
echo "VM image build complete! Images saved to $OUTPUT_DIR"