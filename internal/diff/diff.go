package diff

import (
	"fmt"
	"os"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// GenerateUnifiedDiff generates a line-by-line unified diff between oldText and newText.
func GenerateUnifiedDiff(fileName, oldText, newText string) string {
	dmp := diffmatchpatch.New()
	chars1, chars2, lineArray := dmp.DiffLinesToChars(oldText, newText)
	diffs := dmp.DiffMain(chars1, chars2, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	hasDiff := false
	for _, d := range diffs {
		if d.Type != diffmatchpatch.DiffEqual {
			hasDiff = true
			break
		}
	}

	if !hasDiff {
		return fmt.Sprintf("--- %s\n+++ %s\n(no differences)\n", fileName, fileName)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", fileName, fileName))

	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		// If trailing empty line from split, ignore last empty
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		prefix := " "
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			prefix = "+"
		case diffmatchpatch.DiffDelete:
			prefix = "-"
		}

		for _, l := range lines {
			sb.WriteString(fmt.Sprintf("%s%s\n", prefix, l))
		}
	}

	return sb.String()
}

// CompareFiles reads two files from disk and returns their unified diff.
func CompareFiles(file1, file2 string) (string, error) {
	data1, err := os.ReadFile(file1)
	if err != nil {
		return "", fmt.Errorf("failed reading file 1 (%s): %w", file1, err)
	}
	data2, err := os.ReadFile(file2)
	if err != nil {
		return "", fmt.Errorf("failed reading file 2 (%s): %w", file2, err)
	}

	return GenerateUnifiedDiff(file1+" <=> "+file2, string(data1), string(data2)), nil
}
