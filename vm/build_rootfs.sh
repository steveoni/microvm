#!/bin/bash
# filepath: /Users/mac/Documents/personal/microvm/vm/build_rootfs.sh
set -e

WORK_DIR=$(mktemp -d)
KERNEL_VERSION="5.15.0"
BUSYBOX_VERSION="1.33.1"

# Download and compile Linux kernel
cd $WORK_DIR
wget https://cdn.kernel.org/pub/linux/kernel/v5.x/linux-${KERNEL_VERSION}.tar.xz
tar xf linux-${KERNEL_VERSION}.tar.xz
cd linux-${KERNEL_VERSION}

# Configure minimal kernel
cat > .config <<EOF
CONFIG_BLK_DEV=y
CONFIG_BLK_DEV_LOOP=y
CONFIG_EXT4_FS=y
# Minimal config for microvm
CONFIG_VIRTIO=y
CONFIG_VIRTIO_BLK=y
CONFIG_VIRTIO_PCI=y
CONFIG_PCI=y
CONFIG_NET=y
# Other necessary options
CONFIG_BINFMT_ELF=y
CONFIG_BINFMT_SCRIPT=y
CONFIG_TTY=y
CONFIG_SERIAL_8250=y
CONFIG_SERIAL_8250_CONSOLE=y
CONFIG_PRINTK=y
EOF

make -j$(nproc)
cp arch/x86/boot/bzImage $WORK_DIR/vmlinux

# Create minimal rootfs
cd $WORK_DIR
mkdir -p rootfs/{bin,sbin,etc,proc,sys,dev,tmp}

# Download and build busybox
wget https://busybox.net/downloads/busybox-${BUSYBOX_VERSION}.tar.bz2
tar xf busybox-${BUSYBOX_VERSION}.tar.bz2
cd busybox-${BUSYBOX_VERSION}
make defconfig
sed -i 's/# CONFIG_STATIC is not set/CONFIG_STATIC=y/' .config
make -j$(nproc)
make install CONFIG_PREFIX=$WORK_DIR/rootfs

# Init script to mount and execute user script
cat > $WORK_DIR/rootfs/init <<EOF
#!/bin/sh
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev

# Mount script drive
mkdir -p /mnt/script
mount /dev/vdb /mnt/script
chmod +x /mnt/script/*.sh

# Execute the script and capture output
echo "===== SCRIPT EXECUTION START =====" 
/mnt/script/*.sh > /dev/ttyS0 2>&1
EXIT_CODE=$?
echo "===== SCRIPT EXECUTION END (EXIT CODE: $EXIT_CODE) ====="

# Power off the VM when done
poweroff -f
EOF

chmod +x $WORK_DIR/rootfs/init

# Create an ext4 filesystem
dd if=/dev/zero of=$WORK_DIR/rootfs.ext4 bs=1M count=50
mkfs.ext4 $WORK_DIR/rootfs.ext4
mkdir -p /tmp/rootfs_mount
mount $WORK_DIR/rootfs.ext4 /tmp/rootfs_mount
cp -r $WORK_DIR/rootfs/* /tmp/rootfs_mount/
umount /tmp/rootfs_mount

# Copy output files to the project directory
mkdir -p /Users/mac/Documents/personal/microvm/vm/images
cp $WORK_DIR/vmlinux /Users/mac/Documents/personal/microvm/vm/images/
cp $WORK_DIR/rootfs.ext4 /Users/mac/Documents/personal/microvm/vm/images/

# Clean up
rm -rf $WORK_DIR
echo "VM image build complete!"