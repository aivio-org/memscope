package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/mbergo/memscope/internal/agent"
	"github.com/mbergo/memscope/internal/theme"
	"github.com/mbergo/memscope/internal/tui"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "memscope",
	Short: "Real-time memory profiler for Go and Rust processes",
	Long: `MemScope attaches to live Go or Rust processes and visualizes
memory allocations, syscalls, and stack/heap layouts in real time.

It uses eBPF uprobes (no code changes to the target process) and requires
CAP_BPF, CAP_PERFMON, and CAP_SYS_PTRACE capabilities.

Quick start:
  memscope attach --pid $(pgrep myservice)
  memscope run -- ./myservice
`,
}

// --------------------------------------------------------------------------
// attach command
// --------------------------------------------------------------------------

var (
	attachPID   int
	attachTheme string
)

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to a running process by PID",
	Example: `  # Attach to a running Go service
  memscope attach --pid 12345`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if attachPID == 0 {
			return fmt.Errorf("--pid is required (use 'memscope run -- <binary>' to start a new process)")
		}
		return runTUI(attachPID, attachTheme)
	},
}

// --------------------------------------------------------------------------
// run command
// --------------------------------------------------------------------------

var runTheme string

var runCmd = &cobra.Command{
	Use:   "run -- <binary> [args...]",
	Short: "Start a binary and immediately attach to it",
	Example: `  memscope run -- ./myservice --config prod.yaml`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Spawn the target binary
		child := exec.Command(args[0], args[1:]...)
		child.Stdin = os.Stdin
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr

		if err := child.Start(); err != nil {
			return fmt.Errorf("start %q: %w", args[0], err)
		}
		pid := child.Process.Pid
		fmt.Fprintf(os.Stderr, "started %s (pid %d)\n", args[0], pid)

		// Attach TUI; when the TUI exits, send SIGTERM then kill the child
		err := runTUI(pid, runTheme)
		_ = child.Process.Kill()
		_ = child.Wait()
		return err
	},
}

// --------------------------------------------------------------------------
// version command
// --------------------------------------------------------------------------

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("memscope v0.2.0-phase2")
	},
}

func init() {
	// attach flags
	attachCmd.Flags().IntVar(&attachPID, "pid", 0, "Target process PID (required)")
	attachCmd.Flags().StringVar(&attachTheme, "theme", "", "Path to theme.toml (default: Dracula)")

	// run flags
	runCmd.Flags().StringVar(&runTheme, "theme", "", "Path to theme.toml (default: Dracula)")

	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(versionCmd)
}

// runTUI builds and runs the bubbletea program.
func runTUI(pid int, themePath string) error {
	// Load theme
	var t theme.Theme
	var err error
	if themePath != "" {
		t, err = theme.Load(themePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not load theme %q: %v; using Dracula\n", themePath, err)
			t = theme.Dracula()
		}
	} else {
		t = theme.Dracula()
	}

	// Build real eBPF probe
	p, err := agent.New(pid)
	if err != nil {
		return fmt.Errorf("attach probe: %w", err)
	}

	// Build TUI model
	m := tui.NewModel(p, pid, t)
	// Close always runs — even if prog.Run() returns an error — to release eBPF
	// objects, cancel the pipeline goroutine, and stop the probe cleanly.
	defer m.Close()

	// Run the bubbletea program with alternate screen
	prog := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	finalModel, runErr := prog.Run()
	// Update m to the final state so the deferred Close() targets the correct
	// cancel function (set by probeStartedMsg during Init).
	if fm, ok := finalModel.(tui.Model); ok {
		m = fm
	}
	return runErr
}
