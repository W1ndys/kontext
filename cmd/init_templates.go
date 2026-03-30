package cmd

const defaultManifest = `{
  "project": {
    "name": "my-project",
    "one_line": "用一句话描述你的项目",
    "type": "web_app",
    "business_context": "在这里描述项目的业务背景和目标。\n它解决什么问题？用户是谁？",
    "core_flows": [
      {
        "name": "主流程",
        "steps": "步骤 1 → 步骤 2 → 步骤 3",
        "entry_point": "cmd/main.go"
      }
    ]
  },
  "tech_stack": {
    "language": "Go 1.21+",
    "key_decisions": [
      {
        "decision": "关键架构决策",
        "reason": "做出此决策的原因",
        "constraint": "此决策带来的约束"
      }
    ]
  },
  "scale": {
    "estimated_files": "10-50",
    "modules": "3",
    "phase": "development"
  },
  "status": {
    "completed_modules": [],
    "in_progress": [],
    "not_started": []
  }
}
`

const defaultArchitecture = `{
  "layers": [
    {
      "name": "CLI 层",
      "description": "命令行界面与用户交互",
      "packages": ["cmd"]
    },
    {
      "name": "核心层",
      "description": "核心业务逻辑",
      "packages": ["internal/core"]
    },
    {
      "name": "基础设施层",
      "description": "外部集成与工具库",
      "packages": ["internal/infra"]
    }
  ],
  "rules": [
    {
      "rule": "CLI 层不得包含业务逻辑",
      "reason": "关注点分离"
    },
    {
      "rule": "核心层不得依赖基础设施层",
      "reason": "保持核心逻辑可移植和可测试"
    }
  ]
}
`

const defaultConventions = `{
  "coding": [
    {
      "rule": "使用有描述性的变量名",
      "example": "userCount 而不是 n"
    },
    {
      "rule": "函数体不超过 50 行",
      "reason": "保持可读性"
    }
  ],
  "error_handling": [
    {
      "rule": "错误必须包装上下文信息",
      "example": "fmt.Errorf(\"执行 X 操作: %w\", err)"
    }
  ],
  "forbidden": [
    {
      "rule": "禁止全局可变状态",
      "reason": "会导致测试困难和竞态条件"
    }
  ],
  "ai_rules": [
    {
      "rule": "修改代码前必须先阅读已有代码",
      "reason": "先理解上下文再做变更"
    },
    {
      "rule": "严格遵守模块契约中定义的边界",
      "reason": "维护架构完整性"
    }
  ]
}
`

const defaultContract = `{
  "module": {
    "name": "example",
    "path": "internal/example/",
    "purpose": "在这里描述模块的核心职责。\n它负责哪些功能？解决什么问题？"
  },
  "owns": [
    "该模块负责的功能点 1",
    "该模块负责的功能点 2"
  ],
  "not_responsible_for": [
    "该模块明确不负责的功能",
    "应由其他模块处理的功能"
  ],
  "depends_on": [
    {
      "module": "其他模块名",
      "reason": "为什么依赖这个模块"
    }
  ],
  "public_interface": [
    {
      "name": "ExampleFunc",
      "signature": "func ExampleFunc(param string) (Result, error)",
      "description": "函数功能描述"
    }
  ],
  "modification_rules": [
    {
      "rule": "修改该模块时必须遵守的规则",
      "reason": "原因说明"
    }
  ]
}
`
