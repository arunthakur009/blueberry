# src/dropbear/Makefile — build Dropbear SSH (server + client) against musl
#
# Inputs:
#   DROPBEAR_SRC  path to extracted dropbear source
#   SYSROOT       musl sysroot (for the musl-gcc wrapper)
#   STAGEDIR      install root (the OS rootfs being assembled)
#
# Builds a single static multi-call binary (dropbearmulti) — like busybox — and
# installs it with applet symlinks: dropbear, dropbearkey, dbclient, scp.

MUSL_GCC := $(SYSROOT)/bin/musl-gcc
PROGRAMS := dropbear dropbearkey dbclient scp

.PHONY: all

all: $(STAGEDIR)/usr/sbin/dropbear

$(STAGEDIR)/usr/sbin/dropbear: $(DROPBEAR_SRC)/dropbearmulti
	@echo "[dropbear] installing"
	@install -Dm755 $(DROPBEAR_SRC)/dropbearmulti $(STAGEDIR)/usr/sbin/dropbearmulti
	@ln -sf dropbearmulti $(STAGEDIR)/usr/sbin/dropbear
	@ln -sf dropbearmulti $(STAGEDIR)/usr/sbin/dropbearkey
	@ln -sf ../sbin/dropbearmulti $(STAGEDIR)/usr/bin/dbclient 2>/dev/null || \
	    { mkdir -p $(STAGEDIR)/usr/bin; ln -sf ../sbin/dropbearmulti $(STAGEDIR)/usr/bin/dbclient; }
	@ln -sf ../sbin/dropbearmulti $(STAGEDIR)/usr/bin/scp

$(DROPBEAR_SRC)/dropbearmulti:
	@echo "[dropbear] configuring"
	@cd $(DROPBEAR_SRC) && \
	    CC="$(MUSL_GCC)" ./configure \
	        --host=$(shell uname -m)-linux-musl \
	        --disable-zlib \
	        --disable-lastlog --disable-utmp --disable-utmpx \
	        --disable-wtmp --disable-wtmpx \
	        --enable-static >/dev/null
	@echo "[dropbear] building multi-binary"
	@$(MAKE) -C $(DROPBEAR_SRC) \
	    PROGRAMS="$(PROGRAMS)" MULTI=1 STATIC=1 \
	    SCPPROGRESS=1
