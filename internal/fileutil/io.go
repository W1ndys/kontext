package fileutil

import (
	"os"
	"path/filepath"
)

// ReadFile 读取文件的全部内容。
func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile 将数据写入文件，如果父目录不存在则自动创建。
func WriteFile(path string, data []byte) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// FileExists 判断路径是否存在且为普通文件。
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirExists 判断路径是否存在且为目录。
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// EnsureDir 确保目录存在，如果不存在则递归创建。
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}
