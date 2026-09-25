package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsCompatibility(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "skills_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	skillDir := filepath.Join(tmpDir, ".agents", "skills", "test-runner")
	scriptsDir := filepath.Join(skillDir, "scripts")
	err = os.MkdirAll(scriptsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	skillMd := `---
name: test-runner
description: Automated test runner skill
triggers:
  - test
---

# Test Runner Skill
Follow these steps to run tests.
`
	err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMd), 0644)
	if err != nil {
		t.Fatal(err)
	}

	testScript := `#!/bin/bash
echo "Skill Script Output: arg=$1"
`
	err = os.WriteFile(filepath.Join(scriptsDir, "run_test.sh"), []byte(testScript), 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Test DiscoverSkills
	skills, err := DiscoverSkills(filepath.Join(tmpDir, ".agents", "skills"))
	if err != nil {
		t.Fatalf("failed discovering skills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "test-runner" {
		t.Fatalf("expected skill name 'test-runner', got %q", skills[0].Name)
	}
	if len(skills[0].Scripts) != 1 || skills[0].Scripts[0] != "run_test.sh" {
		t.Fatalf("expected script 'run_test.sh', got %v", skills[0].Scripts)
	}

	// Test GetSkillDetail
	detail, err := GetSkillDetail("test-runner", filepath.Join(tmpDir, ".agents", "skills"))
	if err != nil {
		t.Fatalf("failed getting skill detail: %v", err)
	}
	if !strings.Contains(detail.Content, "Test Runner Skill") {
		t.Fatalf("content missing expected text: %s", detail.Content)
	}

	// Test RunSkillScript
	res, err := RunSkillScript(context.Background(), "test-runner", "run_test.sh", []string{"foo"}, tmpDir, filepath.Join(tmpDir, ".agents", "skills"))
	if err != nil {
		t.Fatalf("failed running skill script: %v", err)
	}
	if !strings.Contains(res.Stdout, "Skill Script Output: arg=foo") {
		t.Fatalf("unexpected script stdout: %s", res.Stdout)
	}
}
