// ABOUTME: Parser and applier for the v4a patch format used by coding agents.
// ABOUTME: Supports Add/Delete/Update/Move file operations with hunk matching, context hints, and fuzzy fallback.

package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// PatchOpType identifies the kind of file operation in a patch.
type PatchOpType string

const (
	PatchOpAdd    PatchOpType = "add"
	PatchOpDelete PatchOpType = "delete"
	PatchOpUpdate PatchOpType = "update"
	PatchOpMove   PatchOpType = "move"
)

// Patch represents a parsed v4a format patch containing one or more file operations.
type Patch struct {
	Operations []PatchOperation
}

// PatchOperation represents a single file operation within a patch.
type PatchOperation struct {
	Type    PatchOpType
	Path    string
	MoveTo  string   // Only used for Move operations
	Content []string // Only used for Add operations (lines without the + prefix)
	Hunks   []Hunk   // Only used for Update operations
}

// Hunk represents a single change region within an Update operation.
// It contains context lines for locating the change, lines to delete, and lines to add.
// MatchLines and ReplaceLines preserve the interleaved order of context and change lines,
// which is required for correct matching against the file.
type Hunk struct {
	ContextHint  string   // Optional search hint from @@@ ... @@@ or @@ ... markers
	ContextLines []string // Lines prefixed with space (context for matching)
	DeleteLines  []string // Lines prefixed with - (to remove)
	AddLines     []string // Lines prefixed with + (to insert)
	MatchLines   []string // Context + delete lines in original order (for file matching)
	ReplaceLines []string // Context + add lines in original order (replacement content)
}

// PatchResult holds the outcome of applying a patch.
type PatchResult struct {
	Summary       string
	FilesCreated  int
	FilesDeleted  int
	FilesModified int
	FilesMoved    int
	Details       []string
}

// ParsePatch parses a v4a format patch string into a structured Patch.
// The parser is lenient with trailing whitespace but strict on the *** markers.
func ParsePatch(input string) (*Patch, error) {
	if input == "" {
		return nil, fmt.Errorf("invalid patch: empty input")
	}

	lines := strings.Split(input, "\n")
	if len(lines) < 1 {
		return nil, fmt.Errorf("invalid patch: too short")
	}

	// Validate *** Begin Patch marker
	if strings.TrimRight(lines[0], " \t\r") != "*** Begin Patch" {
		return nil, fmt.Errorf("invalid patch: expected '*** Begin Patch' on first line, got %q", lines[0])
	}

	patch := &Patch{}
	i := 1 // skip "*** Begin Patch"

	for i < len(lines) {
		line := strings.TrimRight(lines[i], " \t\r")

		// Skip empty lines and the end marker
		if line == "" || line == "*** End Patch" {
			i++
			continue
		}

		if strings.HasPrefix(line, "*** Add File: ") {
			op, nextI := parseAddFile(lines, i)
			patch.Operations = append(patch.Operations, op)
			i = nextI

		} else if strings.HasPrefix(line, "*** Delete File: ") {
			path := strings.TrimPrefix(line, "*** Delete File: ")
			path = strings.TrimRight(path, " \t\r")
			patch.Operations = append(patch.Operations, PatchOperation{
				Type: PatchOpDelete,
				Path: path,
			})
			i++

		} else if strings.HasPrefix(line, "*** Update File: ") {
			op, nextI := parseUpdateFile(lines, i)
			patch.Operations = append(patch.Operations, op)
			i = nextI

		} else if strings.HasPrefix(line, "*** Move File: ") {
			op, err := parseMoveFile(line)
			if err != nil {
				return nil, err
			}
			patch.Operations = append(patch.Operations, op)
			i++

		} else {
			// Skip unrecognized lines
			i++
		}
	}

	return patch, nil
}

