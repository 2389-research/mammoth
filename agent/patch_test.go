// ABOUTME: Tests for the v4a patch format parser and applier.
// ABOUTME: Covers parsing, applying, fuzzy matching, move operations, and error cases using real temp directories.

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ParsePatch tests ---

func TestParsePatchAddFile(t *testing.T) {
	input := `*** Begin Patch
*** Add File: src/hello.go
+package main
+
+func main() {
+	println("hello")
+}
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}

	op := patch.Operations[0]
	if op.Type != PatchOpAdd {
		t.Errorf("expected operation type %q, got %q", PatchOpAdd, op.Type)
	}
	if op.Path != "src/hello.go" {
		t.Errorf("expected path 'src/hello.go', got %q", op.Path)
	}
	if len(op.Content) != 5 {
		t.Errorf("expected 5 content lines, got %d", len(op.Content))
	}
	if op.Content[0] != "package main" {
		t.Errorf("expected first content line 'package main', got %q", op.Content[0])
	}
}

func TestParsePatchDeleteFile(t *testing.T) {
	input := `*** Begin Patch
*** Delete File: old/legacy.py
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}

	op := patch.Operations[0]
	if op.Type != PatchOpDelete {
		t.Errorf("expected operation type %q, got %q", PatchOpDelete, op.Type)
	}
	if op.Path != "old/legacy.py" {
		t.Errorf("expected path 'old/legacy.py', got %q", op.Path)
	}
}

func TestParsePatchUpdateFile(t *testing.T) {
	input := `*** Begin Patch
*** Update File: src/main.go
@@@ func main() @@@
 func main() {
-	println("hello")
+	println("goodbye")
 }
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}

	op := patch.Operations[0]
	if op.Type != PatchOpUpdate {
		t.Errorf("expected operation type %q, got %q", PatchOpUpdate, op.Type)
	}
	if op.Path != "src/main.go" {
		t.Errorf("expected path 'src/main.go', got %q", op.Path)
	}
	if len(op.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(op.Hunks))
	}

	hunk := op.Hunks[0]
	if hunk.ContextHint != "func main()" {
		t.Errorf("expected context hint 'func main()', got %q", hunk.ContextHint)
	}
	if len(hunk.ContextLines) != 2 {
		t.Errorf("expected 2 context lines, got %d: %v", len(hunk.ContextLines), hunk.ContextLines)
	}
	if len(hunk.DeleteLines) != 1 {
		t.Errorf("expected 1 delete line, got %d", len(hunk.DeleteLines))
	}
	if len(hunk.AddLines) != 1 {
		t.Errorf("expected 1 add line, got %d", len(hunk.AddLines))
	}
}

func TestParsePatchMoveFile(t *testing.T) {
	input := `*** Begin Patch
*** Move File: old/path.go -> new/path.go
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}

	op := patch.Operations[0]
	if op.Type != PatchOpMove {
		t.Errorf("expected operation type %q, got %q", PatchOpMove, op.Type)
	}
	if op.Path != "old/path.go" {
		t.Errorf("expected path 'old/path.go', got %q", op.Path)
	}
	if op.MoveTo != "new/path.go" {
		t.Errorf("expected move_to 'new/path.go', got %q", op.MoveTo)
	}
}

