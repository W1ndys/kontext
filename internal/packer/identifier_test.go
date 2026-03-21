package packer

import (
	"fmt"
	"testing"
)

func TestMergeMentionedFilesPreservesOrderAndReasons(t *testing.T) {
	merged := mergeMentionedFiles(
		[]*MentionedFiles{
			{
				Paths: []string{"a.go", "b.go"},
				Reasons: map[string]string{
					"a.go": "reason a",
					"b.go": "reason b",
				},
			},
			{
				Paths: []string{"b.go", "c.go"},
				Reasons: map[string]string{
					"b.go": "ignored duplicate",
					"c.go": "reason c",
				},
			},
		},
	)

	if len(merged.Paths) != 3 {
		t.Fatalf("unexpected path count: %d", len(merged.Paths))
	}
	if merged.Paths[0] != "a.go" || merged.Paths[1] != "b.go" || merged.Paths[2] != "c.go" {
		t.Fatalf("unexpected order: %#v", merged.Paths)
	}
	if merged.Reasons["b.go"] != "reason b" {
		t.Fatalf("unexpected preserved reason: %q", merged.Reasons["b.go"])
	}
}

func TestMergeMentionedFilesCapsAtMaxIdentifiedFiles(t *testing.T) {
	batch := &MentionedFiles{Reasons: make(map[string]string)}
	for i := 0; i < maxIdentifiedFiles+5; i++ {
		path := fmt.Sprintf("file-%d.go", i)
		batch.Paths = append(batch.Paths, path)
		batch.Reasons[path] = "reason"
	}

	merged := mergeMentionedFiles([]*MentionedFiles{batch})
	if len(merged.Paths) != maxIdentifiedFiles {
		t.Fatalf("unexpected capped count: %d", len(merged.Paths))
	}
}
