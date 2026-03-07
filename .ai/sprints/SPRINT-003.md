# SPRINT-003: Fix Non-Atomic Checkpoint Writes

## Goal
Fix the checkpoint Save method to use atomic file writes, preventing corrupted checkpoint files if the process crashes mid-write.

## Background
The auto-checkpoint feature (`engine.go`) repeatedly overwrites the same checkpoint file during pipeline execution. If the process is killed or crashes while `os.WriteFile` is mid-write, the checkpoint file will be partially written and corrupted. On resume, `LoadCheckpoint` would fail with a JSON parse error, making the pipeline unresumable — defeating the purpose of checkpointing.

## Root Cause
File: `attractor/checkpoint.go`

`Save()` calls `os.WriteFile(path, data, 0644)` directly, which is not atomic. A crash during write leaves a partially-written file.

## Requirements
1. Write checkpoint data to a temporary file in the same directory first
2. Use `os.Rename` to atomically replace the target file (atomic on POSIX)
3. Clean up the temp file on write errors
4. Tests should verify the atomic write behavior

## Key Files
- `attractor/checkpoint.go` — The fix goes here, in `Save()`
- `attractor/checkpoint_test.go` — Add tests to verify atomic write behavior

## Definition of Done
- [x] `Save()` writes to a temp file then renames atomically
- [x] Temp file is cleaned up on write errors
- [x] Tests verify correct save/load round-trip
- [x] Tests verify temp file cleanup on error
- [x] `go test ./attractor/...` passes
- [x] `go vet ./...` passes
