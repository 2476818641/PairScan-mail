package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	bloomsetup "PairScan/bloom"
	"PairScan/config"
	"PairScan/database"
	"PairScan/files"
	"PairScan/processor"
	"PairScan/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize"
	"github.com/willf/bloom"
)

func main() {
	// ---------------------------------------------------------
	// 步骤 1: 加载配置文件
	// 支持 YAML 配置文件和环境变量覆盖（如 DB_PASSWORD）
	// ---------------------------------------------------------
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("错误: 无法加载配置文件: %v\n", err)
		os.Exit(1)
	}

	// ---------------------------------------------------------
	// 步骤 2: 初始化数据库连接
	// 根据配置连接到 SQLite (本地) 或 MySQL (远程)
	// ---------------------------------------------------------
	db, err := database.Init(cfg)
	if err != nil {
		fmt.Printf("数据库初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// ---------------------------------------------------------
	// 步骤 3: (可选) 为远程 MySQL 初始化布隆过滤器
	// 通过内存中的布隆过滤器减少数据库查询次数
	// ---------------------------------------------------------
	var bloomFilter *bloom.BloomFilter
	if cfg.Database.UseRemoteMySQL {
		bloomFilter, err = bloomsetup.Initialize(db)
		if err != nil {
			fmt.Printf("布隆过滤器初始化失败: %v\n", err)
			fmt.Println("将继续使用标准去重方式，性能可能会降低")
			bloomFilter = nil
		}
	}

	// ---------------------------------------------------------
	// 步骤 4: 获取用户输入并确定处理模式
	// ---------------------------------------------------------
	fmt.Println("\n请选择处理模式:")
	fmt.Println("1. 处理单个文件 (.txt, .gz, .xz, .zip)")
	fmt.Println("2. 处理文件夹 (包含上述所有支持的格式)")
	fmt.Print("请输入选项 (1 或 2): ")

	reader := bufio.NewReader(os.Stdin)
	choice, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("读取输入失败: %v\n", err)
		os.Exit(1)
	}
	choice = strings.TrimSpace(choice)

	var processorCmd tea.Cmd
	var inputPath, mode string
	var filesFound int

	switch choice {
	case "1":
		// 单文件模式逻辑
		fmt.Print("请输入要处理的文件路径: ")
		filePath, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			os.Exit(1)
		}
		filePath = strings.TrimSpace(filePath)
		inputPath = filePath

		info, err := os.Stat(filePath)
		if err != nil || !files.IsSupported(filePath) {
			fmt.Printf("错误: 文件 '%s' 无效或类型不支持。\n", filePath)
			os.Exit(1)
		}
		mode = "单文件模式"
		filesFound = 1
		processorCmd = processor.RunInMemoryProcessor(
			[]string{filePath},
			info.Size(),
			cfg,
			db,
			bloomFilter,
		)

	case "2":
		// 文件夹模式逻辑
		fmt.Print("请输入要处理的日志文件夹路径: ")
		folderPath, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败: %v\n", err)
			os.Exit(1)
		}
		folderPath = strings.TrimSpace(folderPath)
		inputPath = folderPath

		info, err := os.Stat(folderPath)
		if err != nil || !info.IsDir() {
			fmt.Printf("错误: 文件夹 '%s' 不存在。\n", folderPath)
			os.Exit(1)
		}

		fmt.Println("正在扫描文件夹以确定最佳处理模式...")
		filePaths, totalSize, err := files.ScanFolder(folderPath)
		if err != nil {
			fmt.Printf("扫描文件夹失败: %v\n", err)
			os.Exit(1)
		}
		filesFound = len(filePaths)

		// 使用 config 中定义的常量进行模式选择判断
		if totalSize < config.InMemoryThreshold && len(filePaths) > 0 {
			mode = "高速内存模式"
			fmt.Printf("总文件大小: %s，小于 2GB。将使用 %s。\n", humanize.Bytes(uint64(totalSize)), mode)
			processorCmd = processor.RunInMemoryProcessor(filePaths, totalSize, cfg, db, bloomFilter)
		} else {
			mode = "流式处理模式"
			fmt.Printf("总文件大小: %s，大于等于 2GB。将使用 %s 以节省内存。\n", humanize.Bytes(uint64(totalSize)), mode)
			processorCmd = processor.RunStreamingProcessor(folderPath, cfg, db, bloomFilter)
		}

	default:
		fmt.Println("无效选项，程序退出。")
		os.Exit(1)
	}

	// ---------------------------------------------------------
	// 步骤 5: 初始化并启动 TUI (终端图形界面)
	// ---------------------------------------------------------
	m := tui.NewInitialModel(inputPath, mode, processorCmd)
	m.Stats.FilesFound = filesFound

	program := tea.NewProgram(m, tea.WithAltScreen())
	tui.SetProgram(program)

	if _, err := program.Run(); err != nil {
		fmt.Printf("启动 UI 时出错: %v\n", err)
		os.Exit(1)
	}

	// 清理全局资源
	tui.Cleanup()

	fmt.Println("\n处理完成！")
}
