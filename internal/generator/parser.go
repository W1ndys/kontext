package generator

import (
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

// stripCodeBlock 去除 LLM 返回内容中可能包裹的 markdown 代码块标记。
// 支持 ```yaml、```yml、```json、无语言标记的 ``` 等。
func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	re := regexp.MustCompile("(?s)^```(?:ya?ml|json)?\\s*\n?(.*?)\\s*```$")
	if m := re.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

// ParseInterviewResponse 解析对话阶段 LLM 的 YAML 响应。
// 如果 YAML 解析失败，将原文降级为纯文本问题。
func ParseInterviewResponse(raw string) (*InterviewResponse, error) {
	cleaned := stripCodeBlock(raw)

	var resp InterviewResponse
	if err := yaml.Unmarshal([]byte(cleaned), &resp); err != nil {
		// 降级：将原文当作纯文本问题
		return &InterviewResponse{
			Type:     "question",
			Question: raw,
			Options:  []string{"继续", "其他（请说明）"},
		}, nil
	}

	return &resp, nil
}

// ParseGeneratedYAML 解析生成阶段 LLM 的 YAML 响应。
func ParseGeneratedYAML(raw string) (*GeneratedYAML, error) {
	cleaned := stripCodeBlock(raw)

	var result GeneratedYAML
	if err := yaml.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseAnalyzedFiles 解析文件识别阶段 LLM 的 YAML 响应。
func ParseAnalyzedFiles(raw string) (*AnalyzedFiles, error) {
	cleaned := stripCodeBlock(raw)

	var result AnalyzedFiles
	if err := yaml.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseSelectedFiles 解析重点文件选择阶段 LLM 的 YAML 响应。
func ParseSelectedFiles(raw string) (*SelectedFiles, error) {
	cleaned := stripCodeBlock(raw)

	var result SelectedFiles
	if err := yaml.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseSingleFileYAML 解析分步生成单个文件的 YAML 响应。
func ParseSingleFileYAML(raw string) (*SingleFileYAML, error) {
	cleaned := stripCodeBlock(raw)

	var result SingleFileYAML
	if err := yaml.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ParseModuleContractYAML 解析单个模块契约生成的 YAML 响应。
func ParseModuleContractYAML(raw string) (*ModuleContractYAML, error) {
	cleaned := stripCodeBlock(raw)

	var result ModuleContractYAML
	if err := yaml.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, err
	}

	return &result, nil
}