// parseAddFile parses an Add File operation starting at line index i.
func parseAddFile(lines []string, i int) (PatchOperation, int) {
	line := strings.TrimRight(lines[i], " \t\r")
	path := strings.TrimPrefix(line, "*** Add File: ")
	path = strings.TrimRight(path, " \t\r")
	i++

	var contentLines []string
	for i < len(lines) {
		l := lines[i]
		trimmed := strings.TrimRight(l, " \t\r")
		if strings.HasPrefix(trimmed, "*** ") {
			break
		}
		if strings.HasPrefix(l, "+") {
			contentLines = append(contentLines, l[1:])
		}
		i++
	}

	return PatchOperation{
		Type:    PatchOpAdd,
		Path:    path,
		Content: contentLines,
	}, i
}

// parseUpdateFile parses an Update File operation starting at line index i.
func parseUpdateFile(lines []string, i int) (PatchOperation, int) {
	line := strings.TrimRight(lines[i], " \t\r")
	path := strings.TrimPrefix(line, "*** Update File: ")
	path = strings.TrimRight(path, " \t\r")
	i++

	op := PatchOperation{
		Type: PatchOpUpdate,
		Path: path,
	}

	// Parse hunks until we hit another *** file marker or end
	for i < len(lines) {
		l := strings.TrimRight(lines[i], " \t\r")

		// Stop if we hit another file-level marker (but not End Patch or End of File)
		if isFileMarker(l) {
			break
		}

		if l == "*** End Patch" {
			break
		}

		// Check for context hint: @@@ ... @@@ or @@ ...
		if strings.HasPrefix(l, "@@@") || strings.HasPrefix(l, "@@") {
			hunk, nextI := parseHunk(lines, i)
			op.Hunks = append(op.Hunks, hunk)
			i = nextI
		} else if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "-") || strings.HasPrefix(l, "+") {
			// Hunk without a context hint -- start parsing directly
			hunk, nextI := parseHunkLines(lines, i, "")
			op.Hunks = append(op.Hunks, hunk)
			i = nextI
		} else if l == "*** End of File" {
			i++
			continue
		} else if l == "" {
			i++
			continue
		} else {
			i++
		}
	}

	return op, i
}

// parseHunk parses a hunk starting with a @@@ or @@ context hint marker.
func parseHunk(lines []string, i int) (Hunk, int) {
	line := strings.TrimRight(lines[i], " \t\r")
	contextHint := extractContextHint(line)
	i++
	return parseHunkLines(lines, i, contextHint)
}

// parseHunkLines parses the context/delete/add lines of a hunk.
func parseHunkLines(lines []string, i int, contextHint string) (Hunk, int) {
	hunk := Hunk{
		ContextHint: contextHint,
	}

	for i < len(lines) {
		l := lines[i]
		trimmed := strings.TrimRight(l, " \t\r")

		// Stop at another context hint, file marker, or end patch
		if strings.HasPrefix(trimmed, "@@@") || strings.HasPrefix(trimmed, "@@") {
			break
		}
		if isFileMarker(trimmed) || trimmed == "*** End Patch" {
			break
		}
		if trimmed == "*** End of File" {
			i++
			break
		}

		if len(l) == 0 {
			i++
			continue
		}

		prefix := l[0]
		rest := l[1:]
		switch prefix {
		case ' ':
			hunk.ContextLines = append(hunk.ContextLines, rest)
			hunk.MatchLines = append(hunk.MatchLines, rest)
			hunk.ReplaceLines = append(hunk.ReplaceLines, rest)
		case '-':
			hunk.DeleteLines = append(hunk.DeleteLines, rest)
			hunk.MatchLines = append(hunk.MatchLines, rest)
		case '+':
			hunk.AddLines = append(hunk.AddLines, rest)
			hunk.ReplaceLines = append(hunk.ReplaceLines, rest)
		default:
			// Treat unrecognized prefix lines as context
			hunk.ContextLines = append(hunk.ContextLines, l)
			hunk.MatchLines = append(hunk.MatchLines, l)
			hunk.ReplaceLines = append(hunk.ReplaceLines, l)
		}
		i++
	}

	return hunk, i
}

