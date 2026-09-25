package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kelvinzer0/linux-agent/internal/diff"
	"github.com/kelvinzer0/linux-agent/internal/executor"
	"github.com/kelvinzer0/linux-agent/internal/lsp"
	"github.com/kelvinzer0/linux-agent/internal/protocol"
	"github.com/kelvinzer0/linux-agent/internal/skills"
)

type ToolHandler func(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error)

type Registry struct {
	tools    map[string]protocol.ToolDefinition
	handlers map[string]ToolHandler
}

func NewRegistry() *Registry {
	r := &Registry{
		tools:    make(map[string]protocol.ToolDefinition),
		handlers: make(map[string]ToolHandler),
	}
	r.registerAll()
	return r
}

func (r *Registry) Register(def protocol.ToolDefinition, handler ToolHandler) {
	r.tools[def.Name] = def
	r.handlers[def.Name] = handler
}

func (r *Registry) GetDefinitions() []protocol.ToolDefinition {
	list := make([]protocol.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, t)
	}
	return list
}

func (r *Registry) Execute(ctx context.Context, name string, params map[string]interface{}) (protocol.ToolResult, error) {
	handler, ok := r.handlers[name]
	if !ok {
		return protocol.ErrorResult(fmt.Sprintf("tool %q not found", name)), nil
	}
	return handler(ctx, params)
}

