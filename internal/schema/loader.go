package schema

import (
	"fmt"
	"path/filepath"

	"github.com/w1ndys/kontext/internal/fileutil"
	"gopkg.in/yaml.v3"
)

// Bundle 聚合了 .kontext/ 目录下所有加载的配置数据。
type Bundle struct {
	Manifest     ProjectManifest
	Architecture ArchitectureMap
	Conventions  Conventions
	Contracts    []ModuleContract
}

// LoadBundle 从 .kontext/ 目录加载所有 YAML 配置文件。
// Manifest 为必须项；Architecture、Conventions、Contracts 缺失时使用零值（容错策略）。
func LoadBundle(kontextDir string) (*Bundle, error) {
	b := &Bundle{}

	// Manifest 是必须的
	manifestPath := filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")
	if !fileutil.FileExists(manifestPath) {
		return nil, fmt.Errorf("在 %s 中未找到 PROJECT_MANIFEST.yaml", kontextDir)
	}
	data, err := fileutil.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("读取 manifest 失败: %w", err)
	}
	if err := yaml.Unmarshal(data, &b.Manifest); err != nil {
		return nil, fmt.Errorf("解析 manifest 失败: %w", err)
	}

	// Architecture 为可选项
	archPath := filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml")
	if fileutil.FileExists(archPath) {
		data, err := fileutil.ReadFile(archPath)
		if err == nil {
			yaml.Unmarshal(data, &b.Architecture)
		}
	}

	// Conventions 为可选项
	convPath := filepath.Join(kontextDir, "CONVENTIONS.yaml")
	if fileutil.FileExists(convPath) {
		data, err := fileutil.ReadFile(convPath)
		if err == nil {
			yaml.Unmarshal(data, &b.Conventions)
		}
	}

	// Contracts 为可选项，从 module_contracts/ 目录批量加载
	contractsDir := filepath.Join(kontextDir, "module_contracts")
	if fileutil.DirExists(contractsDir) {
		files, err := fileutil.ScanDirectoryTree(contractsDir, 1)
		if err == nil {
			for _, f := range files {
				if filepath.Ext(f) != ".yaml" && filepath.Ext(f) != ".yml" {
					continue
				}
				data, err := fileutil.ReadFile(filepath.Join(contractsDir, f))
				if err != nil {
					continue
				}
				var c ModuleContract
				if err := yaml.Unmarshal(data, &c); err == nil {
					b.Contracts = append(b.Contracts, c)
				}
			}
		}
	}

	return b, nil
}

// ValidateBundle 校验 .kontext/ 目录下所有 YAML 配置文件是否可正确解析。
func ValidateBundle(kontextDir string) []error {
	var errs []error

	// 校验 manifest（必须项）
	manifestPath := filepath.Join(kontextDir, "PROJECT_MANIFEST.yaml")
	if !fileutil.FileExists(manifestPath) {
		errs = append(errs, fmt.Errorf("未找到 PROJECT_MANIFEST.yaml"))
		return errs
	}
	data, err := fileutil.ReadFile(manifestPath)
	if err != nil {
		errs = append(errs, fmt.Errorf("读取 manifest 失败: %w", err))
		return errs
	}
	var m ProjectManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		errs = append(errs, fmt.Errorf("解析 manifest 失败: %w", err))
	}
	if m.Project.Name == "" {
		errs = append(errs, fmt.Errorf("manifest: project.name 为必填字段"))
	}

	// 校验 architecture（可选项）
	archPath := filepath.Join(kontextDir, "ARCHITECTURE_MAP.yaml")
	if fileutil.FileExists(archPath) {
		data, err := fileutil.ReadFile(archPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("读取 architecture 失败: %w", err))
		} else {
			var a ArchitectureMap
			if err := yaml.Unmarshal(data, &a); err != nil {
				errs = append(errs, fmt.Errorf("解析 architecture 失败: %w", err))
			}
		}
	}

	// 校验 conventions（可选项）
	convPath := filepath.Join(kontextDir, "CONVENTIONS.yaml")
	if fileutil.FileExists(convPath) {
		data, err := fileutil.ReadFile(convPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("读取 conventions 失败: %w", err))
		} else {
			var c Conventions
			if err := yaml.Unmarshal(data, &c); err != nil {
				errs = append(errs, fmt.Errorf("解析 conventions 失败: %w", err))
			}
		}
	}

	// 校验 contracts（可选项）
	contractsDir := filepath.Join(kontextDir, "module_contracts")
	if fileutil.DirExists(contractsDir) {
		files, err := fileutil.ScanDirectoryTree(contractsDir, 1)
		if err == nil {
			for _, f := range files {
				if filepath.Ext(f) != ".yaml" && filepath.Ext(f) != ".yml" {
					continue
				}
				data, err := fileutil.ReadFile(filepath.Join(contractsDir, f))
				if err != nil {
					errs = append(errs, fmt.Errorf("读取 contract %s 失败: %w", f, err))
					continue
				}
				var c ModuleContract
				if err := yaml.Unmarshal(data, &c); err != nil {
					errs = append(errs, fmt.Errorf("解析 contract %s 失败: %w", f, err))
				}
			}
		}
	}

	return errs
}
