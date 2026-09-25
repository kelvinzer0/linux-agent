package executor

import (
	"context"
	"strings"
	"testing"
)

func TestExecuteCode_Python(t *testing.T) {
	code := `
a = 10
b = 25
print(f"Result: {a + b}")
`
	res, err := ExecuteCode(context.Background(), code, "python", 10, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr: %s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Result: 35") {
		t.Fatalf("expected output 'Result: 35', got %q", res.Stdout)
	}
}

func TestExecuteCode_Bash(t *testing.T) {
	code := `echo "Hello Linux Agent"`
	res, err := ExecuteCode(context.Background(), code, "bash", 10, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "Hello Linux Agent") {
		t.Fatalf("expected output 'Hello Linux Agent', got %q", res.Stdout)
	}
}

func TestExecuteCode_Timeout(t *testing.T) {
	code := `sleep 5`
	res, err := ExecuteCode(context.Background(), code, "bash", 1, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected timeout error")
	}
}
