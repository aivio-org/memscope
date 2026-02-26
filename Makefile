# MemScope Makefile
# Usage: make [target]

BINARY      := memscope
VERSION     := 0.2.0
MODULE      := github.com/mbergo/memscope
ARCH        := $(shell dpkg --print-architecture 2>/dev/null || echo amd64)
GOOS        := linux
GOARCH      := amd64
CGO         := 1

# Output directories
BUILD_DIR   := build
DEB_DIR     := $(BUILD_DIR)/deb
DEB_PKG     := $(BUILD_DIR)/$(BINARY)_$(VERSION)_$(ARCH).deb

# Docker
IMAGE_NAME  := ghcr.io/mbergo/$(BINARY)
IMAGE_TAG   := $(VERSION)

# Install paths
PREFIX      := /usr
BINDIR      := $(PREFIX)/bin
DOCDIR      := $(PREFIX)/share/doc/$(BINARY)

# BPF source
BPF_SRC_DIR := bpf/src
BPF_GEN_DIR := internal/bpf
LIBBPF_INC  := /usr/src/linux-headers-$(shell uname -r)/tools/bpf/resolve_btfids/libbpf/include
CLANG_FLAGS := -target bpf -O2 -g -D__TARGET_ARCH_x86 \
               -I$(LIBBPF_INC) \
               -I/usr/include \
               -I/usr/include/x86_64-linux-gnu

LDFLAGS     := -ldflags "-s -w -X main.version=$(VERSION)"

# Use .ONESHELL so each recipe runs in a single shell instance
# (required for the heredoc patterns in _deb-* targets)
.ONESHELL:
.SHELLFLAGS := -eu -c

.PHONY: all build build-debug test test-race test-verbose test-cover lint vet \
        bpf-generate bpf-verify install setcap uninstall \
        deb deb-install deb-remove \
        docker-build docker-push docker-run docker-shell \
        clean clean-all help

# ─────────────────────────────────────────────────────────────────────────────
# Default
# ─────────────────────────────────────────────────────────────────────────────

all: build

# ─────────────────────────────────────────────────────────────────────────────
# Build
# ─────────────────────────────────────────────────────────────────────────────

## build: Compile the memscope binary (uses pre-compiled eBPF bytecode)
build:
	@echo "==> Building $(BINARY) v$(VERSION)"
	CGO_ENABLED=$(CGO) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build $(LDFLAGS) -o $(BINARY) ./cmd/$(BINARY)
	@echo "    Built: $(BINARY) ($$(du -sh $(BINARY) | cut -f1))"

## build-debug: Build with full debug info (no stripping)
build-debug:
	CGO_ENABLED=$(CGO) GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build -o $(BINARY)-debug ./cmd/$(BINARY)

# ─────────────────────────────────────────────────────────────────────────────
# Tests
# ─────────────────────────────────────────────────────────────────────────────

## test: Run the unit test suite
test:
	@echo "==> Running tests"
	go test ./...

## test-race: Run tests with the race detector
test-race:
	@echo "==> Running tests (race detector)"
	go test -race ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "==> Running tests (verbose)"
	go test -v ./...

## test-cover: Generate an HTML coverage report
test-cover:
	@echo "==> Generating coverage report"
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "    Report: coverage.html"

# ─────────────────────────────────────────────────────────────────────────────
# Lint / Vet
# ─────────────────────────────────────────────────────────────────────────────

## lint: Run golangci-lint
lint:
	@echo "==> Linting"
	golangci-lint run ./...

## vet: Run go vet
vet:
	go vet ./...

# ─────────────────────────────────────────────────────────────────────────────
# eBPF
# ─────────────────────────────────────────────────────────────────────────────

## bpf-generate: Recompile .c → .o and regenerate Go bindings (requires clang + bpf2go)
bpf-generate:
	@echo "==> Installing bpf2go"
	go install github.com/cilium/ebpf/cmd/bpf2go@latest
	@echo "==> Running go generate"
	cd $(BPF_GEN_DIR) && go generate gen.go
	@echo "    Generated: $$(ls $(BPF_GEN_DIR)/*.o | xargs -I{} basename {})"