func (r *Registry) registerAll() {
	// 1. execute_code
	r.Register(protocol.ToolDefinition{
		Name:        "execute_code",
		Description: "Executes Python, Bash, Sh, Node.js, or Go scripts in the Linux execution environment. Returns stdout, stderr, exit code, and execution time.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code": map[string]interface{}{
					"type":        "string",
					"description": "The code script content to execute.",
				},
				"language": map[string]interface{}{
					"type":        "string",
					"description": "Programming language: 'python', 'bash', 'sh', 'javascript', 'go'.",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Execution timeout in seconds (default 60, max 600).",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Working directory for execution.",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional command-line arguments to pass to the script.",
				},
			},
			"required": []string{"code", "language"},
		},
	}, handleExecuteCode)

	// 2. bash
	r.Register(protocol.ToolDefinition{
		Name:        "bash",
		Description: "Executes a given bash shell command in the Linux environment with timeout handling.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The bash shell command to execute.",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Timeout in seconds (default 60).",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Working directory to execute command in.",
				},
			},
			"required": []string{"command"},
		},
	}, handleBash)

	// 3. view (OpenCode File View)
	r.Register(protocol.ToolDefinition{
		Name:        "view",
		Description: "Views file content with line numbers, or lists directory items if target is a directory. Supports offset and limit pagination.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file or directory to view.",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "1-based starting line number to read (default 1).",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of lines to return.",
				},
			},
			"required": []string{"path"},
		},
	}, handleView)

	// 4. write (OpenCode File Write)
	r.Register(protocol.ToolDefinition{
		Name:        "write",
		Description: "Writes content to a file. Creates parent directories automatically if they do not exist.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Target file path to write to.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The exact content to write to the file.",
				},
			},
			"required": []string{"path", "content"},
		},
	}, handleWrite)

	// 5. edit (OpenCode File Edit)
	r.Register(protocol.ToolDefinition{
		Name:        "edit",
		Description: "Edits a file by replacing a unique occurrence of target string with replacement string.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to edit.",
				},
				"old_str": map[string]interface{}{
					"type":        "string",
					"description": "The exact text to find and replace.",
				},
				"new_str": map[string]interface{}{
					"type":        "string",
					"description": "The replacement text.",
				},
			},
			"required": []string{"path", "old_str", "new_str"},
		},
	}, handleEdit)

	// 6. ls (OpenCode LS)
	r.Register(protocol.ToolDefinition{
		Name:        "ls",
		Description: "Lists files and subdirectories with file size, permissions, and directory structure.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory path to list (default: current directory).",
				},
			},
		},
	}, handleLS)

	// 7. glob (OpenCode Glob)
	r.Register(protocol.ToolDefinition{
		Name:        "glob",
		Description: "Searches for files matching a wildcard pattern recursively.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Glob pattern (e.g. '**/*.go', '*.json', 'src/**/*.ts').",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Root path to search from (default: current directory).",
				},
			},
			"required": []string{"pattern"},
		},
	}, handleGlob)

	// 8. grep (OpenCode Grep)
	r.Register(protocol.ToolDefinition{
		Name:        "grep",
		Description: "Searches for regular expressions or literal strings across files in a directory.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search pattern or regular expression.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Directory or file to search in (default: current directory).",
				},
				"include": map[string]interface{}{
					"type":        "string",
					"description": "File pattern to include (e.g. '*.go', '*.ts').",
				},
			},
			"required": []string{"query"},
		},
	}, handleGrep)

	// 9. patch (OpenCode Atomic Patch)
	r.Register(protocol.ToolDefinition{
		Name:        "patch",
		Description: "Applies an OpenCode multi-file atomic patch. Format: '*** Begin Patch', '*** Update File: ...', '@@ ...', '-del', '+ins', '*** Add File: ...', '*** Delete File: ...', '*** End Patch'.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"patch_text": map[string]interface{}{
					"type":        "string",
					"description": "The full patch text to apply atomically.",
				},
			},
			"required": []string{"patch_text"},
		},
	}, handlePatch)

	// 10. diff (OpenCode Unified Diff)
	r.Register(protocol.ToolDefinition{
		Name:        "diff",
		Description: "Generates a unified diff comparing two files or two text snippets.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file1": map[string]interface{}{
					"type":        "string",
					"description": "Path to original file.",
				},
				"file2": map[string]interface{}{
					"type":        "string",
					"description": "Path to modified file.",
				},
				"old_text": map[string]interface{}{
					"type":        "string",
					"description": "Original text string (if comparing texts).",
				},
				"new_text": map[string]interface{}{
					"type":        "string",
					"description": "Modified text string (if comparing texts).",
				},
			},
		},
	}, handleDiff)

	// 11. lsp_diagnostics (OpenCode LSP Diagnostics)
	r.Register(protocol.ToolDefinition{
		Name:        "lsp_diagnostics",
		Description: "Analyzes a file or directory for compiler and linter diagnostics, syntax errors, and warnings.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to file or directory to diagnose.",
				},
			},
			"required": []string{"path"},
		},
	}, handleLspDiagnostics)

	// 12. lsp_hover (OpenCode LSP Hover)
	r.Register(protocol.ToolDefinition{
		Name:        "lsp_hover",
		Description: "Gets symbol definition and hover information at a specific line and column.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the source file.",
				},
				"line": map[string]interface{}{
					"type":        "integer",
					"description": "1-based line number.",
				},
				"character": map[string]interface{}{
					"type":        "integer",
					"description": "0-based or 1-based character column number.",
				},
			},
			"required": []string{"path", "line", "character"},
		},
	}, handleLspHover)

	// 13. list_skills (.agents/skills Compatibility)
	r.Register(protocol.ToolDefinition{
		Name:        "list_skills",
		Description: "Discovers all available skills in .agents/skills, .agent/skills, and ~/.gemini/config/skills.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional project root to search skills from.",
				},
			},
		},
	}, handleListSkills)

	// 14. read_skill (.agents/skills Compatibility)
	r.Register(protocol.ToolDefinition{
		Name:        "read_skill",
		Description: "Reads the full instructions and metadata of a specific skill by name.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the skill to read.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional project root to search in.",
				},
			},
			"required": []string{"name"},
		},
	}, handleReadSkill)

	// 15. run_skill_script (.agents/skills Script Runner)
	r.Register(protocol.ToolDefinition{
		Name:        "run_skill_script",
		Description: "Executes a script from the skill's scripts/ directory in the Linux environment.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skill": map[string]interface{}{
					"type":        "string",
					"description": "Name of the skill.",
				},
				"script": map[string]interface{}{
					"type":        "string",
					"description": "Filename of the script inside scripts/ (e.g. 'run.sh', 'audit.py').",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Arguments to pass to the script.",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "Optional working directory.",
				},
			},
			"required": []string{"skill", "script"},
		},
	}, handleRunSkillScript)
}

// ─── Tool Handlers ───

func handleExecuteCode(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	code, _ := params["code"].(string)
	lang, _ := params["language"].(string)
	if code == "" || lang == "" {
		return protocol.ErrorResult("both 'code' and 'language' are required"), nil
	}

	timeoutSec := 60
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	cwd, _ := params["cwd"].(string)

	var extraArgs []string
	if argsRaw, ok := params["args"].([]interface{}); ok {
		for _, a := range argsRaw {
			if s, ok := a.(string); ok {
				extraArgs = append(extraArgs, s)
			}
		}
	}

	res, err := executor.ExecuteCode(ctx, code, lang, timeoutSec, cwd, extraArgs)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("Failed to execute code: %v", err)), nil
	}

	return protocol.ToolResult{
		Content: []protocol.ToolContent{{Type: "text", Text: res.Formatted()}},
		IsError: res.IsError,
	}, nil
}

