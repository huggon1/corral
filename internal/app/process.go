package app

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func startProcess(cwd string, args []string, env map[string]string, logPath string) (int, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("empty command")
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Env = append([]string{}, os.Environ()...)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	configureDetached(cmd)
	if err := cmd.Start(); err != nil {
		if errorsIsExecutableNotFound(err) {
			return 0, fmt.Errorf("executable %q not found", args[0])
		}
		return 0, fmt.Errorf("start %s: %w", strings.Join(args, " "), err)
	}
	pid := cmd.Process.Pid
	// Reap the process while localhost-manager is alive. Because it owns a separate session,
	// it is re-parented and keeps running if this localhost-manager invocation exits first.
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

func errorsIsExecutableNotFound(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "executable file not found") || strings.Contains(strings.ToLower(err.Error()), "file does not exist")
}

func OpenBrowser(url string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{url}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		command, args = "xdg-open", []string{url}
	}
	return exec.Command(command, args...).Start()
}