## bpf-verify: Quick syntax check — compile .c without generating Go bindings
bpf-verify:
	@echo "==> Verifying BPF C sources"
	clang $(CLANG_FLAGS) -c $(BPF_SRC_DIR)/mallocgc.c -o /tmp/mallocgc_verify.o
	clang $(CLANG_FLAGS) -c $(BPF_SRC_DIR)/syscalls.c  -o /tmp/syscalls_verify.o
	@echo "    mallocgc.c: OK"
	@echo "    syscalls.c:  OK"

# ─────────────────────────────────────────────────────────────────────────────
# Install
# ─────────────────────────────────────────────────────────────────────────────

## install: Install binary to $(BINDIR) and set capabilities
install: build
	@echo "==> Installing to $(DESTDIR)$(BINDIR)"
	install -D -m 0755 $(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)
	setcap cap_bpf,cap_perfmon,cap_sys_ptrace+eip $(DESTDIR)$(BINDIR)/$(BINARY)
	@echo "    Installed and capabilities set."

## setcap: Grant eBPF capabilities to ./memscope (requires root / sudo)
setcap:
	@echo "==> Setting capabilities on $(BINARY)"
	setcap cap_bpf,cap_perfmon,cap_sys_ptrace+eip $(BINARY)
	@echo "    $$(getcap $(BINARY))"

## uninstall: Remove installed binary
uninstall:
	@echo "==> Uninstalling"
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY)

# ─────────────────────────────────────────────────────────────────────────────
# Debian package
# ─────────────────────────────────────────────────────────────────────────────

## deb: Build a .deb package  →  build/memscope_$(VERSION)_$(ARCH).deb
deb: build
	@echo "==> Building Debian package v$(VERSION)"
	rm -rf $(DEB_DIR)
	mkdir -p $(DEB_DIR)/DEBIAN
	mkdir -p $(DEB_DIR)$(BINDIR)
	mkdir -p $(DEB_DIR)$(DOCDIR)
	mkdir -p $(DEB_DIR)/lib/systemd/system
	install -m 0755 $(BINARY) $(DEB_DIR)$(BINDIR)/$(BINARY)
	printf '%s\n' \
		'Package: memscope' \
		'Version: $(VERSION)' \
		'Architecture: $(ARCH)' \
		'Maintainer: mbergo <mbergo@users.noreply.github.com>' \
		'Depends: libc6' \
		'Recommends: linux-headers-generic' \
		'Section: utils' \
		'Priority: optional' \
		'Description: Real-time eBPF memory profiler for Go and Rust processes' \
		' MemScope attaches to live processes via eBPF uprobes and visualizes' \
		' heap allocations, goroutine activity, and syscalls in a terminal UI.' \
		' No code changes to the target are required.' \
		' .' \
		' Requires Linux >= 5.8 and CAP_BPF, CAP_PERFMON, CAP_SYS_PTRACE.' \
		> $(DEB_DIR)/DEBIAN/control
	printf '#!/bin/sh\nset -e\nif command -v setcap >/dev/null 2>&1; then\n  setcap cap_bpf,cap_perfmon,cap_sys_ptrace+eip $(BINDIR)/$(BINARY)\nfi\n' \
		> $(DEB_DIR)/DEBIAN/postinst
	chmod 0755 $(DEB_DIR)/DEBIAN/postinst
	printf '#!/bin/sh\nset -e\nif command -v setcap >/dev/null 2>&1; then\n  setcap -r $(BINDIR)/$(BINARY) 2>/dev/null || true\nfi\n' \
		> $(DEB_DIR)/DEBIAN/prerm
	chmod 0755 $(DEB_DIR)/DEBIAN/prerm
	printf '%s\n' \
		'[Unit]' \
		'Description=MemScope profiler for PID %i' \
		'After=network.target' \
		'' \
		'[Service]' \
		'Type=simple' \
		'ExecStart=$(BINDIR)/$(BINARY) attach --pid %i' \
		'Restart=no' \
		'AmbientCapabilities=CAP_BPF CAP_PERFMON CAP_SYS_PTRACE' \
		'' \
		'[Install]' \
		'WantedBy=multi-user.target' \
		> $(DEB_DIR)/lib/systemd/system/memscope@.service
	printf '%s\n' \
		'memscope ($(VERSION)) unstable; urgency=medium' \
		'' \
		'  * Phase 2: Real eBPF probes, syscall tracing, goroutine graph, battery UI.' \
		'  * Removed all mock/synthetic event generators.' \
		'' \
		' -- mbergo <mbergo@users.noreply.github.com>  $(shell date -R)' \
		> $(DEB_DIR)$(DOCDIR)/changelog.Debian
	gzip -9 $(DEB_DIR)$(DOCDIR)/changelog.Debian
	printf '%s\n' \
		'Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/' \
		'Upstream-Name: memscope' \
		'Source: https://github.com/mbergo/memscope' \
		'' \
		'Files: *' \
		'Copyright: $(shell date +%Y) mbergo' \
		'License: GPL-2.0+' \
		> $(DEB_DIR)$(DOCDIR)/copyright
	fakeroot dpkg-deb --build $(DEB_DIR) $(DEB_PKG)
	@echo "    Package: $(DEB_PKG) ($$(du -sh $(DEB_PKG) | cut -f1))"