func handleBash(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	cmdStr, _ := params["command"].(string)
	if cmdStr == "" {
		return protocol.ErrorResult("'command' is required"), nil
	}

	timeoutSec := 60
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}
	cwd, _ := params["cwd"].(string)

	res, err := executor.ExecuteCode(ctx, cmdStr, "bash", timeoutSec, cwd, nil)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("Failed executing bash: %v", err)), nil
	}

	return protocol.ToolResult{
		Content: []protocol.ToolContent{{Type: "text", Text: res.Formatted()}},
		IsError: res.IsError,
	}, nil
}

func handleView(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return protocol.ErrorResult("'path' is required"), nil
	}

	fi, err := os.Stat(path)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("cannot access %s: %v", path, err)), nil
	}

	if fi.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return protocol.ErrorResult(err.Error()), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Directory %s:\n", path))
		for _, e := range entries {
			typeIndicator := ""
			if e.IsDir() {
				typeIndicator = "/"
			}
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			sb.WriteString(fmt.Sprintf("  %s%s (%d bytes)\n", e.Name(), typeIndicator, size))
		}
		return protocol.TextResult(sb.String()), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	lines := strings.Split(string(data), "\n")
	offset := 1
	if o, ok := params["offset"].(float64); ok && o > 0 {
		offset = int(o)
	}
	limit := len(lines)
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	startIdx := offset - 1
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= len(lines) {
		return protocol.TextResult("(offset exceeds total file lines)"), nil
	}

	endIdx := startIdx + limit
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	var sb strings.Builder
	for i := startIdx; i < endIdx; i++ {
		sb.WriteString(fmt.Sprintf("%d: %s\n", i+1, lines[i]))
	}

	return protocol.TextResult(sb.String()), nil
}

func handleWrite(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path, _ := params["path"].(string)
	content, ok := params["content"].(string)
	if path == "" || !ok {
		return protocol.ErrorResult("'path' and 'content' are required"), nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed creating parent directory: %v", err)), nil
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed writing file: %v", err)), nil
	}

	return protocol.TextResult(fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path)), nil
}

func handleEdit(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path, _ := params["path"].(string)
	oldStr, okOld := params["old_str"].(string)
	newStr, okNew := params["new_str"].(string)
	if path == "" || !okOld || !okNew {
		return protocol.ErrorResult("'path', 'old_str', and 'new_str' are required"), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed reading %s: %v", path, err)), nil
	}

	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return protocol.ErrorResult(fmt.Sprintf("old_str not found in %s", path)), nil
	}
	if count > 1 {
		return protocol.ErrorResult(fmt.Sprintf("old_str occurs %d times in %s; edit requires unique target occurrence", count, path)), nil
	}

	replaced := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(replaced), 0644); err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed writing edit: %v", err)), nil
	}

	return protocol.TextResult(fmt.Sprintf("Successfully replaced target string in %s", path)), nil
}

func handleLS(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path := "."
	if p, ok := params["path"].(string); ok && p != "" {
		path = p
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Path: %s\n", path))
	for _, e := range entries {
		info, _ := e.Info()
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		size := int64(0)
		modTime := ""
		if info != nil {
			size = info.Size()
			modTime = info.ModTime().Format("2006-01-02 15:04:05")
		}
		sb.WriteString(fmt.Sprintf("  %-30s %10d bytes  %s\n", e.Name()+suffix, size, modTime))
	}

	return protocol.TextResult(sb.String()), nil
}

func handleGlob(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	pattern, _ := params["pattern"].(string)
	if pattern == "" {
		return protocol.ErrorResult("'pattern' is required"), nil
	}

	root := "."
	if p, ok := params["path"].(string); ok && p != "" {
		root = p
	}

	var matches []string
	cleanPattern := filepath.Base(pattern)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Ignore .git
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		matched, _ := filepath.Match(cleanPattern, info.Name())
		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	if len(matches) == 0 {
		return protocol.TextResult("No files matched the pattern."), nil
	}

	return protocol.TextResult(strings.Join(matches, "\n")), nil
}

func handleGrep(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return protocol.ErrorResult("'query' is required"), nil
	}

	root := "."
	if p, ok := params["path"].(string); ok && p != "" {
		root = p
	}

	include := ""
	if inc, ok := params["include"].(string); ok {
		include = inc
	}

	re, err := regexp.Compile(query)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("invalid regex pattern: %v", err)), nil
	}

	var matches []string
	limit := 100

	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(matches) >= limit {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		if include != "" {
			matched, _ := filepath.Match(include, info.Name())
			if !matched {
				return nil
			}
		}

		// Don't search large files (>2MB)
		if info.Size() > 2*1024*1024 {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", path, idx+1, strings.TrimSpace(line)))
				if len(matches) >= limit {
					break
				}
			}
		}

		return nil
	})

	if len(matches) == 0 {
		return protocol.TextResult("No matching lines found."), nil
	}

	return protocol.TextResult(strings.Join(matches, "\n")), nil
}

