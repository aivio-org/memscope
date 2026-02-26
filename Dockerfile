# ─── Stage 1: Build ────────────────────────────────────────────────────────────
# Uses a full Debian image so we have libc, kernel headers, and clang available.
FROM golang:1.21-bookworm AS builder

ARG VERSION=0.2.0

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    clang \
    llvm \
    libelf-dev \
    linux-headers-generic \
    libpcap-dev \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache Go module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy the full source tree
COPY . .

# Build the binary with CGO enabled
# The pre-compiled .o files are embedded via go:embed, so clang is only needed
# if you want to regenerate them from the .c sources (run go generate first).
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/memscope \
    ./cmd/memscope

# ─── Stage 2: Minimal runtime ─────────────────────────────────────────────────
# distroless/static has no shell, no package manager — just libc + certs.
# The memscope binary is fully statically linked against libc (via CGO),
# so it runs fine here.
FROM gcr.io/distroless/base-debian12:nonroot AS runtime

LABEL org.opencontainers.image.title="memscope"
LABEL org.opencontainers.image.description="Real-time eBPF memory profiler for Go and Rust"
LABEL org.opencontainers.image.source="https://github.com/mbergo/memscope"
LABEL org.opencontainers.image.licenses="GPL-2.0"

COPY --from=builder /out/memscope /usr/bin/memscope

# MemScope requires CAP_BPF, CAP_PERFMON, CAP_SYS_PTRACE at runtime.
# These must be granted by the container runtime (--cap-add or --privileged).
# See README.md §Capabilities & Security.

ENTRYPOINT ["/usr/bin/memscope"]
CMD ["--help"]