## deb-install: Build and install the .deb package
deb-install: deb
	sudo dpkg -i $(DEB_PKG)

## deb-remove: Remove the installed .deb package
deb-remove:
	sudo dpkg -r memscope

# ─────────────────────────────────────────────────────────────────────────────
# Docker
# ─────────────────────────────────────────────────────────────────────────────

## docker-build: Build the Docker image
docker-build:
	@echo "==> Building Docker image $(IMAGE_NAME):$(IMAGE_TAG)"
	docker build \
	  --build-arg VERSION=$(VERSION) \
	  --tag $(IMAGE_NAME):$(IMAGE_TAG) \
	  --tag $(IMAGE_NAME):latest \
	  .
	@echo "    Size: $$(docker image inspect $(IMAGE_NAME):$(IMAGE_TAG) --format='{{.Size}}' | numfmt --to=iec)"

## docker-push: Push the Docker image to the registry
docker-push: docker-build
	@echo "==> Pushing $(IMAGE_NAME):$(IMAGE_TAG)"
	docker push $(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(IMAGE_NAME):latest

## docker-run: Run memscope in Docker against a host PID (requires PID=<n>)
docker-run:
	@test -n "$(PID)" || (echo "Usage: make docker-run PID=<pid>"; exit 1)
	docker run --rm -it \
	  --pid=host \
	  --cap-add=BPF \
	  --cap-add=PERFMON \
	  --cap-add=SYS_PTRACE \
	  $(IMAGE_NAME):latest \
	  attach --pid $(PID)

## docker-shell: Open a shell in the builder stage (for debugging)
docker-shell:
	docker build --target builder -t $(IMAGE_NAME)-builder . -q
	docker run --rm -it --entrypoint=/bin/bash $(IMAGE_NAME)-builder

# ─────────────────────────────────────────────────────────────────────────────
# Clean
# ─────────────────────────────────────────────────────────────────────────────

## clean: Remove build artifacts
clean:
	@echo "==> Cleaning"
	rm -f $(BINARY) $(BINARY)-debug coverage.out coverage.html
	rm -rf $(BUILD_DIR)

## clean-all: Remove build artifacts and Go module cache
clean-all: clean
	go clean -cache -modcache

# ─────────────────────────────────────────────────────────────────────────────
# Help
# ─────────────────────────────────────────────────────────────────────────────

## help: Print available targets
help:
	@echo "MemScope v$(VERSION) — available targets:"
	@echo ""
	@grep -E '^## [a-z]' Makefile | sed 's/^## /  make /' | column -t -s ':'
	@echo ""
	@echo "Override variables:  VERSION=$(VERSION)  ARCH=$(ARCH)  IMAGE_NAME=$(IMAGE_NAME)"