func handlePatch(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	patchText, _ := params["patch_text"].(string)
	if patchText == "" {
		return protocol.ErrorResult("'patch_text' is required"), nil
	}

	parsed, err := diff.ParseOpenCodePatch(patchText)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("patch parsing error: %v", err)), nil
	}

	cwd, _ := os.Getwd()
	res, err := diff.ApplyPatch(parsed, cwd)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed applying patch: %v", err)), nil
	}

	return protocol.TextResult(fmt.Sprintf("✅ Patch applied successfully!\n%s\nFiles modified:\n- %s",
		res.Summary, strings.Join(res.FilesChanged, "\n- "))), nil
}

func handleDiff(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	file1, _ := params["file1"].(string)
	file2, _ := params["file2"].(string)
	oldText, _ := params["old_text"].(string)
	newText, _ := params["new_text"].(string)

	if file1 != "" && file2 != "" {
		out, err := diff.CompareFiles(file1, file2)
		if err != nil {
			return protocol.ErrorResult(err.Error()), nil
		}
		return protocol.TextResult(out), nil
	}

	if oldText != "" || newText != "" {
		out := diff.GenerateUnifiedDiff("text_diff", oldText, newText)
		return protocol.TextResult(out), nil
	}

	return protocol.ErrorResult("must provide either file1 and file2, or old_text and new_text"), nil
}

func handleLspDiagnostics(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return protocol.ErrorResult("'path' is required"), nil
	}

	res, err := lsp.GetDiagnostics(ctx, path)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("diagnostics error: %v", err)), nil
	}

	b, _ := json.MarshalIndent(res, "", "  ")
	return protocol.TextResult(string(b)), nil
}

func handleLspHover(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path, _ := params["path"].(string)
	line := 1
	char := 0
	if l, ok := params["line"].(float64); ok {
		line = int(l)
	}
	if c, ok := params["character"].(float64); ok {
		char = int(c)
	}

	hover, err := lsp.HoverInfo(ctx, path, line, char)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	return protocol.TextResult(hover), nil
}

func handleListSkills(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	var searchDirs []string
	if root, ok := params["path"].(string); ok && root != "" {
		searchDirs = skills.DefaultSkillDirectories(root)
	}

	list, err := skills.DiscoverSkills(searchDirs...)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	if len(list) == 0 {
		return protocol.TextResult("No skills found in workspace or global search paths (.agents/skills, ~/.gemini/config/skills)."), nil
	}

	b, _ := json.MarshalIndent(list, "", "  ")
	return protocol.TextResult(string(b)), nil
}

func handleReadSkill(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return protocol.ErrorResult("'name' is required"), nil
	}

	var searchDirs []string
	if root, ok := params["path"].(string); ok && root != "" {
		searchDirs = skills.DefaultSkillDirectories(root)
	}

	detail, err := skills.GetSkillDetail(name, searchDirs...)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Skill: %s\n", detail.Name))
	if detail.Description != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", detail.Description))
	}
	sb.WriteString(fmt.Sprintf("📁 **Path**: `%s`\n", detail.Path))
	if len(detail.Scripts) > 0 {
		sb.WriteString(fmt.Sprintf("⚡ **Available Scripts**: `%s`\n\n", strings.Join(detail.Scripts, "`, `")))
	}
	sb.WriteString("## Instructions (SKILL.md)\n")
	sb.WriteString(detail.Content)

	return protocol.TextResult(sb.String()), nil
}

func handleRunSkillScript(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	skillName, _ := params["skill"].(string)
	scriptName, _ := params["script"].(string)
	if skillName == "" || scriptName == "" {
		return protocol.ErrorResult("both 'skill' and 'script' are required"), nil
	}

	var args []string
	if argsRaw, ok := params["args"].([]interface{}); ok {
		for _, a := range argsRaw {
			if s, ok := a.(string); ok {
				args = append(args, s)
			}
		}
	}

	cwd, _ := params["cwd"].(string)

	res, err := skills.RunSkillScript(ctx, skillName, scriptName, args, cwd)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	return protocol.ToolResult{
		Content: []protocol.ToolContent{{Type: "text", Text: res.Formatted()}},
		IsError: res.IsError,
	}, nil
}
