# src/installer/Makefile — build the blueberry-install binary.
THISDIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
CC      ?= cc
CFLAGS  ?= -Os -pipe -fstack-protector-strong
CFLAGS  += -Wall -Wextra -std=gnu11
OBJDIR  ?= $(THISDIR).obj
OUT     ?= $(OBJDIR)/blueberry-install

.PHONY: all clean
all: $(OUT)
$(OUT): $(THISDIR)blueberry-install.c | $(OBJDIR)
	$(CC) $(CFLAGS) -o $@ $<
$(OBJDIR):
	@mkdir -p $@
clean:
	rm -rf $(OBJDIR)
