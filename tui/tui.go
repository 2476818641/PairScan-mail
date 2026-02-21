package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// 用于跨包发送消息的全局程序实例。
var program *tea.Program
var shutdownWg sync.WaitGroup // 用于协调资源监控协程的优雅退出。

// SetProgram 允许 main 包设置程序实例。
func SetProgram(p *tea.Program) {
	program = p
}

// Send 是一个线程安全的助手函数，用于从任何地方向 TUI 发送消息。
func Send(msg tea.Msg) {
	if program != nil {
		program.Send(msg)
	}
}

// StatsUpdateMsg 统计数据更新消息。
type StatsUpdateMsg struct {
	FilesFound     int    // 发现的文件总数
	FilesProcessed int    // 已处理的文件数
	PairsExtracted int64  // 提取出的配对总数
	NewPairsFound  int64  // 数据库中不存在的新配对数
	TotalInDB      int64  // 数据库当前总记录数
	LogMessage     string // 要在 UI 中显示的日志消息
}

// ProgressMsg 工作进度消息。
type ProgressMsg struct {
	WorkerID int     // 工作线程 ID
	FilePath string  // 当前正在处理的文件名
	Progress float64 // 进度百分比 (0.0 到 1.0)
}

// ResourceUsageMsg 资源占用情况消息。
type ResourceUsageMsg struct {
	CPUUsage float64 // CPU 使用率百分比
	MemUsage float64 // 内存使用率百分比
	MemTotal uint64  // 系统总内存大小
}

// FinishedMsg 任务完成信号。
type FinishedMsg struct{}

// Model 定义了 TUI 的内部状态。
type Model struct {
	Spinner         spinner.Model       // 加载动画图标
	InputPath       string              // 正在扫描的输入路径
	ProcessingMode  string              // 处理模式描述
	Stats           StatsUpdateMsg      // 当前统计数据
	WorkerProgress  map[int]ProgressMsg // 记录每个工作线程的进度
	Resources       ResourceUsageMsg    // 系统资源占用信息
	ProcessingDone  bool                // 任务是否已完成
	LastLogMessages []string            // 滚动的日志历史记录
	InitialCmd      tea.Cmd             // 初始执行的命令（启动处理流程）
}

// NewInitialModel 为 TUI 创建初始模型。
func NewInitialModel(path, mode string, cmd tea.Cmd) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	// 使用 lipgloss 设置加载动画的样式颜色（兼容新版 API）
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return Model{
		Spinner:        s,
		InputPath:      path,
		ProcessingMode: mode,
		WorkerProgress: make(map[int]ProgressMsg),
		Stats:          StatsUpdateMsg{LogMessage: "正在初始化..."},
		InitialCmd:     cmd,
	}
}

// Init 初始化函数，在 TUI 启动时运行。
func (m Model) Init() tea.Cmd {
	shutdownWg.Add(1)
	go monitorResources() // 启动异步资源监控
	return tea.Batch(m.Spinner.Tick, m.InitialCmd)
}

// Update 处理所有传入的消息并更新模型状态。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// 监听退出快捷键
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			shutdownWg.Done()
			return m, tea.Quit
		}
	case spinner.TickMsg:
		// 更新加载动画
		var cmd tea.Cmd
		if !m.ProcessingDone {
			m.Spinner, cmd = m.Spinner.Update(msg)
		}
		return m, cmd
	case StatsUpdateMsg:
		// 更新统计数据和日志
		m.Stats.FilesProcessed += msg.FilesProcessed
		if msg.FilesFound > 0 {
			m.Stats.FilesFound = msg.FilesFound
		}
		if msg.PairsExtracted > 0 {
			m.Stats.PairsExtracted = msg.PairsExtracted
		}
		if msg.NewPairsFound > 0 {
			m.Stats.NewPairsFound = msg.NewPairsFound
		}
		if msg.TotalInDB > 0 {
			m.Stats.TotalInDB = msg.TotalInDB
		}
		if msg.LogMessage != "" {
			m.logMessage(msg.LogMessage)
		}
	case ProgressMsg:
		// 更新特定工作线程的进度条
		m.WorkerProgress[msg.WorkerID] = msg
	case ResourceUsageMsg:
		// 更新 CPU 和内存信息
		m.Resources = msg
	case FinishedMsg:
		// 标记所有任务已完成
		m.ProcessingDone = true
		m.Spinner.Spinner = spinner.Spinner{Frames: []string{"🎉"}}
		m.logMessage("所有任务处理完成！按 'q' 或 Ctrl+C 退出。")
		shutdownWg.Done()
	}
	return m, nil
}

