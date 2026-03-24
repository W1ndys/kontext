package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/w1ndys/kontext/internal/generator"
)

// spinnerAnimation 显示旋转加载动画，phases 为轮换展示的阶段文案。
func spinnerAnimation(done <-chan struct{}, startTime time.Time, phases []string) {
	dots := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			clearLine()
			return
		case <-ticker.C:
			elapsed := time.Since(startTime).Seconds()
			phase := phases[int(elapsed/15)%len(phases)]
			fmt.Printf("\r   %s %s... (%.0f秒)", dots[i%len(dots)], phase, elapsed)
			i++
		}
	}
}

// localAnalyzeFiles 使用本地规则识别文件（作为 LLM 识别的回退方案）。
func localAnalyzeFiles(allFiles []string) *generator.AnalyzedFiles {
	configFileNames := map[string]bool{
		"go.mod": true, "go.sum": true, "package.json": true, "tsconfig.json": true,
		"Cargo.toml": true, "pyproject.toml": true, "requirements.txt": true,
		"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
		"Makefile": true, "Dockerfile": true, "docker-compose.yml": true,
		"docker-compose.yaml": true, ".gitignore": true, "CMakeLists.txt": true,
		".eslintrc.json": true, ".prettierrc": true, "webpack.config.js": true,
		"vite.config.ts": true, "vite.config.js": true,
	}

	result := &generator.AnalyzedFiles{}
	configSet := make(map[string]bool)

	for _, f := range allFiles {
		base := filepath.Base(f)
		if configFileNames[base] {
			result.ConfigFiles = append(result.ConfigFiles, f)
			configSet[f] = true
		}
	}

	for _, f := range allFiles {
		if configSet[f] {
			continue
		}
		if isSourceFile(f) {
			result.SourceFiles = append(result.SourceFiles, f)
		}
		if len(result.SourceFiles) >= 30 {
			break
		}
	}

	return result
}

// printProgress 打印进度条
func printProgress(current, total int, label string) {
	width := 30
	percent := float64(current) / float64(total)
	filled := int(percent * float64(width))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	fmt.Printf("\r   [%s] %3.0f%% %s", bar, percent*100, label)
}

// printProgressWithFile 打印带文件名的进度条
func printProgressWithFile(current, total int, label, filename string) {
	width := 20
	percent := float64(current) / float64(total)
	filled := int(percent * float64(width))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	// 截断过长的文件名
	displayName := filename
	if len(displayName) > 35 {
		displayName = "..." + displayName[len(displayName)-32:]
	}

	fmt.Printf("\r   [%s] %3.0f%% %s: %-35s", bar, percent*100, label, displayName)
}

// clearLine 清除当前行
func clearLine() {
	fmt.Print("\r\033[K")
}

// printFileList 打印文件列表（带缩进和树形结构）
// maxShow 为最多显示的文件数，超过则显示省略提示
func printFileList(files []string, maxShow int) {
	if len(files) == 0 {
		return
	}

	showCount := len(files)
	if maxShow > 0 && showCount > maxShow {
		showCount = maxShow
	}

	for i := 0; i < showCount; i++ {
		prefix := "├──"
		if i == showCount-1 && (maxShow <= 0 || len(files) <= maxShow) {
			prefix = "└──"
		}
		fmt.Printf("      %s %s\n", prefix, files[i])
	}

	if maxShow > 0 && len(files) > maxShow {
		fmt.Printf("      └── ... 等 %d 个文件\n", len(files)-maxShow)
	}
}

// printFileListWithTitle 打印带标题的文件列表
func printFileListWithTitle(title string, files []string, maxShow int) {
	if len(files) == 0 {
		return
	}
	fmt.Printf("      %s (%d 个):\n", title, len(files))
	for i, f := range files {
		if maxShow > 0 && i >= maxShow {
			fmt.Printf("         ... 等 %d 个文件\n", len(files)-maxShow)
			break
		}
		fmt.Printf("         • %s\n", f)
	}
}

// readFirstNLines 读取文件的前 n 行
func readFirstNLines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() && len(lines) < n {
		lines = append(lines, scanner.Text())
	}

	return strings.Join(lines, "\n"), nil
}

// isSourceFile 判断文件是否为源码文件。
func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	sourceExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
		".java": true, ".kt": true, ".rs": true, ".c": true, ".cpp": true, ".h": true,
		".cs": true, ".rb": true, ".php": true, ".swift": true, ".m": true,
		".scala": true, ".dart": true, ".lua": true, ".sh": true, ".bash": true,
		".vue": true, ".svelte": true,
	}
	return sourceExts[ext]
}
