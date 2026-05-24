# src/busybox/Makefile — build busybox linked against musl
#
# Inputs:
#   BUSYBOX_SRC  path to extracted busybox source
#   SYSROOT      musl sysroot (for CC wrapper)
#   STAGEDIR     install root (the OS rootfs being assembled)
#   ARCH

THISDIR      := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
# config.full is a complete, validated .config generated from busybox 1.36.1
# defconfig + our settings.  No kconfig tools required in CI.
BB_CONFIG    := $(THISDIR)/config.full

# musl-gcc wrapper puts -I/-L for musl automatically
MUSL_GCC := $(SYSROOT)/bin/musl-gcc

.PHONY: all

all: $(STAGEDIR)/bin/busybox

$(STAGEDIR)/bin/busybox: $(BUSYBOX_SRC)/Makefile
	@echo "[busybox] installing config"
	@cp $(BB_CONFIG) $(BUSYBOX_SRC)/.config
	@echo "[busybox] building"
	@$(MAKE) -C $(BUSYBOX_SRC) \
	    CC="$(MUSL_GCC)" \
	    EXTRA_CFLAGS="-Os -pipe" \
	    LDFLAGS="--static" \
	    -j$(JOBS)
	@echo "[busybox] installing applets"
	@$(MAKE) -C $(BUSYBOX_SRC) \
	    CC="$(MUSL_GCC)" \
	    CONFIG_PREFIX=$(STAGEDIR) \
	    install
	@echo "[busybox] done"
