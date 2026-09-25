package diff

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ActionType string

const (
	ActionAdd    ActionType = "add"
	ActionDelete ActionType = "delete"
	ActionUpdate ActionType = "update"
)

type Chunk struct {
	OrigContext string
	DelLines    []string
	InsLines    []string
}

type PatchAction struct {
	Type     ActionType
	NewFile  *string
	Chunks   []Chunk
	MovePath *string
}

type Patch struct {
	Actions map[string]PatchAction
}

type PatchResult struct {
	FilesChanged []string `json:"files_changed"`
	Additions    int      `json:"additions"`
	Removals     int      `json:"removals"`
	Summary      string   `json:"summary"`
}

// ParseOpenCodePatch parses the OpenCode *** Begin Patch format.
func ParseOpenCodePatch(patchText string) (*Patch, error) {
	lines := strings.Split(patchText, "\n")
	p := &Patch{Actions: make(map[string]PatchAction)}

	var currentFile string
	var currentAction *PatchAction
	var inPatch bool

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "*** Begin Patch" {
			inPatch = true
			continue
		}
		if trimmed == "*** End Patch" {
			if currentFile != "" && currentAction != nil {
				p.Actions[currentFile] = *currentAction
				currentFile = ""
				currentAction = nil
			}
			inPatch = false
			break
		}

		if !inPatch {
			// Allow lines if "Begin Patch" was omitted or at beginning
			if strings.HasPrefix(line, "*** Update File: ") ||
				strings.HasPrefix(line, "*** Add File: ") ||
				strings.HasPrefix(line, "*** Delete File: ") {
				inPatch = true
			} else {
				continue
			}
		}

		if strings.HasPrefix(line, "*** Update File: ") {
			if currentFile != "" && currentAction != nil {
				p.Actions[currentFile] = *currentAction
			}
			currentFile = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			currentAction = &PatchAction{
				Type:   ActionUpdate,
				Chunks: []Chunk{},
			}
			continue
		}

		if strings.HasPrefix(line, "*** Add File: ") {
			if currentFile != "" && currentAction != nil {
				p.Actions[currentFile] = *currentAction
			}
			currentFile = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			empty := ""
			currentAction = &PatchAction{
				Type:    ActionAdd,
				NewFile: &empty,
				Chunks:  []Chunk{},
			}
			continue
		}

		if strings.HasPrefix(line, "*** Delete File: ") {
			if currentFile != "" && currentAction != nil {
				p.Actions[currentFile] = *currentAction
			}
			currentFile = strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			currentAction = &PatchAction{
				Type:   ActionDelete,
				Chunks: []Chunk{},
			}
			continue
		}

		if strings.HasPrefix(line, "*** Move to: ") && currentAction != nil {
			moveTo := strings.TrimSpace(strings.TrimPrefix(line, "*** Move to: "))
			currentAction.MovePath = &moveTo
			continue
		}

		if currentAction == nil {
			continue
		}

		if currentAction.Type == ActionAdd {
			contentLine := line
			if strings.HasPrefix(line, "+") {
				contentLine = line[1:]
			}
			if *currentAction.NewFile == "" {
				*currentAction.NewFile = contentLine
			} else {
				*currentAction.NewFile += "\n" + contentLine
			}
			continue
		}

		if currentAction.Type == ActionUpdate {
			if strings.HasPrefix(line, "@@") {
				// New chunk
				currentAction.Chunks = append(currentAction.Chunks, Chunk{
					OrigContext: strings.TrimSpace(strings.TrimPrefix(line, "@@")),
					DelLines:    []string{},
					InsLines:    []string{},
				})
				continue
			}

			if len(currentAction.Chunks) == 0 {
				currentAction.Chunks = append(currentAction.Chunks, Chunk{})
			}

			lastChunk := &currentAction.Chunks[len(currentAction.Chunks)-1]
			if strings.HasPrefix(line, "-") {
				lastChunk.DelLines = append(lastChunk.DelLines, line[1:])
			} else if strings.HasPrefix(line, "+") {
				lastChunk.InsLines = append(lastChunk.InsLines, line[1:])
			} else if strings.HasPrefix(line, " ") {
				// Context line
			}
		}
	}

	if currentFile != "" && currentAction != nil {
		p.Actions[currentFile] = *currentAction
	}

	if len(p.Actions) == 0 {
		return nil, errors.New("no valid patch actions found in patch text")
	}

	return p, nil
}

