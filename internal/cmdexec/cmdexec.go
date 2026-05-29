package cmdexec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/ui"
	"github.com/mattn/go-isatty"
)

const defaultCLICommandWaitDelay = time.Second

var (
	cliWaitMessagePrint = func(message string) {
		ui.Info("%s", message)
	}
	cliWaitMessageIsTerminal = func() bool { return isatty.IsTerminal(os.Stdout.Fd()) }
)

type CLICommandOptions struct {
	WaitMessage string
	WaitDelay   time.Duration
}

// RunShellCommand - for shell commands with pipes, variables, etc.
func RunCommand(ctx context.Context, command, workDir string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("empty command")
	}

	shell, flag := findShell()
	cmd := exec.CommandContext(ctx, shell, flag, command)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	return cmd.Run()
}

// RunShellCommandWithOutput - shell command that returns output
func RunCommandWithOutput(ctx context.Context, command, workDir string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty command")
	}

	shell, flag := findShell()
	cmd := exec.CommandContext(ctx, shell, flag, command)
	cmd.Dir = workDir
	cmd.Env = os.Environ()

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("shell command failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// RunCLICommandInDir executes a CLI command directly with streamed output and no shell parsing.
func RunCLICommandInDir(ctx context.Context, workDir, name string, args ...string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err == exec.ErrNotFound {
			return fmt.Errorf("command not found: '%s'. Is it installed and in your PATH?", name)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("command '%s' failed with exit code %d", name, exitErr.ExitCode())
		}
		return fmt.Errorf("failed to execute '%s': %w", name, err)
	}

	return nil
}

// RunCLICommand - for direct CLI tool execution (no shell interpretation)
func RunCLICommand(ctx context.Context, name string, args ...string) (string, error) {
	return RunCLICommandWithOptions(ctx, CLICommandOptions{}, name, args...)
}

// RunCLICommandWithOptions executes a CLI command directly and returns captured stdout.
func RunCLICommandWithOptions(ctx context.Context, opts CLICommandOptions, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := runCLICommand(cmd, opts)
	if err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err == exec.ErrNotFound {
			return "", fmt.Errorf("command not found: '%s'. Is it installed and in your PATH?", name)
		}
		if _, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("command '%s' failed: %s", name, stderr.String())
		}
		return "", fmt.Errorf("failed to execute '%s': %w", name, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func runCLICommand(cmd *exec.Cmd, opts CLICommandOptions) error {
	if strings.TrimSpace(opts.WaitMessage) == "" || !cliWaitMessageIsTerminal() {
		return cmd.Run()
	}

	delay := opts.WaitDelay
	if delay <= 0 {
		delay = defaultCLICommandWaitDelay
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		cliWaitMessagePrint(opts.WaitMessage)
		return <-done
	}
}

func findShell() (string, string) {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell, "-c"
	}

	if bashPath, err := exec.LookPath("bash"); err == nil {
		return bashPath, "-c"
	}

	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		return comspec, "/C"
	}

	if pwsh, err := exec.LookPath("powershell"); err == nil {
		return pwsh, "-Command"
	}

	if cmd, err := exec.LookPath("cmd"); err == nil {
		return cmd, "/C"
	}

	return "sh", "-c"
}
