# src/busybox/Makefile — build busybox linked against the host glibc
#
# Inputs:
#   BUSYBOX_SRC  path to extracted busybox source
#   CC           host C compiler (glibc)
#   STAGEDIR     install root (the OS rootfs being assembled)
#   ARCH

THISDIR      := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
# config.full is a complete, validated .config generated from busybox 1.36.1
# defconfig + our settings.  No kconfig tools required in CI.
BB_CONFIG    := $(THISDIR)/config.full

CC ?= gcc

.PHONY: all

all: $(STAGEDIR)/bin/busybox

$(STAGEDIR)/bin/busybox: $(BUSYBOX_SRC)/Makefile $(BB_CONFIG)
	@echo "[busybox] installing config"
	@cp $(BB_CONFIG) $(BUSYBOX_SRC)/.config
	@echo "[busybox] building (dynamic, glibc)"
	@$(MAKE) -C $(BUSYBOX_SRC) \
	    CC="$(CC)" \
	    EXTRA_CFLAGS="-Os -pipe" \
	    -j$(JOBS)
	@echo "[busybox] installing applets"
	@$(MAKE) -C $(BUSYBOX_SRC) \
	    CC="$(CC)" \
	    CONFIG_PREFIX=$(STAGEDIR) \
	    install
	@echo "[busybox] done"
