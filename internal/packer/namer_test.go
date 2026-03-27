package packer

import (
	"errors"
	"testing"

	"github.com/w1ndys/kontext/internal/llm"
)

func TestGenerateFilenameSuggestionUsesStructuredOutput(t *testing.T) {
	client := &stubLLMClient{
		chatStructuredFn: func(req *llm.ChatRequest, schemaName string, out any) (*llm.ChatResponse, error) {
			if schemaName != "pack_filename_suggestion" {
				t.Fatalf("unexpected schema name: %q", schemaName)
			}
			if len(req.Messages) != 2 {
				t.Fatalf("unexpected message count: %d", len(req.Messages))
			}

			suggestion, ok := out.(*filenameSuggestion)
			if !ok {
				t.Fatalf("unexpected output type: %T", out)
			}
			suggestion.Title = "\n“pack 命令 Prompt 命名”\n"
			return &llm.ChatResponse{Content: `{"title":"pack 命令 Prompt 命名"}`}, nil
		},
	}

	got, err := GenerateFilenameSuggestion(client, "给 pack 命令生成 markdown prompt", nil)
	if err != nil {
		t.Fatalf("GenerateFilenameSuggestion returned error: %v", err)
	}
	if got != "pack 命令 Prompt 命名" {
		t.Fatalf("GenerateFilenameSuggestion() = %q", got)
	}
}

func TestGenerateFilenameSuggestionRejectsEmptyTitle(t *testing.T) {
	client := &stubLLMClient{
		chatStructuredFn: func(req *llm.ChatRequest, schemaName string, out any) (*llm.ChatResponse, error) {
			suggestion := out.(*filenameSuggestion)
			suggestion.Title = " \n "
			return &llm.ChatResponse{Content: `{"title":""}`}, nil
		},
	}

	_, err := GenerateFilenameSuggestion(client, "实现 pack 命令", nil)
	if err == nil {
		t.Fatalf("expected error for empty title")
	}
}

func TestNormalizeFilenameSuggestionTruncatesLongTitles(t *testing.T) {
	got := normalizeFilenameSuggestion("这是一个非常非常非常非常非常长的文件名标题建议，用来测试截断行为")
	if got == "" {
		t.Fatalf("normalizeFilenameSuggestion returned empty string")
	}
	if len([]rune(got)) > maxFilenameSuggestionRunes {
		t.Fatalf("normalized title too long: %d", len([]rune(got)))
	}
}

type stubLLMClient struct {
	chatStructuredFn func(req *llm.ChatRequest, schemaName string, out any) (*llm.ChatResponse, error)
}

func (s *stubLLMClient) Generate(req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLLMClient) Chat(req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLLMClient) ChatStream(req *llm.ChatRequest, onChunk func(string) error) (*llm.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLLMClient) ChatStructured(req *llm.ChatRequest, schemaName string, out any) (*llm.ChatResponse, error) {
	if s.chatStructuredFn == nil {
		return nil, errors.New("not implemented")
	}
	return s.chatStructuredFn(req, schemaName, out)
}

func (s *stubLLMClient) ListModels() ([]string, error) {
	return nil, errors.New("not implemented")
}
