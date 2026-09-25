package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodePatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patch_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	origFile := filepath.Join(tmpDir, "hello.txt")
	err = os.WriteFile(origFile, []byte("line 1\nline 2 to change\nline 3\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	patchText := `*** Begin Patch
*** Update File: ` + origFile + `
@@ line 1
-line 2 to change
+line 2 changed!
 line 3
*** Add File: ` + filepath.Join(tmpDir, "new_file.txt") + `
+hello from new file
*** End Patch`

	parsed, err := ParseOpenCodePatch(patchText)
	if err != nil {
		t.Fatalf("failed parsing patch: %v", err)
	}

	if len(parsed.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(parsed.Actions))
	}

	res, err := ApplyPatch(parsed, tmpDir)
	if err != nil {
		t.Fatalf("failed applying patch: %v", err)
	}

	if len(res.FilesChanged) != 2 {
		t.Fatalf("expected 2 files changed, got %d", len(res.FilesChanged))
	}

	// Verify updated file
	data, _ := os.ReadFile(origFile)
	if !strings.Contains(string(data), "line 2 changed!") {
		t.Fatalf("updated file content incorrect: %s", string(data))
	}

	// Verify added file
	newData, _ := os.ReadFile(filepath.Join(tmpDir, "new_file.txt"))
	if !strings.Contains(string(newData), "hello from new file") {
		t.Fatalf("new file content incorrect: %s", string(newData))
	}
}

func TestUnifiedDiff(t *testing.T) {
	oldText := "apple\nbanana\ncherry\n"
	newText := "apple\nblueberry\ncherry\n"

	diffText := GenerateUnifiedDiff("fruit.txt", oldText, newText)
	if !strings.Contains(diffText, "-banana") || !strings.Contains(diffText, "+blueberry") {
		t.Fatalf("unexpected diff output:\n%s", diffText)
	}
}
