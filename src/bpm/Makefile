# src/bpm/Makefile — build the Blueberry Package Manager
#
# Inputs:
#   OUT           destination binary path
#   GO            go command (default: go)
#   GOFLAGS       extra go build flags
#   GO_LDFLAGS    linker flags
#   ARCH          target architecture (mapped to GOARCH)

GOARCH_x86_64   := amd64
GOARCH_aarch64  := arm64
GOARCH_riscv64  := riscv64
GOARCH          := $(GOARCH_$(ARCH))

ifeq ($(GOARCH),)
  GOARCH := amd64
endif

VERSION := $(shell git describe --tags --always 2>/dev/null || echo "dev")

.PHONY: all clean test

all:
	@echo "[bpm] go build (GOARCH=$(GOARCH))"
	@CGO_ENABLED=0 GOARCH=$(GOARCH) $(GO) build \
	    $(GOFLAGS) \
	    -ldflags "$(GO_LDFLAGS) -X main.version=$(VERSION)" \
	    -o $(OUT) .

test:
	@echo "[bpm] go test"
	@$(GO) test ./...

clean:
	@rm -f $(OUT)