// extractContextHint extracts the text from @@@ ... @@@ or @@ ... markers.
func extractContextHint(line string) string {
	// Try @@@ ... @@@ first
	if strings.HasPrefix(line, "@@@") {
		hint := strings.TrimPrefix(line, "@@@")
		if idx := strings.Index(hint, "@@@"); idx >= 0 {
			hint = hint[:idx]
		}
		return strings.TrimSpace(hint)
	}
	// Fallback: @@ ...
	if strings.HasPrefix(line, "@@") {
		hint := strings.TrimPrefix(line, "@@")
		return strings.TrimSpace(hint)
	}
	return ""
}

// isFileMarker returns true if the line is a file-level *** marker (Add/Delete/Update/Move).
func isFileMarker(line string) bool {
	return strings.HasPrefix(line, "*** Add File:") ||
		strings.HasPrefix(line, "*** Delete File:") ||
		strings.HasPrefix(line, "*** Update File:") ||
		strings.HasPrefix(line, "*** Move File:")
}

// parseMoveFile parses a Move File line into a PatchOperation.
func parseMoveFile(line string) (PatchOperation, error) {
	rest := strings.TrimPrefix(line, "*** Move File: ")
	rest = strings.TrimRight(rest, " \t\r")
	parts := strings.SplitN(rest, " -> ", 2)
	if len(parts) != 2 {
		return PatchOperation{}, fmt.Errorf("invalid move syntax: expected 'old/path -> new/path', got %q (missing '->' separator)", rest)
	}
	return PatchOperation{
		Type:   PatchOpMove,
		Path:   strings.TrimSpace(parts[0]),
		MoveTo: strings.TrimSpace(parts[1]),
	}, nil
}

// ApplyPatch applies a parsed Patch to the filesystem via the ExecutionEnvironment.
func ApplyPatch(patch *Patch, env ExecutionEnvironment) (*PatchResult, error) {
	result := &PatchResult{}

	for _, op := range patch.Operations {
		switch op.Type {
		case PatchOpAdd:
			content := strings.Join(op.Content, "\n")
			if err := env.WriteFile(op.Path, content); err != nil {
				return nil, fmt.Errorf("add file %s: %w", op.Path, err)
			}
			result.FilesCreated++
			result.Details = append(result.Details, fmt.Sprintf("Added: %s", op.Path))

		case PatchOpDelete:
			// The ExecutionEnvironment interface does not have a Delete method,
			// so deletion is implemented by writing empty content.
			if err := env.WriteFile(op.Path, ""); err != nil {
				return nil, fmt.Errorf("delete file %s: %w", op.Path, err)
			}
			result.FilesDeleted++
			result.Details = append(result.Details, fmt.Sprintf("Deleted: %s", op.Path))

		case PatchOpUpdate:
			if err := applyUpdateOperation(op, env); err != nil {
				return nil, err
			}
			result.FilesModified++
			result.Details = append(result.Details, fmt.Sprintf("Updated: %s", op.Path))

		case PatchOpMove:
			if err := applyMoveOperation(op, env); err != nil {
				return nil, err
			}
			result.FilesMoved++
			result.Details = append(result.Details, fmt.Sprintf("Moved: %s -> %s", op.Path, op.MoveTo))

		default:
			return nil, fmt.Errorf("unknown operation type: %s", op.Type)
		}
	}

	result.Summary = strings.Join(result.Details, "\n")
	return result, nil
}

// lineNumberPattern matches the line-number prefix format from ReadFile (e.g. "   1\t").
var lineNumberPattern = regexp.MustCompile(`^\s*\d+\t`)

// stripLineNumbers removes line-number prefixes from ReadFile output.
// Each line in the format "  NN\t<content>" becomes just "<content>".
// If no line-number prefix is detected, the content is returned unchanged.
func stripLineNumbers(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}

	// Check if the first non-empty line has a line-number prefix
	hasPrefix := false
	for _, l := range lines {
		if l == "" {
			continue
		}
		if lineNumberPattern.MatchString(l) {
			hasPrefix = true
		}
		break
	}

	if !hasPrefix {
		return content
	}

	stripped := make([]string, 0, len(lines))
	for _, l := range lines {
		if l == "" {
			stripped = append(stripped, "")
			continue
		}
		loc := lineNumberPattern.FindStringIndex(l)
		if loc != nil {
			stripped = append(stripped, l[loc[1]:])
		} else {
			stripped = append(stripped, l)
		}
	}

	return strings.Join(stripped, "\n")
}

