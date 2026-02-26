package events

import (
	"fmt"
	"time"
)

// EventKind identifies the type of memory event.
type EventKind int

const (
	KindAlloc      EventKind = iota // heap allocation
	KindDealloc                     // heap deallocation
	KindGCPause                     // GC stop-the-world begin
	KindGCResume                    // GC stop-the-world end
	KindStackGrow                   // goroutine stack growth
	KindSyscall                     // syscall entry/exit (see SyscallEvent)
)

func (k EventKind) String() string {
	switch k {
	case KindAlloc:
		return "alloc"
	case KindDealloc:
		return "dealloc"
	case KindGCPause:
		return "gc_pause"
	case KindGCResume:
		return "gc_resume"
	case KindStackGrow:
		return "stack_grow"
	case KindSyscall:
		return "syscall"
	default:
		return "unknown"
	}
}

// SyscallEvent captures one completed syscall pair (entry + exit).
// Nr is the raw syscall number; Name is resolved in user-space via
// the syscall name table.
type SyscallEvent struct {
	Nr         uint32
	Name       string    // resolved from Nr by SyscallName()
	Pid        uint32
	Tid        uint32
	Ret        int64
	DurationNs uint64
	Timestamp  time.Time
}

// syscallNames maps Linux amd64 syscall numbers to names.
var syscallNames = map[uint32]string{
	0: "read", 1: "write", 2: "open", 3: "close", 4: "stat", 5: "fstat",
	6: "lstat", 7: "poll", 8: "lseek", 9: "mmap", 10: "mprotect",
	11: "munmap", 12: "brk", 13: "rt_sigaction", 14: "rt_sigprocmask",
	17: "pread64", 18: "pwrite64", 19: "readv", 20: "writev",
	21: "access", 22: "pipe", 23: "select", 24: "sched_yield",
	28: "madvise", 32: "dup", 33: "dup2", 39: "getpid",
	41: "socket", 42: "connect", 43: "accept", 44: "sendto",
	45: "recvfrom", 49: "bind", 50: "listen", 54: "setsockopt",
	55: "getsockopt", 56: "clone", 57: "fork", 58: "vfork",
	59: "execve", 60: "exit", 61: "wait4", 62: "kill",
	63: "uname", 72: "fcntl", 73: "flock", 74: "fsync",
	79: "getcwd", 80: "chdir", 83: "mkdir", 84: "rmdir",
	85: "creat", 86: "link", 87: "unlink", 88: "symlink",
	89: "readlink", 90: "chmod", 92: "chown", 95: "umask",
	96: "gettimeofday", 97: "getrlimit", 99: "sysinfo",
	102: "getuid", 104: "getgid", 105: "setuid", 106: "setgid",
	107: "geteuid", 108: "getegid", 110: "getppid",
	111: "getpgrp", 112: "setsid", 128: "rt_sigsuspend",
	131: "sigaltstack", 158: "arch_prctl", 186: "gettid",
	202: "futex", 217: "getdents64", 228: "clock_gettime",
	229: "clock_getres", 230: "clock_nanosleep", 231: "exit_group",
	233: "epoll_ctl", 247: "waitid", 257: "openat",
	261: "futimesat", 262: "newfstatat", 263: "unlinkat",
	265: "linkat", 266: "symlinkat", 267: "readlinkat",
	269: "faccessat", 270: "pselect6", 271: "ppoll",
	281: "epoll_pwait", 282: "signalfd", 283: "timerfd_create",
	284: "eventfd", 285: "fallocate", 288: "accept4",
	290: "epoll_create1", 291: "dup3", 292: "pipe2",
	293: "inotify_init1", 302: "prlimit64", 316: "renameat2",
	318: "getrandom", 319: "memfd_create", 334: "rseq",
}

// SyscallName returns the human-readable name for the given syscall number,
// or "sys_NNN" if the number is not in the table.
func SyscallName(nr uint32) string {
	if name, ok := syscallNames[nr]; ok {
		return name
	}
	return fmt.Sprintf("sys_%d", nr)
}

// MemEvent is the normalized event produced by the pipeline.
type MemEvent struct {
	Kind        EventKind
	Addr        uint64
	Size        uint64
	TypeName    string
	StackID     uint32
	GoroutineID uint64
	Timestamp   time.Time
	SourceFile  string
	SourceLine  uint32
}

// RegionKind classifies a /proc/<pid>/maps entry.
type RegionKind int

const (
	RegionHeap  RegionKind = iota
	RegionStack            // [stack]
	RegionBSS              // anonymous rw-p
	RegionText             // r-xp
	RegionMmap             // named mmap
	RegionGuard            // ---p (guard page)
	RegionVDSO             // [vdso]
	RegionVvar             // [vvar]
	RegionOther
)

func (r RegionKind) String() string {
	switch r {
	case RegionHeap:
		return "heap"
	case RegionStack:
		return "stack"
	case RegionBSS:
		return "bss"
	case RegionText:
		return "text"
	case RegionMmap:
		return "mmap"
	case RegionGuard:
		return "guard"
	case RegionVDSO:
		return "vdso"
	case RegionVvar:
		return "vvar"
	default:
		return "other"
	}
}

// MemRegion represents a single entry from /proc/<pid>/maps.
type MemRegion struct {
	Start  uint64
	End    uint64
	Perms  string
	Offset uint64
	Dev    string
	Inode  uint64
	Name   string
	Kind   RegionKind
}

// Size returns the byte size of the region.
func (r MemRegion) Size() uint64 {
	return r.End - r.Start
}
