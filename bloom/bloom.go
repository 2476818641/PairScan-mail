package bloom

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/willf/bloom"
)

// Initialize 通过从数据库加载所有现有键来创建一个新的布隆过滤器。
func Initialize(db *sql.DB) (*bloom.BloomFilter, error) {
	fmt.Println("--------------------------------------------------")
	fmt.Println("🚀 正在初始化布隆过滤器以优化性能...")

	// 步骤 1: 获取总记录数，用于估算布隆过滤器的大小。
	fmt.Println("步骤 1/3: 正在查询数据库记录总数...")
	var n uint
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM blacklist").Scan(&n)
	if err != nil {
		return nil, fmt.Errorf("查询总记录数失败: %w", err)
	}
	// 如果表是空的，给一个默认值 1 以便初始化过滤器
	if n == 0 {
		n = 1 
	}

	// 步骤 2: 在内存中创建布隆过滤器。
	// 容错率（False Positive Rate）设为 0.001 (0.1%)。
	// 注意：容错率越低，内存占用越高；0.001 是性能和精度的良好折中。
	fmt.Printf("数据库中当前存有 %s 条记录。\n", humanize.Comma(int64(n)))
	fmt.Println("步骤 2/3: 正在分配内存空间 (误报率设为 0.1%)...")
	bloomFilter := bloom.NewWithEstimates(n, 0.001)

	// 步骤 3: 从数据库流式读取所有配对并添加到过滤器中。
	fmt.Println("步骤 3/3: 正在从数据库同步数据 (海量数据下此过程可能需要几分钟)...")
	ctx, cancel = context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	rows, err := db.QueryContext(ctx, "SELECT pair FROM blacklist")
	if err != nil {
		return nil, fmt.Errorf("读取全量数据失败: %w", err)
	}
	defer rows.Close()

	var loadedCount int64
	loadTicker := time.NewTicker(5 * time.Second)
	defer loadTicker.Stop()

	done := make(chan bool)
	go func() {
		for {
			select {
			case <-loadTicker.C:
				fmt.Printf("...已加载 %s / %s 条记录\n", humanize.Comma(loadedCount), humanize.Comma(int64(n)))
			case <-done:
				return
			}
		}
	}()

	for rows.Next() {
		var pair string
		if err := rows.Scan(&pair); err != nil {
			fmt.Fprintf(os.Stderr, "读取行数据时出错: %v\n", err)
			continue
		}
		bloomFilter.AddString(pair)
		loadedCount++
	}

	// 停止进度打印
	done <- true

	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "遍历结果集时出错: %v\n", err)
	}

	fmt.Printf("...最终成功将 %s 条记录载入内存过滤器。\n", humanize.Comma(loadedCount))
	fmt.Println("✅ 布隆过滤器初始化成功！")
	fmt.Println("--------------------------------------------------")
	return bloomFilter, nil
}