// View 渲染 TUI 的外观
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n  🚀 正在处理: %s\n", m.InputPath))
	b.WriteString(fmt.Sprintf("  💡 处理模式: %s\n\n", m.ProcessingMode))

	status := "运行中"
	if m.ProcessingDone {
		status = "已完成"
	}
	b.WriteString(fmt.Sprintf("  %s 状态: %s\n", m.Spinner.View(), status))
	b.WriteString(fmt.Sprintf("  文件: %d / %d | 提取: %s | 新增: %s | DB总数: %s\n",
		m.Stats.FilesProcessed, m.Stats.FilesFound,
		humanize.Comma(m.Stats.PairsExtracted),
		humanize.Comma(m.Stats.NewPairsFound),
		humanize.Comma(m.Stats.TotalInDB)))

	// 格式化资源占用显示
	b.WriteString(fmt.Sprintf("  资源: CPU: %5.1f%% | 内存: %s / %s (%.1f%%)\n\n",
		m.Resources.CPUUsage,
		humanize.Bytes(uint64(float64(m.Resources.MemTotal)*m.Resources.MemUsage/100.0)),
		humanize.Bytes(m.Resources.MemTotal),
		m.Resources.MemUsage))

	// 渲染进度条部分
	if m.ProcessingMode == "高速内存模式" || m.ProcessingMode == "单文件模式" {
		b.WriteString("  总读取进度:\n")
		prog, ok := m.WorkerProgress[0]
		if ok {
			barStr := renderProgressBar(40, prog.Progress)
			b.WriteString(fmt.Sprintf("  进度: %s %.0f%%\n", barStr, prog.Progress*100))
		}
	} else {
		b.WriteString("  多线程工作进度:\n")
		workerIDs := make([]int, 0, len(m.WorkerProgress))
		for id := range m.WorkerProgress {
			workerIDs = append(workerIDs, id)
		}
		sort.Ints(workerIDs)
		for _, id := range workerIDs {
			prog := m.WorkerProgress[id]
			barStr := renderProgressBar(40, prog.Progress)
			b.WriteString(fmt.Sprintf("  W%d: %s %.0f%% %s\n", id, barStr, prog.Progress*100, prog.FilePath))
		}
	}
	b.WriteString("\n")

	// 渲染滚动日志
	b.WriteString("  最新日志:\n")
	for _, log := range m.LastLogMessages {
		b.WriteString(fmt.Sprintf("  %s\n", log))
	}

	if !m.ProcessingDone {
		b.WriteString("\n  (按 'q' 或 Ctrl+C 强制退出并停止)")
	}

	return b.String()
}

// logMessage 向历史记录添加一条新日志，并保持记录条数在 5 条以内。
func (m *Model) logMessage(msg string) {
	m.LastLogMessages = append(m.LastLogMessages, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
	if len(m.LastLogMessages) > 5 {
		m.LastLogMessages = m.LastLogMessages[1:]
	}
}

// renderProgressBar 渲染一个简单的文本进度条。
func renderProgressBar(width int, progress float64) string {
	filledWidth := int(float64(width) * progress)
	if filledWidth > width {
		filledWidth = width
	}
	// 使用绿色 ANSI 转义码显示进度
	return "[\x1b[32m" + strings.Repeat("=", filledWidth) + "\x1b[0m" + strings.Repeat("-", width-filledWidth) + "]"
}

// monitorResources 在独立的协程中运行，每秒发送一次系统资源占用更新。
func monitorResources() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		shutdownWg.Wait()
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
			cpuPerc, _ := cpu.Percent(0, false)
			memInfo, _ := mem.VirtualMemory()
			if len(cpuPerc) > 0 {
				Send(ResourceUsageMsg{
					CPUUsage: cpuPerc[0],
					MemUsage: memInfo.UsedPercent,
					MemTotal: memInfo.Total,
				})
			}
		}
	}
}

// Cleanup 清理全局资源。
// 并发安全：应确保在使用 Send() 的所有 goroutine 完成后再调用。
func Cleanup() {
	shutdownWg = sync.WaitGroup{}
	program = nil
}