// ApplyPatch applies the parsed patch atomically to the filesystem.
func ApplyPatch(p *Patch, baseDir string) (*PatchResult, error) {
	result := &PatchResult{
		FilesChanged: make([]string, 0, len(p.Actions)),
	}

	// Phase 1: Pre-validation & read all targets
	type plannedChange struct {
		targetPath string
		actionType ActionType
		oldContent string
		newContent string
		movePath   string
	}

	var planned []plannedChange

	for filePath, action := range p.Actions {
		fullPath := filePath
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(baseDir, filePath)
		}

		switch action.Type {
		case ActionAdd:
			if _, err := os.Stat(fullPath); err == nil {
				return nil, fmt.Errorf("cannot add file %s: already exists", filePath)
			}
			content := ""
			if action.NewFile != nil {
				content = *action.NewFile
			}
			lines := strings.Split(content, "\n")
			result.Additions += len(lines)
			planned = append(planned, plannedChange{
				targetPath: fullPath,
				actionType: ActionAdd,
				newContent: content,
			})

		case ActionDelete:
			data, err := os.ReadFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf("cannot delete file %s: %w", filePath, err)
			}
			lines := strings.Split(string(data), "\n")
			result.Removals += len(lines)
			planned = append(planned, plannedChange{
				targetPath: fullPath,
				actionType: ActionDelete,
			})

		case ActionUpdate:
			data, err := os.ReadFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf("cannot update file %s: %w", filePath, err)
			}
			oldStr := string(data)
			newStr, adds, rems, err := applyChunksToFile(oldStr, action.Chunks)
			if err != nil {
				return nil, fmt.Errorf("failed to apply patch chunks to %s: %w", filePath, err)
			}
			result.Additions += adds
			result.Removals += rems

			move := ""
			if action.MovePath != nil {
				move = *action.MovePath
				if !filepath.IsAbs(move) {
					move = filepath.Join(baseDir, move)
				}
			}

			planned = append(planned, plannedChange{
				targetPath: fullPath,
				actionType: ActionUpdate,
				oldContent: oldStr,
				newContent: newStr,
				movePath:   move,
			})
		}
	}

	// Phase 2: Apply changes
	for _, pc := range planned {
		switch pc.actionType {
		case ActionAdd:
			if err := os.MkdirAll(filepath.Dir(pc.targetPath), 0755); err != nil {
				return nil, fmt.Errorf("failed creating directory: %w", err)
			}
			if err := os.WriteFile(pc.targetPath, []byte(pc.newContent), 0644); err != nil {
				return nil, fmt.Errorf("failed writing file %s: %w", pc.targetPath, err)
			}
			result.FilesChanged = append(result.FilesChanged, pc.targetPath)

		case ActionDelete:
			if err := os.Remove(pc.targetPath); err != nil {
				return nil, fmt.Errorf("failed removing file %s: %w", pc.targetPath, err)
			}
			result.FilesChanged = append(result.FilesChanged, pc.targetPath)

		case ActionUpdate:
			destPath := pc.targetPath
			if pc.movePath != "" {
				_ = os.Remove(pc.targetPath)
				destPath = pc.movePath
				_ = os.MkdirAll(filepath.Dir(destPath), 0755)
			}
			if err := os.WriteFile(destPath, []byte(pc.newContent), 0644); err != nil {
				return nil, fmt.Errorf("failed updating file %s: %w", destPath, err)
			}
			result.FilesChanged = append(result.FilesChanged, destPath)
		}
	}

	result.Summary = fmt.Sprintf("Successfully modified %d file(s) (+%d lines, -%d lines)", len(result.FilesChanged), result.Additions, result.Removals)
	return result, nil
}

func applyChunksToFile(original string, chunks []Chunk) (string, int, int, error) {
	current := original
	totalAdds := 0
	totalRems := 0

	for _, chunk := range chunks {
		delText := strings.Join(chunk.DelLines, "\n")
		insText := strings.Join(chunk.InsLines, "\n")

		totalAdds += len(chunk.InsLines)
		totalRems += len(chunk.DelLines)

		if delText == "" && insText == "" {
			continue
		}

		if delText == "" {
			// Append or insert after context
			if chunk.OrigContext != "" && strings.Contains(current, chunk.OrigContext) {
				current = strings.Replace(current, chunk.OrigContext, chunk.OrigContext+"\n"+insText, 1)
			} else {
				current += "\n" + insText
			}
			continue
		}

		if !strings.Contains(current, delText) {
			// Try fuzzy match trimming whitespace
			trimmedDel := strings.TrimSpace(delText)
			if trimmedDel != "" && strings.Contains(current, trimmedDel) {
				current = strings.Replace(current, trimmedDel, insText, 1)
				continue
			}
			return "", 0, 0, fmt.Errorf("target deletion text not found in file:\n%s", delText)
		}

		current = strings.Replace(current, delText, insText, 1)
	}

	return current, totalAdds, totalRems, nil
}
