//go:build ignore

package bpf

// Generate Go eBPF bindings from BPF C sources.
// Run from the internal/bpf/ directory:
//   go generate gen.go

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -D__TARGET_ARCH_x86 -I/usr/src/linux-headers-6.18.7-76061807-generic/tools/bpf/resolve_btfids/libbpf/include -I/usr/include -I/usr/include/x86_64-linux-gnu" -target bpfel MallocGC ../../bpf/src/mallocgc.c
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -D__TARGET_ARCH_x86 -I/usr/src/linux-headers-6.18.7-76061807-generic/tools/bpf/resolve_btfids/libbpf/include -I/usr/include -I/usr/include/x86_64-linux-gnu" -target bpfel Syscalls ../../bpf/src/syscalls.c
