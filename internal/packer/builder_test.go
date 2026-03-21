package packer

import "testing"

func TestMarkdownFenceLanguage(t *testing.T) {
	cases := map[string]string{
		"main.go":     "go",
		"script.py":   "python",
		"index.ts":    "typescript",
		"view.tsx":    "tsx",
		"worker.js":   "javascript",
		"unknown.txt": "",
	}

	for path, want := range cases {
		if got := markdownFenceLanguage(path); got != want {
			t.Fatalf("markdownFenceLanguage(%q) = %q, want %q", path, got, want)
		}
	}
}
