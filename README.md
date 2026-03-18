# Kontext

**从模糊需求到 AI-Native 工程物料的生成系统**

> 现在所有的工程规范、代码注释、项目文档都是为**人**设计的。  
> 但真正写代码的，越来越多是 AI。  
> **Kontext 为 AI 重新设计了工程上下文。**

## 🤔 解决什么问题？

你让 AI 帮你写代码时，是不是经常遇到：

- AI 不了解项目全貌，写出的代码风格不一致
- 要反复解释项目约定、架构分层、命名规范
- 低级模型（GPT-3.5/Qwen-7B）几乎不可用，必须上最贵的模型
- 每次对话都在"补充上下文"，真正写代码的时间反而很少
- 换一个 AI 工具/模型，又要从头解释一遍

**根因：我们给 AI 的信息是"为人设计的"——隐式的、散文式的、假设读者有经验的。**

## 💡 Kontext 的解法

将模糊的人类需求，转化为一套 **AI-Consumable Engineering Artifacts**：

| 物料 | 作用 | 对标人类世界 |
|------|------|-------------|
| `PROJECT_MANIFEST.yaml` | AI 的项目大脑 | 项目简介 + 技术方案 |
| `ARCHITECTURE_MAP.yaml` | AI 的导航地图 | 架构文档 |
| `MODULE_CONTRACTS/` | 每个模块的接口契约 | 接口文档 |
| `DEPENDENCY_GRAPH.yaml` | 模块依赖关系 | 架构图 |
| `TASK_CONTEXTS/` | 任务上下文包 | 任务 Brief |
| `CODE_SKELETON/` | AI 注释的代码骨架 | 脚手架 |
| `CONVENTIONS.yaml` | 项目约定规范 | 编码规范 |
| `DEV_PLAYBOOK.yaml` | AI 开发剧本 | 开发指南 |

## 🎯 效果
