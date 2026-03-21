package packer

import "testing"

func TestParseMentionedFiles(t *testing.T) {
	raw := "```json\n{\"paths\":[\"cmd/pack.go\"],\"reasons\":{\"cmd/pack.go\":\"入口文件\"}}\n```"
	result, err := ParseMentionedFiles(raw)
	if err != nil {
		t.Fatalf("ParseMentionedFiles failed: %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "cmd/pack.go" {
		t.Fatalf("unexpected paths: %#v", result.Paths)
	}
	if result.Reasons["cmd/pack.go"] != "入口文件" {
		t.Fatalf("unexpected reason: %v", result.Reasons)
	}
}
