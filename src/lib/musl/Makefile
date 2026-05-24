# src/lib/musl/Makefile — build musl libc and create a sysroot
#
# Inputs:
#   MUSL_SRC   path to extracted musl source
#   SYSROOT    destination sysroot directory
#   ARCH       target architecture
#   CC         host C compiler
#   CFLAGS

BUILD_DIR := $(MUSL_SRC)/build-blueberry

.PHONY: all

all: $(SYSROOT)/lib/libc.so

$(SYSROOT)/lib/libc.so: $(MUSL_SRC)/Makefile
	@mkdir -p $(BUILD_DIR) $(SYSROOT)
	@echo "[musl] configuring"
	@cd $(BUILD_DIR) && $(MUSL_SRC)/configure \
	    --prefix=$(SYSROOT)/usr \
	    --libdir=$(SYSROOT)/usr/lib \
	    --syslibdir=$(SYSROOT)/lib \
	    --enable-wrapper=all \
	    ARCH=$(ARCH) \
	    CC="$(CC)" \
	    CFLAGS="$(CFLAGS)"
	@echo "[musl] building"
	@$(MAKE) -C $(BUILD_DIR) -j$(JOBS)
	@echo "[musl] installing into sysroot"
	@$(MAKE) -C $(BUILD_DIR) install
	@# Create ld symlinks expected by the dynamic linker
	@mkdir -p $(SYSROOT)/lib
	@ln -sf $(SYSROOT)/usr/lib/libc.so $(SYSROOT)/lib/libc.musl-$(ARCH).so.1 2>/dev/null || true
	@ln -sf $(SYSROOT)/usr/lib/libc.so $(SYSROOT)/lib/ld-musl-$(ARCH).so.1   2>/dev/null || true
	@# Convenience symlink: $(SYSROOT)/bin/musl-gcc → usr/bin/musl-gcc
	@mkdir -p $(SYSROOT)/bin
	@ln -sf $(SYSROOT)/usr/bin/musl-gcc $(SYSROOT)/bin/musl-gcc 2>/dev/null || true
	@echo "[musl] sysroot ready at $(SYSROOT)"
