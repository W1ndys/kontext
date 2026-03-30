package generator

import (
	"encoding/json"
	"regexp"
	"strings"
)

// stripCodeBlock 去除 LLM 返回内容中可能包裹的 markdown 代码块标记。
// 优先匹配 json 代码块，也支持 yaml、yml、无语言标记的 ``` 等。
func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	re := regexp.MustCompile("(?s)^```(?:json|ya?ml)?\\s*\n?(.*?)\\s*```$")
	if m := re.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

// ParseInterviewResponse 解析对话阶段 LLM 的 JSON 响应。
// 如果 JSON 解析失败，将原文降级为纯文本问题。
func ParseInterviewResponse(raw string) (*InterviewResponse, error) {
	cleaned := stripCodeBlock(raw)

	var resp InterviewResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		// 降级：将原文当作纯文本问题
		return &InterviewResponse{
			Type:     "question",
			Question: raw,
			Options:  []string{"继续", "其他（请说明）"},
		}, nil
	}

	return &resp, nil
}

// ParseGeneratedJSON 解析生成阶段 LLM 的 JSON 响应。
func ParseGeneratedJSON(raw string) (*GeneratedContent, error) {
	cleaned := stripCodeBlock(raw)

	var result GeneratedContent
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseAnalyzedFiles 解析文件识别阶段 LLM 的 JSON 响应。
func ParseAnalyzedFiles(raw string) (*AnalyzedFiles, error) {
	cleaned := stripCodeBlock(raw)

	var result AnalyzedFiles
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseSelectedFiles 解析重点文件选择阶段 LLM 的 JSON 响应。
func ParseSelectedFiles(raw string) (*SelectedFiles, error) {
	cleaned := stripCodeBlock(raw)

	var result SelectedFiles
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseSingleFileJSON 解析分步生成单个文件的 JSON 响应。
func ParseSingleFileJSON(raw string) (*SingleFileContent, error) {
	cleaned := stripCodeBlock(raw)

	var result SingleFileContent
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseModuleContractJSON 解析单个模块契约生成的 JSON 响应。
func ParseModuleContractJSON(raw string) (*ModuleContractContent, error) {
	cleaned := stripCodeBlock(raw)

	var result ModuleContractContent
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}
