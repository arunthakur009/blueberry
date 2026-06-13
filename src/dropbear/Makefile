# src/dropbear/Makefile — build Dropbear SSH (server + client) against glibc
#
# Inputs:
#   DROPBEAR_SRC  path to extracted dropbear source
#   CC            host C compiler (glibc)
#   STAGEDIR      install root (the OS rootfs being assembled)
#
# Builds a single multi-call binary (dropbearmulti) — like busybox — and
# installs it with applet symlinks: dropbear, dropbearkey, dbclient, scp.

CC ?= gcc
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
	    CC="$(CC)" ./configure \
	        --disable-zlib \
	        --disable-lastlog --disable-utmp --disable-utmpx \
	        --disable-wtmp --disable-wtmpx >/dev/null
	@echo "[dropbear] building multi-binary (dynamic, glibc)"
	@$(MAKE) -C $(DROPBEAR_SRC) \
	    PROGRAMS="$(PROGRAMS)" MULTI=1 \
	    SCPPROGRESS=1
