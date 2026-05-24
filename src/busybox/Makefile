# src/busybox/Makefile — build busybox linked against musl
#
# Inputs:
#   BUSYBOX_SRC  path to extracted busybox source
#   SYSROOT      musl sysroot (for CC wrapper)
#   STAGEDIR     install root (the OS rootfs being assembled)
#   ARCH

THISDIR  := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
BB_CONFIG := $(THISDIR)/config

# musl-gcc wrapper puts -I/-L for musl automatically
MUSL_GCC := $(SYSROOT)/bin/musl-gcc

.PHONY: all

all: $(STAGEDIR)/bin/busybox

$(STAGEDIR)/bin/busybox: $(BUSYBOX_SRC)/Makefile
	@echo "[busybox] generating config"
	@# defconfig gives a complete .config (all Kconfig defaults); we then
	@# patch in our fragment line-by-line.  No interactive prompts.
	@$(MAKE) -C $(BUSYBOX_SRC) CC="$(MUSL_GCC)" defconfig
	@while IFS= read -r line; do \
	    case "$$line" in \
	        CONFIG_*=*) \
	            key=$${line%%=*}; \
	            if grep -q "^$$key=" $(BUSYBOX_SRC)/.config; then \
	                sed -i "s|^$$key=.*|$$line|" $(BUSYBOX_SRC)/.config; \
	            elif grep -q "^# $$key is not set" $(BUSYBOX_SRC)/.config; then \
	                sed -i "s|^# $$key is not set|$$line|" $(BUSYBOX_SRC)/.config; \
	            fi ;; \
	    esac; \
	done < $(BB_CONFIG)
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