// applyUpdateOperation reads a file, applies all hunks, and writes the result back.
func applyUpdateOperation(op PatchOperation, env ExecutionEnvironment) error {
	content, err := env.ReadFile(op.Path, 0, 0)
	if err != nil {
		return fmt.Errorf("read file for update %s: %w", op.Path, err)
	}

	content = stripLineNumbers(content)
	fileLines := strings.Split(content, "\n")

	for _, hunk := range op.Hunks {
		fileLines = applyPatchHunk(fileLines, hunk)
	}

	newContent := strings.Join(fileLines, "\n")
	if err := env.WriteFile(op.Path, newContent); err != nil {
		return fmt.Errorf("write updated file %s: %w", op.Path, err)
	}
	return nil
}

// applyPatchHunk applies a single hunk by finding the match lines (context + delete)
// in the file and replacing them with the replace lines (context + add).
// The interleaved order is preserved via MatchLines/ReplaceLines.
func applyPatchHunk(fileLines []string, hunk Hunk) []string {
	if len(hunk.MatchLines) == 0 {
		// No match lines -- append added lines at the end
		return append(fileLines, hunk.AddLines...)
	}

	// Find the match sequence in the file (exact match with trailing whitespace tolerance)
	matchIdx := findSequence(fileLines, hunk.MatchLines)
	if matchIdx < 0 {
		// Fuzzy match: try trimmed comparison
		matchIdx = findSequenceFuzzy(fileLines, hunk.MatchLines)
	}

	if matchIdx < 0 {
		// Could not find the hunk location; append added lines at the end as fallback
		return append(fileLines, hunk.AddLines...)
	}

	// Replace matched region with the replacement lines
	var result []string
	result = append(result, fileLines[:matchIdx]...)
	result = append(result, hunk.ReplaceLines...)
	result = append(result, fileLines[matchIdx+len(hunk.MatchLines):]...)

	return result
}

// applyMoveOperation reads the source file, writes it to the destination, and empties the source.
func applyMoveOperation(op PatchOperation, env ExecutionEnvironment) error {
	content, err := env.ReadFile(op.Path, 0, 0)
	if err != nil {
		return fmt.Errorf("read file for move %s: %w", op.Path, err)
	}

	content = stripLineNumbers(content)

	if err := env.WriteFile(op.MoveTo, content); err != nil {
		return fmt.Errorf("write moved file %s: %w", op.MoveTo, err)
	}

	// Clear the source file (no Delete method on ExecutionEnvironment)
	if err := env.WriteFile(op.Path, ""); err != nil {
		return fmt.Errorf("clear source file after move %s: %w", op.Path, err)
	}

	return nil
}

// findSequence finds the starting index of a sequence of lines within fileLines.
// Trailing whitespace on each line is ignored during comparison.
// Returns -1 if not found.
func findSequence(fileLines, seq []string) int {
	if len(seq) == 0 || len(fileLines) < len(seq) {
		return -1
	}
	for i := 0; i <= len(fileLines)-len(seq); i++ {
		match := true
		for j := 0; j < len(seq); j++ {
			if strings.TrimRight(fileLines[i+j], " \t") != strings.TrimRight(seq[j], " \t") {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// findSequenceFuzzy performs a fuzzy match by trimming all whitespace from both sides.
// This handles cases where indentation differs between the patch and the file.
// Returns -1 if not found.
func findSequenceFuzzy(fileLines, seq []string) int {
	if len(seq) == 0 || len(fileLines) < len(seq) {
		return -1
	}
	for i := 0; i <= len(fileLines)-len(seq); i++ {
		match := true
		for j := 0; j < len(seq); j++ {
			if strings.TrimSpace(fileLines[i+j]) != strings.TrimSpace(seq[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
