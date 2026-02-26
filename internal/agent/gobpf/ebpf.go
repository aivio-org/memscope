//go:build linux

package gobpf

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"

	"github.com/mbergo/memscope/internal/bpf"
	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/pipeline"
)

// rawAllocEventSize is sizeof(struct raw_alloc_event) from mallocgc.c: 4×u64 = 32.
const rawAllocEventSize = 32

// rawSyscallEventSize is sizeof(struct raw_syscall_event) from syscalls.c:
// timestamp_ns(8)+duration_ns(8)+ret(8)+pid(4)+tid(4)+nr(4)+pad(4) = 40.
const rawSyscallEventSize = 40

// Probe is the real eBPF-based Go memory + syscall probe.
type Probe struct {
	pid int

	mallocObjs  bpf.MallocGCObjects
	syscallObjs bpf.SyscallsObjects

	links         []link.Link
	mallocReader  *ringbuf.Reader
	syscallReader *ringbuf.Reader

	memCh chan events.MemEvent
	sysCh chan events.SyscallEvent
	done  chan struct{}
}

// Start attaches all eBPF probes and returns event channels.
func (p *Probe) Start(pid int) (<-chan events.MemEvent, <-chan events.SyscallEvent, error) {
	p.pid = pid
	p.memCh = make(chan events.MemEvent, 8192)
	p.sysCh = make(chan events.SyscallEvent, 8192)
	p.done = make(chan struct{})

	if err := p.attachMallocProbe(pid); err != nil {
		return nil, nil, fmt.Errorf("mallocgc probe: %w", err)
	}
	if err := p.attachSyscallProbe(pid); err != nil {
		p.closeLinks()
		p.mallocObjs.Close()
		return nil, nil, fmt.Errorf("syscall probe: %w", err)
	}

	// Record BPF boot reference for timestamp conversion.
	pipeline.SetBootReference(bpfKtimeNow())

	go p.readMallocLoop()
	go p.readSyscallLoop()
	return p.memCh, p.sysCh, nil
}

func (p *Probe) attachMallocProbe(pid int) error {
	if err := bpf.LoadMallocGCObjects(&p.mallocObjs, &ebpf.CollectionOptions{}); err != nil {
		return fmt.Errorf("load objects: %w", err)
	}

	exePath, err := resolveExe(pid)
	if err != nil {
		p.mallocObjs.Close()
		return err
	}

	ex, err := link.OpenExecutable(exePath)
	if err != nil {
		p.mallocObjs.Close()
		return fmt.Errorf("open executable %s: %w", exePath, err)
	}

	entryLink, err := ex.Uprobe("runtime.mallocgc", p.mallocObjs.UprobeMallocgcEntry,
		&link.UprobeOptions{PID: pid})
	if err != nil {
		p.mallocObjs.Close()
		return fmt.Errorf("attach uprobe runtime.mallocgc: %w", err)
	}
	p.links = append(p.links, entryLink)

	retLink, err := ex.Uretprobe("runtime.mallocgc", p.mallocObjs.UretprobeMallocgcExit,
		&link.UprobeOptions{PID: pid})
	if err != nil {
		p.closeLinks()
		p.mallocObjs.Close()
		return fmt.Errorf("attach uretprobe runtime.mallocgc: %w", err)
	}
	p.links = append(p.links, retLink)

	reader, err := ringbuf.NewReader(p.mallocObjs.Events)
	if err != nil {
		p.closeLinks()
		p.mallocObjs.Close()
		return fmt.Errorf("malloc ring buffer reader: %w", err)
	}
	p.mallocReader = reader
	return nil
}