func TestParsePatchMultiFile(t *testing.T) {
	input := `*** Begin Patch
*** Add File: new.txt
+hello
*** Delete File: old.txt
*** Update File: existing.txt
@@@ some context @@@
 existing line
-remove me
+add me
*** Move File: a.txt -> b.txt
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 4 {
		t.Fatalf("expected 4 operations, got %d", len(patch.Operations))
	}

	if patch.Operations[0].Type != PatchOpAdd {
		t.Errorf("expected first op to be Add, got %q", patch.Operations[0].Type)
	}
	if patch.Operations[1].Type != PatchOpDelete {
		t.Errorf("expected second op to be Delete, got %q", patch.Operations[1].Type)
	}
	if patch.Operations[2].Type != PatchOpUpdate {
		t.Errorf("expected third op to be Update, got %q", patch.Operations[2].Type)
	}
	if patch.Operations[3].Type != PatchOpMove {
		t.Errorf("expected fourth op to be Move, got %q", patch.Operations[3].Type)
	}
}

func TestParsePatchUpdateWithMultipleHunks(t *testing.T) {
	input := `*** Begin Patch
*** Update File: src/main.go
@@@ func foo @@@
 func foo() {
-	return 1
+	return 2
 }
@@@ func bar @@@
 func bar() {
-	return 3
+	return 4
 }
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}

	op := patch.Operations[0]
	if len(op.Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(op.Hunks))
	}

	if op.Hunks[0].ContextHint != "func foo" {
		t.Errorf("expected first hunk context hint 'func foo', got %q", op.Hunks[0].ContextHint)
	}
	if op.Hunks[1].ContextHint != "func bar" {
		t.Errorf("expected second hunk context hint 'func bar', got %q", op.Hunks[1].ContextHint)
	}
}

