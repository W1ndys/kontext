package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/w1ndys/kontext/internal/ui"
)

const (
	// githubLatestReleaseURL 是 GitHub API 获取最新 release 的地址。
	githubLatestReleaseURL = "https://api.github.com/repos/W1ndys/kontext/releases/latest"
	// versionCheckTimeout 是版本检查的超时时间。
	versionCheckTimeout = 3 * time.Second
)

// githubRelease 是 GitHub release API 返回的最小结构。
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// checkLatestVersion 检查是否有新版本可用。
// 返回最新版本号（不含 v 前缀）；如果当前已是最新或检查失败则返回空字符串。
func checkLatestVersion(currentVersion string) string {
	if currentVersion == "" || currentVersion == "dev" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	latest := normalizeVersion(release.TagName)
	current := normalizeVersion(currentVersion)

	if latest == "" || current == "" {
		return ""
	}

	if compareVersions(latest, current) > 0 {
		return latest
	}
	return ""
}

// printVersionHint 在终端输出新版本提示。
func printVersionHint(latestVersion string) {
	ui.Warn("发现新版本 v%s（当前 v%s），可通过以下命令更新：",
		latestVersion, normalizeVersion(Version))
	ui.Plain("  go install github.com/w1ndys/kontext@v%s", latestVersion)
	ui.Plain("  更多安装方式请参阅 https://github.com/W1ndys/kontext#readme")
	fmt.Println()
}

// normalizeVersion 移除版本号的 "v" 前缀并去除空白。
func normalizeVersion(v string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
}

// compareVersions 比较两个语义化版本号。
// 返回 1 表示 a > b，-1 表示 a < b，0 表示相等。
// 仅支持 major.minor.patch 格式。
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	// 补齐为三段
	for len(aParts) < 3 {
		aParts = append(aParts, "0")
	}
	for len(bParts) < 3 {
		bParts = append(bParts, "0")
	}

	for i := 0; i < 3; i++ {
		aNum := parseVersionPart(aParts[i])
		bNum := parseVersionPart(bParts[i])
		if aNum > bNum {
			return 1
		}
		if aNum < bNum {
			return -1
		}
	}
	return 0
}

// parseVersionPart 将版本号的一段解析为整数，失败返回 0。
func parseVersionPart(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	return n
}
