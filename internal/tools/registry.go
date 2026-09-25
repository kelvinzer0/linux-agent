package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	// ─── 1. Code Interpreter & Linux System ───
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

	// ─── 2. OpenCode Native Tooling (100% Parameter & Behavior Match) ───

	// bash (OpenCode BashToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "bash",
		Description: "Executes a given bash command in a shell session with optional timeout, ensuring proper handling and security measures.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The command to execute in the bash shell",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "The timeout for the command in milliseconds or seconds (optional)",
				},
				"cwd": map[string]interface{}{
					"type":        "string",
					"description": "The working directory for command execution",
				},
			},
			"required": []string{"command"},
		},
	}, handleBash)

	// view (OpenCode ViewToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "view",
		Description: "Reads and displays the content of a file or lists files in a directory with 1-based line numbers and pagination.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to read (or directory to list)",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "The line number to start reading from (0-based or 1-based)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "The number of lines to read (defaults to 2000)",
				},
			},
			"required": []string{"file_path"},
		},
	}, handleView)

	// write (OpenCode WriteToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "write",
		Description: "Writes content to a file. Overwrites existing content or creates a new file. Automatically creates parent directories.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to write to the file",
				},
			},
			"required": []string{"file_path", "content"},
		},
	}, handleWrite)

	// edit (OpenCode EditToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "edit",
		Description: "Replaces text in a file. Requires exact match of old_string and replaces it with new_string. old_string must be unique.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to modify",
				},
				"old_string": map[string]interface{}{
					"type":        "string",
					"description": "The text to replace",
				},
				"new_string": map[string]interface{}{
					"type":        "string",
					"description": "The text to replace it with",
				},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
	}, handleEdit)

	// patch (OpenCode PatchToolName)
	patchDef := protocol.ToolDefinition{
		Name:        "patch",
		Description: "Applies a patch to multiple files in one operation. Follows OpenCode format: '*** Begin Patch', '*** Update File: ...', '@@ ...', '-del', '+ins', '*** Add File: ...', '*** Delete File: ...', '*** End Patch'.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"patch_text": map[string]interface{}{
					"type":        "string",
					"description": "The full patch text that describes all changes to be made",
				},
			},
			"required": []string{"patch_text"},
		},
	}
	r.Register(patchDef, handlePatch)
	// Also alias as apply_patch
	applyPatchDef := patchDef
	applyPatchDef.Name = "apply_patch"
	r.Register(applyPatchDef, handlePatch)

	// ls (OpenCode LSToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "ls",
		Description: "Lists files and directories with file size and modified timestamps.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the directory to list (defaults to current working directory)",
				},
				"ignore": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "List of glob patterns to ignore",
				},
			},
		},
	}, handleLS)

	// glob (OpenCode GlobToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "glob",
		Description: "Finds files matching a glob pattern across directories recursively.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "The glob pattern to match against file paths",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The directory to search in. Defaults to current working directory.",
				},
			},
			"required": []string{"pattern"},
		},
	}, handleGlob)

	// grep (OpenCode GrepToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "grep",
		Description: "Searches for regex patterns or literal text across file contents in a directory.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "The regex pattern to search for in file contents",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The directory to search in. Defaults to current working directory.",
				},
				"include": map[string]interface{}{
					"type":        "string",
					"description": "File pattern to include in search (e.g. '*.go', '*.ts')",
				},
				"literal_text": map[string]interface{}{
					"type":        "boolean",
					"description": "If true, pattern will be treated as literal text. Default false.",
				},
			},
			"required": []string{"pattern"},
		},
	}, handleGrep)

	// fetch (OpenCode FetchToolName)
	r.Register(protocol.ToolDefinition{
		Name:        "fetch",
		Description: "Fetches content from a URL via HTTP GET and returns it in text, markdown, or html format.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to fetch content from",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "The format to return content in (text, markdown, html)",
					"enum":        []string{"text", "markdown", "html"},
				},
				"timeout": map[string]interface{}{
					"type":        "number",
					"description": "Optional timeout in seconds (default 30, max 120)",
				},
			},
			"required": []string{"url", "format"},
		},
	}, handleFetch)

	// diagnostics (OpenCode DiagnosticsToolName)
	diagDef := protocol.ToolDefinition{
		Name:        "diagnostics",
		Description: "Gets compiler and linter diagnostics for a file or directory (syntax errors, warnings, type issues).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to get diagnostics for (leave empty for project diagnostics)",
				},
			},
		},
	}
	r.Register(diagDef, handleDiagnostics)
	// Also alias as lsp_diagnostics
	lspDiagDef := diagDef
	lspDiagDef.Name = "lsp_diagnostics"
	r.Register(lspDiagDef, handleDiagnostics)

	// diff (Unified Diff)
	r.Register(protocol.ToolDefinition{
		Name:        "diff",
		Description: "Generates a line-by-line unified diff comparing two files or two text snippets.",
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
					"description": "Original text string.",
				},
				"new_text": map[string]interface{}{
					"type":        "string",
					"description": "Modified text string.",
				},
			},
		},
	}, handleDiff)

	// lsp_hover
	r.Register(protocol.ToolDefinition{
		Name:        "lsp_hover",
		Description: "Gets symbol definition and hover information at a specific line and column.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the source file.",
				},
				"line": map[string]interface{}{
					"type":        "integer",
					"description": "Line number.",
				},
				"character": map[string]interface{}{
					"type":        "integer",
					"description": "Character column.",
				},
			},
			"required": []string{"file_path", "line", "character"},
		},
	}, handleLspHover)

	// ─── 3. .agents/skills Compatibility ───
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
		// OpenCode accepts milliseconds or seconds
		if t > 1000 {
			timeoutSec = int(t / 1000)
		} else {
			timeoutSec = int(t)
		}
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
	// Support both file_path (OpenCode standard) and path
	filePath, _ := params["file_path"].(string)
	if filePath == "" {
		filePath, _ = params["path"].(string)
	}
	if filePath == "" {
		return protocol.ErrorResult("'file_path' is required"), nil
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("cannot access %s: %v", filePath, err)), nil
	}

	if fi.IsDir() {
		entries, err := os.ReadDir(filePath)
		if err != nil {
			return protocol.ErrorResult(err.Error()), nil
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Directory %s:\n", filePath))
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

	data, err := os.ReadFile(filePath)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	lines := strings.Split(string(data), "\n")
	offset := 0
	if o, ok := params["offset"].(float64); ok && o >= 0 {
		offset = int(o)
	}
	limit := len(lines)
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	startIdx := offset
	// Adjust if 1-based offset was provided
	if startIdx > 0 && startIdx <= len(lines) {
		startIdx = startIdx - 1
	}
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
	filePath, _ := params["file_path"].(string)
	if filePath == "" {
		filePath, _ = params["path"].(string)
	}
	content, ok := params["content"].(string)
	if filePath == "" || !ok {
		return protocol.ErrorResult("'file_path' and 'content' are required"), nil
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed creating parent directory: %v", err)), nil
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed writing file: %v", err)), nil
	}

	return protocol.TextResult(fmt.Sprintf("File written successfully: %s (%d bytes)", filePath, len(content))), nil
}

func handleEdit(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	filePath, _ := params["file_path"].(string)
	if filePath == "" {
		filePath, _ = params["path"].(string)
	}
	oldStr, okOld := params["old_string"].(string)
	if !okOld || oldStr == "" {
		oldStr, okOld = params["old_str"].(string)
	}
	newStr, okNew := params["new_string"].(string)
	if !okNew {
		newStr, okNew = params["new_str"].(string)
	}

	if filePath == "" || !okOld || !okNew {
		return protocol.ErrorResult("'file_path', 'old_string', and 'new_string' are required"), nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed reading %s: %v", filePath, err)), nil
	}

	content := string(data)
	count := strings.Count(content, oldStr)
	if count == 0 {
		return protocol.ErrorResult(fmt.Sprintf("old_string not found in %s", filePath)), nil
	}
	if count > 1 {
		return protocol.ErrorResult(fmt.Sprintf("old_string occurs %d times in %s; edit requires unique target occurrence", count, filePath)), nil
	}

	replaced := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(filePath, []byte(replaced), 0644); err != nil {
		return protocol.ErrorResult(fmt.Sprintf("failed writing edit: %v", err)), nil
	}

	return protocol.TextResult(fmt.Sprintf("File modified successfully: %s", filePath)), nil
}

func handleLS(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path := "."
	if p, ok := params["path"].(string); ok && p != "" {
		path = p
	}

	var ignores []string
	if ignRaw, ok := params["ignore"].([]interface{}); ok {
		for _, item := range ignRaw {
			if s, ok := item.(string); ok {
				ignores = append(ignores, s)
			}
		}
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Path: %s\n", path))
	for _, e := range entries {
		name := e.Name()
		skip := false
		for _, ign := range ignores {
			if matched, _ := filepath.Match(ign, name); matched {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

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
		sb.WriteString(fmt.Sprintf("  %-30s %10d bytes  %s\n", name+suffix, size, modTime))
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
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules") {
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
	pattern, _ := params["pattern"].(string)
	if pattern == "" {
		pattern, _ = params["query"].(string)
	}
	if pattern == "" {
		return protocol.ErrorResult("'pattern' is required"), nil
	}

	root := "."
	if p, ok := params["path"].(string); ok && p != "" {
		root = p
	}

	include := ""
	if inc, ok := params["include"].(string); ok {
		include = inc
	}

	literal, _ := params["literal_text"].(bool)
	var re *regexp.Regexp
	var err error

	if literal {
		re = regexp.MustCompile(regexp.QuoteMeta(pattern))
	} else {
		re, err = regexp.Compile(pattern)
		if err != nil {
			return protocol.ErrorResult(fmt.Sprintf("invalid regex pattern: %v", err)), nil
		}
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

func handleFetch(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	urlStr, _ := params["url"].(string)
	format, _ := params["format"].(string)
	if urlStr == "" || format == "" {
		return protocol.ErrorResult("'url' and 'format' are required"), nil
	}

	timeoutSec := 30
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(execCtx, "GET", urlStr, nil)
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}
	req.Header.Set("User-Agent", "linux-agent/1.0.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("Failed fetching URL: %v", err)), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return protocol.ErrorResult(err.Error()), nil
	}

	text := string(body)
	return protocol.TextResult(text), nil
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

func handleDiagnostics(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path, _ := params["file_path"].(string)
	if path == "" {
		path, _ = params["path"].(string)
	}
	if path == "" {
		path = "."
	}

	res, err := lsp.GetDiagnostics(ctx, path)
	if err != nil {
		return protocol.ErrorResult(fmt.Sprintf("diagnostics error: %v", err)), nil
	}

	b, _ := json.MarshalIndent(res, "", "  ")
	return protocol.TextResult(string(b)), nil
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

func handleLspHover(ctx context.Context, params map[string]interface{}) (protocol.ToolResult, error) {
	path, _ := params["file_path"].(string)
	if path == "" {
		path, _ = params["path"].(string)
	}
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
