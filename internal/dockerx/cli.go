package dockerx

import (
	"fmt"
	"os/exec"

	"github.com/guilhermepantoja789/docker-tui/internal/model"
)

// CLIArgs prepends docker connection flags for the given host.
// Prefer Context when set; otherwise pass -H with the daemon address.
func CLIArgs(host model.Host, rest ...string) []string {
	args := make([]string, 0, len(rest)+2)
	switch {
	case host.Context != "" && host.Context != "default":
		args = append(args, "--context", host.Context)
	case host.Address != "":
		args = append(args, "-H", host.Address)
	}
	return append(args, rest...)
}

// ExecCommand builds `docker exec -it <id> sh` (interactive shell).
func ExecCommand(host model.Host, containerID string) *exec.Cmd {
	args := CLIArgs(host, "exec", "-it", containerID, "sh")
	return exec.Command("docker", args...)
}

// AttachCommand builds `docker attach <id>`.
func AttachCommand(host model.Host, containerID string) *exec.Cmd {
	args := CLIArgs(host, "attach", containerID)
	return exec.Command("docker", args...)
}

// FormatCLIError wraps an exec error with a hint about PATH / TTY.
func FormatCLIError(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w (requires docker CLI on PATH)", op, err)
}
