package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/w1ndys/kontext/internal/fileutil"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "初始化 .kontext/ 目录并写入默认模板",
	RunE: func(cmd *cobra.Command, args []string) error {
		kontextDir := ".kontext"

		if fileutil.DirExists(kontextDir) && fileutil.FileExists(filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")) {
			fmt.Println(".kontext/ 已存在，跳过初始化。")
			return nil
		}

		// 创建目录结构
		dirs := []string{
			kontextDir,
			filepath.Join(kontextDir, "module_contracts"),
			filepath.Join(kontextDir, "prompts"),
		}
		for _, d := range dirs {
			if err := fileutil.EnsureDir(d); err != nil {
				return fmt.Errorf("创建目录 %s 失败: %w", d, err)
			}
		}

		// 写入默认模板文件
		templates := map[string]string{
			filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml"): defaultManifest,
			filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml"): defaultArchitecture,
			filepath.Join(kontextDir, "CONVENTIONS.yaml"):      defaultConventions,
		}

		for path, content := range templates {
			if fileutil.FileExists(path) {
				fmt.Printf("  跳过: %s (已存在)\n", path)
				continue
			}
			if err := fileutil.WriteFile(path, []byte(content)); err != nil {
				return fmt.Errorf("写入 %s 失败: %w", path, err)
			}
			fmt.Printf("  已创建: %s\n", path)
		}

		fmt.Println("\n.kontext/ 初始化完成！")
		fmt.Println("后续步骤：")
		fmt.Println("  1. 编辑 .kontext/PROJECT_MANIFEST.yaml 填写项目信息")
		fmt.Println("  2. 编辑 .kontext/ARCHITECTURE_MAP.yaml 填写架构信息")
		fmt.Println("  3. 编辑 .kontext/CONVENTIONS.yaml 填写编码规范")
		fmt.Println("  4. 运行 'kontext validate' 校验配置是否正确")

		return nil
	},
}

const defaultManifest = `# .kontext/PROJECT_MANIFEST.yaml
# 用途：AI 开发助手的首要上下文文件，建立项目全局理解

project:
  name: "my-project"
  one_line: "用一句话描述你的项目"
  type: "web_app"  # 可选: cli_tool, web_app, library, microservice

  business_context: |
    在这里描述项目的业务背景和目标。
    它解决什么问题？用户是谁？

  core_flows:
    - name: "主流程"
      steps: "步骤 1 → 步骤 2 → 步骤 3"
      entry_point: "cmd/main.go"

tech_stack:
  language: "Go 1.21+"
  # 在这里添加你的技术栈详情
  key_decisions:
    - decision: "关键架构决策"
      reason: "做出此决策的原因"
      constraint: "此决策带来的约束"

scale:
  estimated_files: "10-50"
  modules: "3"
  phase: "development"

status:
  completed_modules: []
  in_progress: []
  not_started: []
`

const defaultArchitecture = `# .kontext/ARCHITECTURE_MAP.yaml
# 用途：定义项目的分层架构和架构规则

layers:
  - name: "CLI 层"
    description: "命令行界面与用户交互"
    packages:
      - "cmd"

  - name: "核心层"
    description: "核心业务逻辑"
    packages:
      - "internal/core"

  - name: "基础设施层"
    description: "外部集成与工具库"
    packages:
      - "internal/infra"

rules:
  - rule: "CLI 层不得包含业务逻辑"
    reason: "关注点分离"

  - rule: "核心层不得依赖基础设施层"
    reason: "保持核心逻辑可移植和可测试"
`

const defaultConventions = `# .kontext/CONVENTIONS.yaml
# 用途：定义编码规范和 AI 协作规则

coding:
  - rule: "使用有描述性的变量名"
    example: "userCount 而不是 n"
  - rule: "函数体不超过 50 行"
    reason: "保持可读性"

error_handling:
  - rule: "错误必须包装上下文信息"
    example: 'fmt.Errorf("执行 X 操作: %w", err)'

forbidden:
  - rule: "禁止全局可变状态"
    reason: "会导致测试困难和竞态条件"

ai_rules:
  - rule: "修改代码前必须先阅读已有代码"
    reason: "先理解上下文再做变更"
  - rule: "严格遵守模块契约中定义的边界"
    reason: "维护架构完整性"
`
