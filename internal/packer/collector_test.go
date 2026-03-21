package packer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadIdentifiedFileContentTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	content := make([]byte, maxIdentifiedFileBytes+10)
	for i := range content {
		content[i] = 'a'
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	consumed := 0
	read, truncated, err := readIdentifiedFileContent(dir, "sample.txt", &consumed)
	if err != nil {
		t.Fatalf("readIdentifiedFileContent: %v", err)
	}
	if !truncated {
		t.Fatalf("expected truncated=true")
	}
	if len(read) == 0 {
		t.Fatalf("expected content")
	}
}

func TestValidateAndCleanMentionedFilesFiltersMissing(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "exists.go")
	if err := os.WriteFile(filePath, []byte("package main"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mentioned := &MentionedFiles{
		Paths: []string{"exists.go", "missing.go"},
		Reasons: map[string]string{
			"exists.go": "exists",
			"missing.go": "missing",
		},
	}
	result, err := validateAndCleanMentionedFiles(mentioned, []string{"exists.go", "missing.go"}, dir)
	if err != nil {
		t.Fatalf("validateAndCleanMentionedFiles: %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "exists.go" {
		t.Fatalf("unexpected paths: %#v", result.Paths)
	}
	if result.Reasons["exists.go"] != "exists" {
		t.Fatalf("unexpected reason: %q", result.Reasons["exists.go"])
	}
}

func TestValidateAndCleanMentionedFilesUsesDefaultReason(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "exists.go")
	if err := os.WriteFile(filePath, []byte("package main"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mentioned := &MentionedFiles{
		Paths:   []string{"exists.go"},
		Reasons: map[string]string{},
	}
	result, err := validateAndCleanMentionedFiles(mentioned, []string{"exists.go"}, dir)
	if err != nil {
		t.Fatalf("validateAndCleanMentionedFiles: %v", err)
	}
	if result.Reasons["exists.go"] != defaultIdentifiedReason {
		t.Fatalf("unexpected default reason: %q", result.Reasons["exists.go"])
	}
}
