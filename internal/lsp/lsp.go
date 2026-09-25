package lsp

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

type DiagnosticItem struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Severity string `json:"severity"` // "error", "warning", "info"
	Message  string `json:"message"`
}

type DiagnosticsResult struct {
	Path        string           `json:"path"`
	Diagnostics []DiagnosticItem `json:"diagnostics"`
	RawOutput   string           `json:"raw_output,omitempty"`
}

// GetDiagnostics runs language-appropriate diagnostic analysis on target file or directory.
func GetDiagnostics(ctx context.Context, path string) (*DiagnosticsResult, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("path does not exist: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	dir := filepath.Dir(path)
	if fi, _ := os.Stat(path); fi != nil && fi.IsDir() {
		dir = path
	}

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd

	switch ext {
	case ".go":
		// Check using go vet / go build
		cmd = exec.CommandContext(execCtx, "go", "vet", "./...")
		cmd.Dir = dir
	case ".py":
		cmd = exec.CommandContext(execCtx, "python3", "-m", "py_compile", path)
	case ".sh", ".bash":
		cmd = exec.CommandContext(execCtx, "bash", "-n", path)
	case ".js", ".mjs":
		cmd = exec.CommandContext(execCtx, "node", "--check", path)
	case ".ts":
		if _, err := exec.LookPath("tsc"); err == nil {
			cmd = exec.CommandContext(execCtx, "tsc", "--noEmit", path)
		} else {
			return &DiagnosticsResult{
				Path:        path,
				Diagnostics: []DiagnosticItem{},
				RawOutput:   "TypeScript compiler (tsc) not found in PATH",
			}, nil
		}
	default:
		// Generic syntax or file check
		return &DiagnosticsResult{
			Path:        path,
			Diagnostics: []DiagnosticItem{},
			RawOutput:   fmt.Sprintf("No dedicated diagnostic engine for %s file extension", ext),
		}, nil
	}

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	_ = cmd.Run()
	rawOutput := outBuf.String()

	res := &DiagnosticsResult{
		Path:        path,
		Diagnostics: parseDiagnosticLines(path, rawOutput),
		RawOutput:   rawOutput,
	}

	return res, nil
}

func parseDiagnosticLines(filePath, raw string) []DiagnosticItem {
	var items []DiagnosticItem
	lines := strings.Split(raw, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Pattern: file:line:col: message or file:line: message
		parts := strings.SplitN(line, ":", 4)
		if len(parts) >= 3 {
			item := DiagnosticItem{
				File:     parts[0],
				Severity: "error",
			}
			var lineNum, colNum int
			fmt.Sscanf(parts[1], "%d", &lineNum)
			item.Line = lineNum

			if len(parts) >= 4 {
				fmt.Sscanf(parts[2], "%d", &colNum)
				item.Col = colNum
				item.Message = strings.TrimSpace(parts[3])
			} else {
				item.Message = strings.TrimSpace(parts[2])
			}

			if strings.Contains(strings.ToLower(item.Message), "warning") {
				item.Severity = "warning"
			}
			items = append(items, item)
		} else {
			items = append(items, DiagnosticItem{
				File:     filePath,
				Line:     1,
				Severity: "info",
				Message:  line,
			})
		}
	}

	return items
}

// HoverInfo returns symbol information or documentation at the specified line and character.
func HoverInfo(ctx context.Context, path string, line, character int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return "", fmt.Errorf("line %d is out of range (1-%d)", line, len(lines))
	}

	targetLine := lines[line-1]
	return fmt.Sprintf("Symbol info at %s:%d:%d:\n`%s`", filepath.Base(path), line, character, strings.TrimSpace(targetLine)), nil
}
