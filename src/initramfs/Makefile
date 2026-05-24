# src/initramfs/Makefile — build the initramfs CPIO image
#
# Produces $(BOOTDIR)/initramfs.cpio.zst
#
# Inputs:
#   STAGEDIR   the assembled OS rootfs (provides busybox + runit)
#   BOOTDIR    where to write initramfs.cpio.zst
#   OBJDIR     scratch space

THISDIR    := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
INITRAMDIR := $(OBJDIR)/initramfs

.PHONY: all

all: $(BOOTDIR)/initramfs.cpio.zst

$(BOOTDIR)/initramfs.cpio.zst: $(THISDIR)/init
	@echo "[initramfs] assembling"
	@rm -rf $(INITRAMDIR)
	@mkdir -p $(INITRAMDIR)/bin $(INITRAMDIR)/sbin $(INITRAMDIR)/lib \
	          $(INITRAMDIR)/lib64 $(INITRAMDIR)/proc $(INITRAMDIR)/sys \
	          $(INITRAMDIR)/dev $(INITRAMDIR)/mnt/root $(INITRAMDIR)/run \
	          $(INITRAMDIR)/tmp

	@# Copy busybox (static binary — provides everything we need)
	@if [ -f $(STAGEDIR)/bin/busybox ]; then \
	    cp $(STAGEDIR)/bin/busybox $(INITRAMDIR)/bin/busybox; \
	else \
	    echo "WARNING: busybox not found in $(STAGEDIR)/bin/busybox"; \
	fi

	@# Create busybox applet symlinks needed in initramfs
	@for app in sh ash mount umount switch_root mdev blkid sleep \
	            modprobe insmod lsmod mkdir cp mv rm ln ls cat \
	            find grep sed awk echo printf head tail; do \
	    ln -sf /bin/busybox $(INITRAMDIR)/bin/$$app 2>/dev/null || true; \
	done
	@for app in cryptsetup vgchange; do \
	    [ -f $(STAGEDIR)/sbin/$$app ] && \
	        cp $(STAGEDIR)/sbin/$$app $(INITRAMDIR)/sbin/ 2>/dev/null || true; \
	done
	@mkdir -p $(INITRAMDIR)/sbin
	@for app in blkid; do \
	    [ -f $(STAGEDIR)/sbin/$$app ] && \
	        cp $(STAGEDIR)/sbin/$$app $(INITRAMDIR)/sbin/ 2>/dev/null || true; \
	done

	@# Copy musl dynamic linker if busybox is dynamic
	@if ldd $(STAGEDIR)/bin/busybox 2>/dev/null | grep -q musl; then \
	    musl_ld=$$(ldd $(STAGEDIR)/bin/busybox 2>/dev/null | \
	              grep -o '/lib/ld-musl[^ ]*' | head -1); \
	    [ -n "$$musl_ld" ] && cp "$$musl_ld" $(INITRAMDIR)/lib/; \
	fi

	@# Install /init
	@cp $(THISDIR)/init $(INITRAMDIR)/init
	@chmod 755 $(INITRAMDIR)/init

	@# Create device nodes
	@mknod -m 600 $(INITRAMDIR)/dev/console  c 5 1 2>/dev/null || true
	@mknod -m 666 $(INITRAMDIR)/dev/null     c 1 3 2>/dev/null || true
	@mknod -m 666 $(INITRAMDIR)/dev/zero     c 1 5 2>/dev/null || true
	@mknod -m 444 $(INITRAMDIR)/dev/urandom  c 1 9 2>/dev/null || true
	@mknod -m 444 $(INITRAMDIR)/dev/random   c 1 8 2>/dev/null || true
	@mknod -m 666 $(INITRAMDIR)/dev/tty      c 5 0 2>/dev/null || true

	@# Pack as CPIO
	@echo "[initramfs] packing"
	@cd $(INITRAMDIR) && \
	    find . | sort | \
	    cpio --create --format=newc --quiet 2>/dev/null | \
	    zstd -19 -q > $(BOOTDIR)/initramfs.cpio.zst
	@echo "[initramfs] written to $(BOOTDIR)/initramfs.cpio.zst"
	@ls -lh $(BOOTDIR)/initramfs.cpio.zst
