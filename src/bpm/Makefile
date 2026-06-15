# src/bpm/Makefile — build the bpm package manager binary.
#
# Outputs $(BPM_OUT). Standalone `make` builds .obj/bpm here; the top-level build
# overrides BPM_OUT and OBJDIR to keep artefacts under the project OBJDIR.
#
# Links libc + libzstd. TLS comes from vendored BearSSL (bearssl/); tar, HTTP
# and SHA-256 are implemented in-tree.

THISDIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

CC      ?= cc
CFLAGS  ?= -Os -pipe -fstack-protector-strong
CFLAGS  += -Wall -Wextra -std=gnu11
LDFLAGS ?=
LDLIBS  := -lzstd

BEAR    := $(THISDIR)bearssl
BEARINC := -I$(BEAR)/inc -I$(BEAR)/src

OBJDIR  ?= $(THISDIR).obj
BPM_OUT ?= $(OBJDIR)/bpm

SRCS := main.c util.c sha256.c vercmp.c archive.c net.c tls.c sig.c pkginfo.c repo.c db.c install.c cmd.c
OBJS := $(addprefix $(OBJDIR)/,$(SRCS:.c=.o))

BEARSRCS := $(shell find $(BEAR)/src -name '*.c')
BEAROBJS := $(patsubst $(BEAR)/src/%.c,$(OBJDIR)/bearssl/%.o,$(BEARSRCS))

.PHONY: all clean
all: $(BPM_OUT)

$(BPM_OUT): $(OBJS) $(BEAROBJS)
	$(CC) $(CFLAGS) $(LDFLAGS) -o $@ $(OBJS) $(BEAROBJS) $(LDLIBS)

# bpm objects (tls.c pulls in the BearSSL headers)
$(OBJDIR)/%.o: $(THISDIR)%.c $(THISDIR)bpm.h | $(OBJDIR)
	$(CC) $(CFLAGS) $(BEARINC) -c -o $@ $<

# vendored BearSSL objects (warnings silenced; upstream code)
$(OBJDIR)/bearssl/%.o: $(BEAR)/src/%.c
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -w $(BEARINC) -c -o $@ $<

$(OBJDIR):
	@mkdir -p $@

clean:
	rm -rf $(OBJDIR)