func (p *Probe) attachSyscallProbe(pid int) error {
	if err := bpf.LoadSyscallsObjects(&p.syscallObjs, &ebpf.CollectionOptions{}); err != nil {
		return fmt.Errorf("load syscall objects: %w", err)
	}

	// Set target PID filter (key=0 → pid).
	key := uint32(0)
	pidU32 := uint32(pid)
	if err := p.syscallObjs.TargetPid.Put(key, pidU32); err != nil {
		p.syscallObjs.Close()
		return fmt.Errorf("set target_pid: %w", err)
	}

	enterLink, err := link.AttachRawTracepoint(link.RawTracepointOptions{
		Name:    "sys_enter",
		Program: p.syscallObjs.TpSysEnter,
	})
	if err != nil {
		p.syscallObjs.Close()
		return fmt.Errorf("attach raw_tp/sys_enter: %w", err)
	}
	p.links = append(p.links, enterLink)

	exitLink, err := link.AttachRawTracepoint(link.RawTracepointOptions{
		Name:    "sys_exit",
		Program: p.syscallObjs.TpSysExit,
	})
	if err != nil {
		p.closeLinks()
		p.syscallObjs.Close()
		return fmt.Errorf("attach raw_tp/sys_exit: %w", err)
	}
	p.links = append(p.links, exitLink)

	reader, err := ringbuf.NewReader(p.syscallObjs.SyscallEvents)
	if err != nil {
		p.closeLinks()
		p.syscallObjs.Close()
		return fmt.Errorf("syscall ring buffer reader: %w", err)
	}
	p.syscallReader = reader
	return nil
}

// Stop detaches all probes and releases all kernel objects.
func (p *Probe) Stop() error {
	p.closeLinks()
	if p.mallocReader != nil {
		p.mallocReader.Close()
	}
	if p.syscallReader != nil {
		p.syscallReader.Close()
	}
	<-p.done
	p.mallocObjs.Close()
	p.syscallObjs.Close()
	return nil
}

// Lang returns the detected target language.
func (p *Probe) Lang() string { return "go" }

func (p *Probe) readMallocLoop() {
	defer func() {
		close(p.memCh)
		// Signal done only after both loops can signal — use select to avoid double-close.
		select {
		case <-p.done:
		default:
			close(p.done)
		}
	}()
	for {
		record, err := p.mallocReader.Read()
		if err != nil {
			return
		}
		if len(record.RawSample) < rawAllocEventSize {
			continue
		}
		raw := parseRawAllocEvent(record.RawSample)
		e := pipeline.Normalize(raw, events.KindAlloc)
		p.memCh <- e
	}
}

func (p *Probe) readSyscallLoop() {
	defer close(p.sysCh)
	ref := pipeline.BootRef()
	for {
		record, err := p.syscallReader.Read()
		if err != nil {
			return
		}
		if len(record.RawSample) < rawSyscallEventSize {
			continue
		}
		e := parseRawSyscallEvent(record.RawSample, ref)
		p.sysCh <- e
	}
}

func parseRawAllocEvent(data []byte) pipeline.RawAllocEvent {
	return pipeline.RawAllocEvent{
		Addr:        binary.LittleEndian.Uint64(data[0:8]),
		Size:        binary.LittleEndian.Uint64(data[8:16]),
		GoroutineID: binary.LittleEndian.Uint64(data[16:24]),
		TimestampNs: binary.LittleEndian.Uint64(data[24:32]),
	}
}

func parseRawSyscallEvent(data []byte, ref pipeline.BootReference) events.SyscallEvent {
	timestampNs := binary.LittleEndian.Uint64(data[0:8])
	durationNs := binary.LittleEndian.Uint64(data[8:16])
	ret := int64(binary.LittleEndian.Uint64(data[16:24]))
	pid := binary.LittleEndian.Uint32(data[24:28])
	tid := binary.LittleEndian.Uint32(data[28:32])
	nr := binary.LittleEndian.Uint32(data[32:36])

	return events.SyscallEvent{
		Nr:         nr,
		Name:       events.SyscallName(nr),
		Pid:        pid,
		Tid:        tid,
		Ret:        ret,
		DurationNs: durationNs,
		Timestamp:  pipeline.BpfNsToWall(timestampNs, ref),
	}
}

func (p *Probe) closeLinks() {
	for _, l := range p.links {
		l.Close()
	}
	p.links = nil
}

func resolveExe(pid int) (string, error) {
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", fmt.Errorf("readlink /proc/%d/exe: %w", pid, err)
	}
	return filepath.Clean(path), nil
}

// bpfKtimeNow reads CLOCK_BOOTTIME — the same clock used by bpf_ktime_get_ns().
func bpfKtimeNow() uint64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return 0
	}
	return uint64(ts.Sec)*1e9 + uint64(ts.Nsec) //nolint:gosec
}
