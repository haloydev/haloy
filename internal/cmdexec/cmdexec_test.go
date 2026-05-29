package cmdexec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunCLICommandInDir_Succeeds(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	err := RunCLICommandInDir(
		context.Background(),
		"",
		helperProcessPath(t),
		"-test.run=TestHelperProcess",
		"--",
		"success",
	)
	if err != nil {
		t.Fatalf("RunCLICommandInDir() unexpected error: %v", err)
	}
}

func TestRunCLICommandInDir_UsesWorkDir(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	workDir := t.TempDir()
	err := RunCLICommandInDir(
		context.Background(),
		workDir,
		helperProcessPath(t),
		"-test.run=TestHelperProcess",
		"--",
		"check-workdir",
		workDir,
	)
	if err != nil {
		t.Fatalf("RunCLICommandInDir() unexpected error: %v", err)
	}
}

func TestRunCLICommandInDir_ReportsExitCode(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	err := RunCLICommandInDir(
		context.Background(),
		"",
		helperProcessPath(t),
		"-test.run=TestHelperProcess",
		"--",
		"exit-7",
	)
	if err == nil {
		t.Fatal("RunCLICommandInDir() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed with exit code 7") {
		t.Fatalf("RunCLICommandInDir() error = %q, want exit code detail", err.Error())
	}
}

func TestRunCLICommandInDir_ReportsMissingCommand(t *testing.T) {
	err := RunCLICommandInDir(context.Background(), "", "haloy-command-that-does-not-exist-12345")
	if err == nil {
		t.Fatal("RunCLICommandInDir() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "command not found") {
		t.Fatalf("RunCLICommandInDir() error = %q, want missing command detail", err.Error())
	}
}

func TestRunCLICommandWithOptions_FastCommandDoesNotPrintWaitMessage(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	waitOutput := configureCLIWaitMessageTestDoubles(t, true)

	output, err := RunCLICommandWithOptions(
		context.Background(),
		CLICommandOptions{WaitMessage: "waiting", WaitDelay: 100 * time.Millisecond},
		helperProcessPath(t),
		"-test.run=TestHelperProcess",
		"--",
		"stdout",
		"done",
	)
	if err != nil {
		t.Fatalf("RunCLICommandWithOptions() unexpected error: %v", err)
	}
	if output != "done" {
		t.Fatalf("RunCLICommandWithOptions() output = %q, want done", output)
	}
	if waitOutput.String() != "" {
		t.Fatalf("RunCLICommandWithOptions() wait output = %q, want no wait message", waitOutput.String())
	}
}

func TestRunCLICommandWithOptions_SlowCommandPrintsWaitMessageOnce(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	waitOutput := configureCLIWaitMessageTestDoubles(t, true)

	output, err := RunCLICommandWithOptions(
		context.Background(),
		CLICommandOptions{WaitMessage: "waiting", WaitDelay: 10 * time.Millisecond},
		helperProcessPath(t),
		"-test.run=TestHelperProcess",
		"--",
		"sleep-stdout",
		"50ms",
		"done",
	)
	if err != nil {
		t.Fatalf("RunCLICommandWithOptions() unexpected error: %v", err)
	}
	if output != "done" {
		t.Fatalf("RunCLICommandWithOptions() output = %q, want done", output)
	}
	if got, want := waitOutput.String(), "waiting\n"; got != want {
		t.Fatalf("RunCLICommandWithOptions() wait output = %q, want %q", got, want)
	}
}

func TestRunCLICommandWithOptions_NonTerminalDoesNotPrintWaitMessage(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	waitOutput := configureCLIWaitMessageTestDoubles(t, false)

	output, err := RunCLICommandWithOptions(
		context.Background(),
		CLICommandOptions{WaitMessage: "waiting", WaitDelay: 10 * time.Millisecond},
		helperProcessPath(t),
		"-test.run=TestHelperProcess",
		"--",
		"sleep-stdout",
		"50ms",
		"done",
	)
	if err != nil {
		t.Fatalf("RunCLICommandWithOptions() unexpected error: %v", err)
	}
	if output != "done" {
		t.Fatalf("RunCLICommandWithOptions() output = %q, want done", output)
	}
	if waitOutput.String() != "" {
		t.Fatalf("RunCLICommandWithOptions() wait output = %q, want no wait message", waitOutput.String())
	}
}

func TestRunCLICommand_ReportsExitStderr(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	_, err := RunCLICommand(
		context.Background(),
		helperProcessPath(t),
		"-test.run=TestHelperProcess",
		"--",
		"stderr-exit-7",
		"boom",
	)
	if err == nil {
		t.Fatal("RunCLICommand() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "command '") || !strings.Contains(err.Error(), "failed: boom") {
		t.Fatalf("RunCLICommand() error = %q, want stderr detail", err.Error())
	}
}

func configureCLIWaitMessageTestDoubles(t *testing.T, terminal bool) *bytes.Buffer {
	t.Helper()

	originalPrint := cliWaitMessagePrint
	originalIsTerminal := cliWaitMessageIsTerminal

	var output bytes.Buffer
	cliWaitMessagePrint = func(message string) {
		fmt.Fprintln(&output, message)
	}
	cliWaitMessageIsTerminal = func() bool { return terminal }

	t.Cleanup(func() {
		cliWaitMessagePrint = originalPrint
		cliWaitMessageIsTerminal = originalIsTerminal
	})

	return &output
}

func helperProcessPath(t *testing.T) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() failed: %v", err)
	}
	return exe
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	separator := indexOfArg(args, "--")
	if separator == -1 || separator+1 >= len(args) {
		fmt.Fprintln(os.Stderr, "missing helper process mode")
		os.Exit(2)
	}

	mode := args[separator+1]
	switch mode {
	case "success":
		os.Exit(0)
	case "stdout":
		if separator+2 >= len(args) {
			fmt.Fprintln(os.Stderr, "missing stdout value")
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, args[separator+2])
		os.Exit(0)
	case "sleep-stdout":
		if separator+3 >= len(args) {
			fmt.Fprintln(os.Stderr, "missing sleep duration or stdout value")
			os.Exit(2)
		}
		duration, err := time.ParseDuration(args[separator+2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse duration: %v\n", err)
			os.Exit(2)
		}
		time.Sleep(duration)
		fmt.Fprintln(os.Stdout, args[separator+3])
		os.Exit(0)
	case "check-workdir":
		if separator+2 >= len(args) {
			fmt.Fprintln(os.Stderr, "missing expected workdir")
			os.Exit(2)
		}

		got, err := filepath.Abs(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolve cwd: %v\n", err)
			os.Exit(2)
		}
		want, err := filepath.Abs(args[separator+2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "resolve expected cwd: %v\n", err)
			os.Exit(2)
		}
		gotInfo, err := os.Stat(got)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stat cwd: %v\n", err)
			os.Exit(2)
		}
		wantInfo, err := os.Stat(want)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stat expected cwd: %v\n", err)
			os.Exit(2)
		}
		if !os.SameFile(gotInfo, wantInfo) {
			fmt.Fprintf(os.Stderr, "cwd = %q, want %q\n", got, want)
			os.Exit(3)
		}
		os.Exit(0)
	case "exit-7":
		os.Exit(7)
	case "stderr-exit-7":
		if separator+2 >= len(args) {
			fmt.Fprintln(os.Stderr, "missing stderr value")
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, args[separator+2])
		os.Exit(7)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(2)
	}
}

func indexOfArg(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}