func TestParsePatchUpdateWithoutContextHint(t *testing.T) {
	input := `*** Begin Patch
*** Update File: src/main.go
 func main() {
-	println("hello")
+	println("goodbye")
 }
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}

	op := patch.Operations[0]
	if len(op.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(op.Hunks))
	}

	hunk := op.Hunks[0]
	if hunk.ContextHint != "" {
		t.Errorf("expected empty context hint, got %q", hunk.ContextHint)
	}
}

func TestParsePatchTrailingWhitespace(t *testing.T) {
	input := "*** Begin Patch  \n*** Add File: test.txt  \n+hello  \n*** End Patch  "

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}
	if patch.Operations[0].Type != PatchOpAdd {
		t.Errorf("expected Add operation, got %q", patch.Operations[0].Type)
	}
}

// --- ParsePatch error cases ---

func TestParsePatchMalformedNoBegin(t *testing.T) {
	input := `*** Add File: test.txt
+hello
*** End Patch`

	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for patch without *** Begin Patch, got nil")
	}
	if !strings.Contains(err.Error(), "Begin Patch") {
		t.Errorf("expected error mentioning 'Begin Patch', got: %v", err)
	}
}

func TestParsePatchEmpty(t *testing.T) {
	_, err := ParsePatch("")
	if err == nil {
		t.Fatal("expected error for empty patch, got nil")
	}
}

func TestParsePatchMalformedMoveArrow(t *testing.T) {
	input := `*** Begin Patch
*** Move File: old.txt
*** End Patch`

	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for move without -> arrow, got nil")
	}
	if !strings.Contains(err.Error(), "->") {
		t.Errorf("expected error mentioning '->', got: %v", err)
	}
}

// --- ApplyPatch tests with real temp directories ---

func setupTempDir(t *testing.T) (string, ExecutionEnvironment) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "patch_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	env := NewLocalExecutionEnvironment(tmpDir)
	if err := env.Initialize(); err != nil {
		t.Fatalf("failed to initialize env: %v", err)
	}
	return tmpDir, env
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("failed to create parent dirs for %s: %v", name, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", name, err)
	}
}

func readTestFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("failed to read file %s: %v", name, err)
	}
	return string(data)
}

func fileExists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func TestApplyPatchAddCreatesFileWithParentDirs(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	patch := &Patch{
		Operations: []PatchOperation{
			{
				Type: PatchOpAdd,
				Path: filepath.Join(tmpDir, "deep/nested/dir/new.go"),
				Content: []string{
					"package main",
					"",
					"func main() {}",
				},
			},
		},
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if !strings.Contains(result.Summary, "Added") {
		t.Errorf("expected summary to contain 'Added', got %q", result.Summary)
	}

	content := readTestFile(t, tmpDir, "deep/nested/dir/new.go")
	if !strings.Contains(content, "package main") {
		t.Errorf("expected file content to contain 'package main', got %q", content)
	}
	if !strings.Contains(content, "func main() {}") {
		t.Errorf("expected file content to contain 'func main() {}', got %q", content)
	}
}

func TestApplyPatchDeleteRemovesFile(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	writeTestFile(t, tmpDir, "to_delete.txt", "this should be removed")

	patch := &Patch{
		Operations: []PatchOperation{
			{
				Type: PatchOpDelete,
				Path: filepath.Join(tmpDir, "to_delete.txt"),
			},
		},
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if !strings.Contains(result.Summary, "Deleted") {
		t.Errorf("expected summary to contain 'Deleted', got %q", result.Summary)
	}

	// File should no longer have real content (env.WriteFile writes empty string for delete)
	content := readTestFile(t, tmpDir, "to_delete.txt")
	if content != "" {
		t.Errorf("expected file to be empty after deletion, got %q", content)
	}
}

func TestApplyPatchUpdateReplacesLines(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	original := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n\treturn\n}\n"
	writeTestFile(t, tmpDir, "main.go", original)

	patch := &Patch{
		Operations: []PatchOperation{
			{
				Type: PatchOpUpdate,
				Path: filepath.Join(tmpDir, "main.go"),
				Hunks: []Hunk{
					{
						ContextHint:  "func main()",
						ContextLines: []string{"func main() {", "\tprintln(\"hello\")"},
						DeleteLines:  []string{"\treturn"},
						AddLines:     []string{"\tprintln(\"world\")", "\treturn"},
					},
				},
			},
		},
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if !strings.Contains(result.Summary, "Updated") {
		t.Errorf("expected summary to contain 'Updated', got %q", result.Summary)
	}

	content := readTestFile(t, tmpDir, "main.go")
	if !strings.Contains(content, "println(\"world\")") {
		t.Errorf("expected updated content to contain println(\"world\"), got:\n%s", content)
	}
	if !strings.Contains(content, "println(\"hello\")") {
		t.Errorf("expected context line println(\"hello\") preserved, got:\n%s", content)
	}
}

func TestApplyPatchMoveRenamesFile(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	writeTestFile(t, tmpDir, "old_name.go", "package old\n")

	patch := &Patch{
		Operations: []PatchOperation{
			{
				Type:   PatchOpMove,
				Path:   filepath.Join(tmpDir, "old_name.go"),
				MoveTo: filepath.Join(tmpDir, "subdir/new_name.go"),
			},
		},
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if !strings.Contains(result.Summary, "Moved") {
		t.Errorf("expected summary to contain 'Moved', got %q", result.Summary)
	}

	// Source should be gone (empty content)
	srcContent := readTestFile(t, tmpDir, "old_name.go")
	if srcContent != "" {
		t.Errorf("expected source file to be empty after move, got %q", srcContent)
	}

	// Destination should have the content
	if !fileExists(t, tmpDir, "subdir/new_name.go") {
		t.Fatal("expected destination file to exist after move")
	}
	dstContent := readTestFile(t, tmpDir, "subdir/new_name.go")
	if dstContent != "package old\n" {
		t.Errorf("expected destination content 'package old\\n', got %q", dstContent)
	}
}

func TestApplyPatchFuzzyMatchWhitespace(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	// File has tabs, but patch uses spaces
	original := "func main() {\n\tprintln(\"hello\")\n\treturn 0\n}\n"
	writeTestFile(t, tmpDir, "fuzzy.go", original)

	patch := &Patch{
		Operations: []PatchOperation{
			{
				Type: PatchOpUpdate,
				Path: filepath.Join(tmpDir, "fuzzy.go"),
				Hunks: []Hunk{
					{
						ContextLines: []string{"func main() {", "  println(\"hello\")"},
						DeleteLines:  []string{"  return 0"},
						AddLines:     []string{"  return 1"},
					},
				},
			},
		},
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if result.FilesModified != 1 {
		t.Errorf("expected 1 file modified, got %d", result.FilesModified)
	}

	content := readTestFile(t, tmpDir, "fuzzy.go")
	if !strings.Contains(content, "return 1") {
		t.Errorf("expected fuzzy-matched update to contain 'return 1', got:\n%s", content)
	}
}

func TestApplyPatchUpdateMissingFile(t *testing.T) {
	_, env := setupTempDir(t)

	patch := &Patch{
		Operations: []PatchOperation{
			{
				Type: PatchOpUpdate,
				Path: "/nonexistent/file.go",
				Hunks: []Hunk{
					{
						ContextLines: []string{"some context"},
						DeleteLines:  []string{"old"},
						AddLines:     []string{"new"},
					},
				},
			},
		},
	}

	_, err := ApplyPatch(patch, env)
	if err == nil {
		t.Fatal("expected error for update on nonexistent file, got nil")
	}
}

func TestApplyPatchUpdateNoContextMatch(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	writeTestFile(t, tmpDir, "noctx.go", "func main() {\n\treturn 0\n}\n")

	patch := &Patch{
		Operations: []PatchOperation{
			{
				Type: PatchOpUpdate,
				Path: filepath.Join(tmpDir, "noctx.go"),
				Hunks: []Hunk{
					{
						ContextLines: []string{"this context does not exist anywhere"},
						DeleteLines:  []string{"also not present"},
						AddLines:     []string{"replacement"},
					},
				},
			},
		},
	}

	// The hunk should fail to find a match, which results in a fallback append
	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	// Even with no match, it should still report as updated (fallback append behavior)
	if result.FilesModified != 1 {
		t.Errorf("expected 1 file modified (fallback append), got %d", result.FilesModified)
	}
}

// --- End-to-end: parse + apply ---

func TestParseThenApplyUpdate(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	filePath := filepath.Join(tmpDir, "main.go")
	writeTestFile(t, tmpDir, "main.go", original)

	input := "*** Begin Patch\n" +
		"*** Update File: " + filePath + "\n" +
		"@@@ func main() @@@\n" +
		" func main() {\n" +
		"-\tfmt.Println(\"hello\")\n" +
		"+\tfmt.Println(\"goodbye\")\n" +
		" }\n" +
		"*** End Patch"

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if result.FilesModified != 1 {
		t.Errorf("expected 1 file modified, got %d", result.FilesModified)
	}

	content := readTestFile(t, tmpDir, "main.go")
	if !strings.Contains(content, "goodbye") {
		t.Errorf("expected content to contain 'goodbye', got:\n%s", content)
	}
	if strings.Contains(content, "hello") {
		t.Errorf("expected 'hello' to be removed, still present in:\n%s", content)
	}
}

func TestParseThenApplyAdd(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	filePath := filepath.Join(tmpDir, "brand_new.go")
	input := "*** Begin Patch\n" +
		"*** Add File: " + filePath + "\n" +
		"+package brand\n" +
		"+\n" +
		"+func New() {}\n" +
		"*** End Patch"

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if result.FilesCreated != 1 {
		t.Errorf("expected 1 file created, got %d", result.FilesCreated)
	}

	content := readTestFile(t, tmpDir, "brand_new.go")
	if !strings.Contains(content, "package brand") {
		t.Errorf("expected content to contain 'package brand', got:\n%s", content)
	}
}

func TestParseThenApplyDelete(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	writeTestFile(t, tmpDir, "removeme.txt", "should be gone")
	filePath := filepath.Join(tmpDir, "removeme.txt")

	input := "*** Begin Patch\n" +
		"*** Delete File: " + filePath + "\n" +
		"*** End Patch"

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if result.FilesDeleted != 1 {
		t.Errorf("expected 1 file deleted, got %d", result.FilesDeleted)
	}
}

func TestParseThenApplyMove(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	writeTestFile(t, tmpDir, "src.txt", "moving this")

	srcPath := filepath.Join(tmpDir, "src.txt")
	dstPath := filepath.Join(tmpDir, "dst/moved.txt")

	input := "*** Begin Patch\n" +
		"*** Move File: " + srcPath + " -> " + dstPath + "\n" +
		"*** End Patch"

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if result.FilesMoved != 1 {
		t.Errorf("expected 1 file moved, got %d", result.FilesMoved)
	}

	dstContent := readTestFile(t, tmpDir, "dst/moved.txt")
	// ReadFile adds a trailing newline, so the round-tripped content includes it
	if !strings.Contains(dstContent, "moving this") {
		t.Errorf("expected destination content to contain 'moving this', got %q", dstContent)
	}
}

func TestParseThenApplyMultiFile(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	writeTestFile(t, tmpDir, "update_me.txt", "line1\nline2\nline3\n")
	writeTestFile(t, tmpDir, "delete_me.txt", "bye")

	updatePath := filepath.Join(tmpDir, "update_me.txt")
	deletePath := filepath.Join(tmpDir, "delete_me.txt")
	addPath := filepath.Join(tmpDir, "added.txt")

	input := "*** Begin Patch\n" +
		"*** Update File: " + updatePath + "\n" +
		" line1\n" +
		"-line2\n" +
		"+line2_modified\n" +
		" line3\n" +
		"*** Delete File: " + deletePath + "\n" +
		"*** Add File: " + addPath + "\n" +
		"+brand new content\n" +
		"*** End Patch"

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(patch.Operations))
	}

	result, err := ApplyPatch(patch, env)
	if err != nil {
		t.Fatalf("ApplyPatch returned error: %v", err)
	}

	if result.FilesModified != 1 {
		t.Errorf("expected 1 modified, got %d", result.FilesModified)
	}
	if result.FilesDeleted != 1 {
		t.Errorf("expected 1 deleted, got %d", result.FilesDeleted)
	}
	if result.FilesCreated != 1 {
		t.Errorf("expected 1 created, got %d", result.FilesCreated)
	}

	updatedContent := readTestFile(t, tmpDir, "update_me.txt")
	if !strings.Contains(updatedContent, "line2_modified") {
		t.Errorf("expected updated file to contain 'line2_modified', got:\n%s", updatedContent)
	}

	addedContent := readTestFile(t, tmpDir, "added.txt")
	if !strings.Contains(addedContent, "brand new content") {
		t.Errorf("expected added file to contain 'brand new content', got:\n%s", addedContent)
	}
}

// --- Tool integration test ---

func TestApplyPatchToolRegisteredInCoreTools(t *testing.T) {
	registry := NewToolRegistry()
	RegisterCoreTools(registry)

	if !registry.Has("apply_patch") {
		t.Error("expected apply_patch to be registered in core tools")
	}

	tool := registry.Get("apply_patch")
	if tool == nil {
		t.Fatal("expected non-nil tool for apply_patch")
	}
	if tool.Execute == nil {
		t.Fatal("expected non-nil Execute function for apply_patch")
	}
	if tool.Definition.Description == "" {
		t.Error("expected non-empty description for apply_patch tool")
	}
}

func TestApplyPatchToolExecute(t *testing.T) {
	tmpDir, env := setupTempDir(t)

	writeTestFile(t, tmpDir, "tool_test.txt", "original content\nline two\n")

	filePath := filepath.Join(tmpDir, "tool_test.txt")
	patchStr := "*** Begin Patch\n" +
		"*** Update File: " + filePath + "\n" +
		" original content\n" +
		"-line two\n" +
		"+line two modified\n" +
		"*** End Patch"

	tool := NewApplyPatchTool()
	result, err := tool.Execute(map[string]any{
		"patch": patchStr,
	}, env)
	if err != nil {
		t.Fatalf("tool Execute returned error: %v", err)
	}

	if !strings.Contains(result, "Updated") {
		t.Errorf("expected result to mention update, got: %s", result)
	}

	content := readTestFile(t, tmpDir, "tool_test.txt")
	if !strings.Contains(content, "line two modified") {
		t.Errorf("expected file to contain 'line two modified', got:\n%s", content)
	}
}

// --- Compatibility test: @@ single-at context hints (backwards compat) ---

func TestParsePatchDoubleAtContextHint(t *testing.T) {
	input := `*** Begin Patch
*** Update File: src/main.go
@@ context_hint
 func main() {
-	println("hello")
+	println("goodbye")
 }
*** End Patch`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("ParsePatch returned error: %v", err)
	}

	if len(patch.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(patch.Operations))
	}

	hunk := patch.Operations[0].Hunks[0]
	if hunk.ContextHint != "context_hint" {
		t.Errorf("expected context hint 'context_hint', got %q", hunk.ContextHint)
	}
}
