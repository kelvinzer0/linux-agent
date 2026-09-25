package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolRegistry(t *testing.T) {
	reg := NewRegistry()
	defs := reg.GetDefinitions()
	if len(defs) < 10 {
		t.Fatalf("expected at least 10 tools registered, got %d", len(defs))
	}

	ctx := context.Background()

	// 1. Test execute_code
	res, err := reg.Execute(ctx, "execute_code", map[string]interface{}{
		"code":     "print(100 * 2)",
		"language": "python",
	})
	if err != nil || res.IsError {
		t.Fatalf("execute_code failed: %v, content: %v", err, res.Content)
	}
	if !strings.Contains(res.Content[0].Text, "200") {
		t.Fatalf("expected 200 in output, got %s", res.Content[0].Text)
	}

	// 2. Test bash
	res, err = reg.Execute(ctx, "bash", map[string]interface{}{
		"command": "echo 'linux-agent-bash-test'",
	})
	if err != nil || res.IsError {
		t.Fatalf("bash failed: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, "linux-agent-bash-test") {
		t.Fatalf("expected bash output match, got %s", res.Content[0].Text)
	}

	// 3. Test write & view & edit
	tmpDir, _ := os.MkdirTemp("", "tool_reg_*")
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "demo.txt")

	// write
	res, err = reg.Execute(ctx, "write", map[string]interface{}{
		"path":    testFile,
		"content": "line 1\noriginal target\nline 3\n",
	})
	if err != nil || res.IsError {
		t.Fatalf("write failed: %v", err)
	}

	// view
	res, err = reg.Execute(ctx, "view", map[string]interface{}{
		"path": testFile,
	})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].Text, "1: line 1") {
		t.Fatalf("view failed: %v, text: %v", err, res.Content)
	}

	// edit
	res, err = reg.Execute(ctx, "edit", map[string]interface{}{
		"path":    testFile,
		"old_str": "original target",
		"new_str": "edited replacement",
	})
	if err != nil || res.IsError {
		t.Fatalf("edit failed: %v", err)
	}

	// verify edit
	res, _ = reg.Execute(ctx, "view", map[string]interface{}{"path": testFile})
	if !strings.Contains(res.Content[0].Text, "edited replacement") {
		t.Fatalf("edit verification failed: %s", res.Content[0].Text)
	}

	// 4. Test glob
	res, err = reg.Execute(ctx, "glob", map[string]interface{}{
		"pattern": "*.txt",
		"path":    tmpDir,
	})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].Text, "demo.txt") {
		t.Fatalf("glob failed: %v, text: %v", err, res.Content)
	}

	// 5. Test grep
	res, err = reg.Execute(ctx, "grep", map[string]interface{}{
		"query": "edited replacement",
		"path":  tmpDir,
	})
	if err != nil || res.IsError || !strings.Contains(res.Content[0].Text, "edited replacement") {
		t.Fatalf("grep failed: %v, text: %v", err, res.Content)
	}
}
