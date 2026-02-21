package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"PairScan/tui"
)

// 支持的文件扩展名映射表，用于快速查询。
var supportedExtensions = map[string]bool{
	".txt": true,
	".gz":  true,
	".xz":  true,
	".zip": true,
}

// IsSupported 检查文件的扩展名是否在支持列表中。
func IsSupported(filePath string) bool {
	return supportedExtensions[strings.ToLower(filepath.Ext(filePath))]
}

// FindFiles 递归遍历目录，并将支持的文件路径发送到 filePathsChan 通道中。
func FindFiles(folderPath string, filePathsChan chan<- string) {
	defer close(filePathsChan)
	_ = filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && IsSupported(path) {
			filePathsChan <- path
			// 发送日志消息到 UI 界面
			tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("发现文件: %s", filepath.Base(path))})
		}
		return nil
	})
	tui.Send(tui.StatsUpdateMsg{LogMessage: "文件扫描完成。"})
}

// ScanFolder 扫描目录以获取所有支持的文件路径列表以及它们的总文件大小。
func ScanFolder(folderPath string) (filePaths []string, totalSize int64, err error) {
	err = filepath.Walk(folderPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && IsSupported(path) {
			filePaths = append(filePaths, path)
			totalSize += info.Size()
		}
		return nil
	})
	return
}
