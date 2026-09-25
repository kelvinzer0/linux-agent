package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ExecutionResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	IsError    bool   `json:"is_error"`
	ErrorMsg   string `json:"error_msg,omitempty"`
}

func (r *ExecutionResult) Formatted() string {
	var sb strings.Builder
	if r.IsError && r.ErrorMsg != "" {
		sb.WriteString(fmt.Sprintf("❌ Error: %s\n", r.ErrorMsg))
	} else if r.ExitCode != 0 {
		sb.WriteString(fmt.Sprintf("⚠️ Process exited with code %d\n", r.ExitCode))
	} else {
		sb.WriteString("✅ Success\n")
	}

	sb.WriteString(fmt.Sprintf("⏱️ Duration: %dms\n", r.DurationMs))

	if len(r.Stdout) > 0 {
		sb.WriteString("\n--- STDOUT ---\n")
		sb.WriteString(r.Stdout)
		if !strings.HasSuffix(r.Stdout, "\n") {
			sb.WriteString("\n")
		}
	}

	if len(r.Stderr) > 0 {
		sb.WriteString("\n--- STDERR ---\n")
		sb.WriteString(r.Stderr)
		if !strings.HasSuffix(r.Stderr, "\n") {
			sb.WriteString("\n")
		}
	}

	if len(r.Stdout) == 0 && len(r.Stderr) == 0 && !r.IsError {
		sb.WriteString("\n(No output produced)\n")
	}

	return sb.String()
}

// ExecuteCode runs code in the specified language in the Linux environment.
func ExecuteCode(ctx context.Context, code string, language string, timeoutSec int, cwd string, extraArgs []string) (*ExecutionResult, error) {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > 600 {
		timeoutSec = 600 // max 10 minutes
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	lang := strings.ToLower(strings.TrimSpace(language))
	start := time.Now()

	var cmd *exec.Cmd
	var cleanup func()

	switch lang {
	case "python", "python3", "py":
		tmpFile, err := os.CreateTemp("", "agent_run_*.py")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp script: %w", err)
		}
		cleanup = func() { _ = os.Remove(tmpFile.Name()) }
		if _, err := tmpFile.WriteString(code); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to write script content: %w", err)
		}
		_ = tmpFile.Close()

		pythonBin := "python3"
		if _, err := exec.LookPath("python3"); err != nil {
			pythonBin = "python"
		}
		args := append([]string{tmpFile.Name()}, extraArgs...)
		cmd = exec.CommandContext(execCtx, pythonBin, args...)

	case "bash":
		args := []string{"-c", code}
		if len(extraArgs) > 0 {
			args = append(args, "bash")
			args = append(args, extraArgs...)
		}
		cmd = exec.CommandContext(execCtx, "bash", args...)

	case "sh":
		args := []string{"-c", code}
		if len(extraArgs) > 0 {
			args = append(args, "sh")
			args = append(args, extraArgs...)
		}
		cmd = exec.CommandContext(execCtx, "sh", args...)

	case "node", "javascript", "js":
		tmpFile, err := os.CreateTemp("", "agent_run_*.js")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp script: %w", err)
		}
		cleanup = func() { _ = os.Remove(tmpFile.Name()) }
		if _, err := tmpFile.WriteString(code); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to write script content: %w", err)
		}
		_ = tmpFile.Close()

		args := append([]string{tmpFile.Name()}, extraArgs...)
		cmd = exec.CommandContext(execCtx, "node", args...)

	case "go", "golang":
		// Handle go run
		tmpDir, err := os.MkdirTemp("", "agent_go_*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %w", err)
		}
		cleanup = func() { _ = os.RemoveAll(tmpDir) }

		src := code
		if !strings.Contains(src, "package ") {
			src = "package main\n\nimport \"fmt\"\n\nfunc main() {\n" + src + "\n}\n"
		}

		filePath := filepath.Join(tmpDir, "main.go")
		if err := os.WriteFile(filePath, []byte(src), 0644); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to write go source: %w", err)
		}

		args := append([]string{"run", filePath}, extraArgs...)
		cmd = exec.CommandContext(execCtx, "go", args...)

	default:
		return nil, fmt.Errorf("unsupported language: %q (supported: python, bash, sh, javascript, go)", language)
	}

	if cleanup != nil {
		defer cleanup()
	}

	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	duration := time.Since(start).Milliseconds()

	result := &ExecutionResult{
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		DurationMs: duration,
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.IsError = true
			result.ErrorMsg = fmt.Sprintf("Execution timed out after %d seconds", timeoutSec)
			result.ExitCode = 124
			return result, nil
		}

		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			if result.ExitCode != 0 {
				// Normal program exit with error code
				return result, nil
			}
		}

		result.IsError = true
		result.ErrorMsg = err.Error()
		result.ExitCode = 1
		return result, nil
	}

	result.ExitCode = 0
	return result, nil
}
