package skills

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kelvinzer0/linux-agent/internal/executor"
	"gopkg.in/yaml.v3"
)

type SkillFrontmatter struct {
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Triggers    []string          `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Metadata    map[string]string `yaml:",inline" json:"metadata,omitempty"`
}

type SkillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	SkillFile   string   `json:"skill_file"`
	Scripts     []string `json:"scripts,omitempty"`
}

type SkillDetail struct {
	SkillInfo
	Content      string            `json:"content"`
	Frontmatter  SkillFrontmatter  `json:"frontmatter"`
	AvailableDir map[string]string `json:"available_dirs,omitempty"`
}

// DefaultSkillDirectories returns common paths where .agents/skills are located.
func DefaultSkillDirectories(customRoots ...string) []string {
	var dirs []string

	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	allRoots := append([]string{cwd}, customRoots...)
	for _, root := range allRoots {
		if root == "" {
			continue
		}
		dirs = append(dirs,
			filepath.Join(root, ".agents", "skills"),
			filepath.Join(root, ".agent", "skills"),
			filepath.Join(root, "skills"),
		)
	}

	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".gemini", "config", "skills"),
			filepath.Join(home, ".gemini", "antigravity-cli", "builtin", "skills"),
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".config", "agents", "skills"),
		)
	}

	return dirs
}

// DiscoverSkills scans the provided search directories for valid skills.
func DiscoverSkills(searchDirs ...string) ([]SkillInfo, error) {
	if len(searchDirs) == 0 {
		searchDirs = DefaultSkillDirectories()
	}

	var results []SkillInfo
	seen := make(map[string]bool)

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillPath := filepath.Join(dir, entry.Name())
			skillMdPath := filepath.Join(skillPath, "SKILL.md")
			if _, err := os.Stat(skillMdPath); err != nil {
				// Also check skill.md lowercase
				skillMdPath = filepath.Join(skillPath, "skill.md")
				if _, err := os.Stat(skillMdPath); err != nil {
					continue
				}
			}

			fm, _, err := parseSkillFile(skillMdPath)
			name := entry.Name()
			desc := ""
			if err == nil {
				if fm.Name != "" {
					name = fm.Name
				}
				desc = fm.Description
			}

			if seen[name] {
				continue
			}
			seen[name] = true

			// Discover scripts in scripts/
			var scripts []string
			scriptsDir := filepath.Join(skillPath, "scripts")
			if sEntries, err := os.ReadDir(scriptsDir); err == nil {
				for _, se := range sEntries {
					if !se.IsDir() {
						scripts = append(scripts, se.Name())
					}
				}
			}

			results = append(results, SkillInfo{
				Name:        name,
				Description: desc,
				Path:        skillPath,
				SkillFile:   skillMdPath,
				Scripts:     scripts,
			})
		}
	}

	return results, nil
}

// GetSkillDetail retrieves the full content of a skill.
func GetSkillDetail(name string, searchDirs ...string) (*SkillDetail, error) {
	skills, err := DiscoverSkills(searchDirs...)
	if err != nil {
		return nil, err
	}

	for _, s := range skills {
		if strings.EqualFold(s.Name, name) || strings.EqualFold(filepath.Base(s.Path), name) {
			fm, content, err := parseSkillFile(s.SkillFile)
			if err != nil {
				return nil, fmt.Errorf("error reading %s: %w", s.SkillFile, err)
			}

			return &SkillDetail{
				SkillInfo:   s,
				Content:     content,
				Frontmatter: *fm,
			}, nil
		}
	}

	return nil, fmt.Errorf("skill %q not found in search paths", name)
}

// RunSkillScript executes a script inside the skill's scripts/ directory.
func RunSkillScript(ctx context.Context, skillName, scriptName string, args []string, cwd string, searchDirs ...string) (*executor.ExecutionResult, error) {
	detail, err := GetSkillDetail(skillName, searchDirs...)
	if err != nil {
		return nil, err
	}

	scriptPath := filepath.Join(detail.Path, "scripts", scriptName)
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("script %q not found in skill %s (%s)", scriptName, skillName, scriptPath)
	}

	if cwd == "" {
		cwd = detail.Path
	}

	ext := strings.ToLower(filepath.Ext(scriptPath))
	switch ext {
	case ".py":
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			return nil, err
		}
		return executor.ExecuteCode(ctx, string(content), "python", 120, cwd, args)
	case ".sh", ".bash", "":
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			return nil, err
		}
		return executor.ExecuteCode(ctx, string(content), "bash", 120, cwd, args)
	case ".js":
		content, err := os.ReadFile(scriptPath)
		if err != nil {
			return nil, err
		}
		return executor.ExecuteCode(ctx, string(content), "node", 120, cwd, args)
	default:
		return executor.ExecuteCode(ctx, scriptPath+" "+strings.Join(args, " "), "bash", 120, cwd, nil)
	}
}

func parseSkillFile(filePath string) (*SkillFrontmatter, string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var frontmatterLines []string
	var bodyLines []string

	inFrontmatter := false
	frontmatterDone := false
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		if lineNum == 1 && strings.TrimSpace(line) == "---" {
			inFrontmatter = true
			continue
		}

		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				frontmatterDone = true
				continue
			}
			frontmatterLines = append(frontmatterLines, line)
		} else {
			bodyLines = append(bodyLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, "", err
	}

	var fm SkillFrontmatter
	if frontmatterDone && len(frontmatterLines) > 0 {
		fmText := strings.Join(frontmatterLines, "\n")
		_ = yaml.Unmarshal([]byte(fmText), &fm)
	}

	return &fm, strings.Join(bodyLines, "\n"), nil
}
