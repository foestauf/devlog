package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Run is the main entry point for wrapping a child process. It ensures the
// broker is running, connects to it, spawns the child, captures stdout/stderr,
// mirrors output to the terminal, and streams log events to the broker.
// It returns the child's exit code.
func Run(ctx context.Context, service string, dataDir string, args []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("client: no command provided")
	}

	// Make sure the broker is up.
	if err := EnsureBroker(dataDir); err != nil {
		return 1, fmt.Errorf("client: %w", err)
	}

	// Connect to the broker.
	c := New(service, dataDir)
	if err := c.Connect(); err != nil {
		return 1, err
	}
	defer c.Close()

	// Build the child command.
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	// Set up pipes for stdout and stderr.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("client: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return 1, fmt.Errorf("client: stderr pipe: %w", err)
	}

	// Pass stdin through to the child.
	cmd.Stdin = os.Stdin

	// Start the child process.
	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("client: start process: %w", err)
	}

	// Register the session with the broker now that we have a PID.
	commandStr := strings.Join(args, " ")
	if _, err := c.RegisterSession(cmd.Process.Pid, commandStr); err != nil {
		// Non-fatal — we still want to run the child even if the broker is misbehaving.
		fmt.Fprintf(os.Stderr, "devlog: warning: failed to register session: %v\n", err)
	}

	// Forward signals to the child process.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			_ = cmd.Process.Signal(sig)
		}
	}()

	// Read stdout and stderr concurrently.
	doneCh := make(chan struct{}, 2)
	go func() {
		scanLines(ctx, stdoutPipe, os.Stdout, "stdout", c)
		doneCh <- struct{}{}
	}()
	go func() {
		scanLines(ctx, stderrPipe, os.Stderr, "stderr", c)
		doneCh <- struct{}{}
	}()

	// Wait for both scanners to finish before calling cmd.Wait,
	// otherwise we might close the pipes early.
	<-doneCh
	<-doneCh

	// Stop signal forwarding.
	signal.Stop(sigCh)
	close(sigCh)

	// Wait for the child to exit.
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return 1, fmt.Errorf("client: wait: %w", err)
		}
	}

	// Tell the broker the session is done.
	if err := c.EndSession(exitCode); err != nil {
		fmt.Fprintf(os.Stderr, "devlog: warning: failed to end session: %v\n", err)
	}

	return exitCode, nil
}

// scanLines reads lines from r, writes each to w (terminal passthrough),
// and sends each as a log event to the broker via the client. If the broker
// connection breaks, lines are buffered and a reconnect is triggered.
func scanLines(ctx context.Context, r io.Reader, w io.Writer, stream string, c *Client) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		// Mirror to the terminal.
		fmt.Fprintln(w, line)

		// Send to the broker (with reconnect support).
		c.SendLog(ctx, stream, line, time.Now().UnixMilli())
	}
}
