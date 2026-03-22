package promptdoc

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateFilenameAtUsesTitleFirst(t *testing.T) {
	got := generateFilenameAt(time.Date(2026, 3, 22, 21, 41, 28, 0, time.UTC), "实现 pack 命令", "Task From File")
	want := "20260322214128-task-from-file.md"
	if got != want {
		t.Fatalf("generateFilenameAt() = %q, want %q", got, want)
	}
}

func TestGenerateFilenameAtFallsBackToTask(t *testing.T) {
	got := generateFilenameAt(time.Date(2026, 3, 22, 21, 41, 28, 0, time.UTC), "给 pack 命令生成 Markdown prompt", "")
	want := "20260322214128-给-pack-命令生成-markdown-prompt.md"
	if got != want {
		t.Fatalf("generateFilenameAt() = %q, want %q", got, want)
	}
}

func TestGenerateFilenameAtFallsBackToPromptWhenSanitizedEmpty(t *testing.T) {
	got := generateFilenameAt(time.Date(2026, 3, 22, 21, 41, 28, 0, time.UTC), "???///***", "")
	want := "20260322214128-prompt.md"
	if got != want {
		t.Fatalf("generateFilenameAt() = %q, want %q", got, want)
	}
}

func TestGenerateFilenameAtTruncatesLongBase(t *testing.T) {
	got := generateFilenameAt(time.Date(2026, 3, 22, 21, 41, 28, 0, time.UTC), strings.Repeat("超长标题", 20), "")
	if !strings.HasPrefix(got, "20260322214128-") {
		t.Fatalf("unexpected prefix: %q", got)
	}
	if !strings.HasSuffix(got, ".md") {
		t.Fatalf("unexpected suffix: %q", got)
	}

	base := strings.TrimSuffix(strings.TrimPrefix(got, "20260322214128-"), ".md")
	if len([]rune(base)) > maxFilenameBaseRunes {
		t.Fatalf("base too long: %d", len([]rune(base)))
	}
}
