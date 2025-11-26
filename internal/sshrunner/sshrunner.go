package sshrunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
)

type Config struct {
	User     string
	Host     string
	Port     int
	Identity string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func RunStreaming(
	ctx context.Context,
	cfg Config,
	remoteCommand string,
	stdout, stderr io.Writer,
) (Result, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return Result{}, fmt.Errorf("ssh binary not found: please install OpenSSH client")
	}

	args := buildSSHArgs(cfg, remoteCommand)
	cmd := exec.CommandContext(ctx, "ssh", args...)

	cmd.Stdin = os.Stdin

	var stdoutBuf, stderrBuf bytes.Buffer

	if stdout != nil {
		cmd.Stdout = io.MultiWriter(stdout, &stdoutBuf)
	} else {
		cmd.Stdout = &stdoutBuf
	}

	if stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}

	err := cmd.Run()

	result := Result{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, fmt.Errorf("ssh command failed (exit %d): %s", result.ExitCode, stderrBuf.String())
		}
		return result, fmt.Errorf("ssh command failed: %w", err)
	}

	return result, nil
}

func Run(ctx context.Context, cfg Config, remoteCommand string) (Result, error) {
	return RunStreaming(ctx, cfg, remoteCommand, nil, nil)
}

func buildSSHArgs(cfg Config, remoteCommand string) []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=no",
	}

	if cfg.Port != 0 && cfg.Port != 22 {
		args = append(args, "-p", strconv.Itoa(cfg.Port))
	}

	if cfg.Identity != "" {
		args = append(args, "-i", cfg.Identity)
	}

	user := cfg.User
	if user == "" {
		user = "root"
	}

	args = append(args, user+"@"+cfg.Host, remoteCommand)

	return args
}
