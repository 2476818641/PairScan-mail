package processor

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"PairScan/config"
	"PairScan/database"
	"PairScan/files"
	"PairScan/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ulikunitz/xz"
	"github.com/willf/bloom"
)

var (
	pairPattern = regexp.MustCompile(`^(?:https?:\/\/|android:\/\/.*?:|[^/:]+\.[^/:]+:)?.*?([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})\s*:\s*([^:]+)$`)
)

// isPotentialMatch 执行极速初筛，避开开销大的正则引擎
func isPotentialMatch(line string) bool {
	// 基础长度过滤 (e.g., a@b.co:12345678)
	if len(line) < 15 {
		return false
	}
	// 必须包含 @ 符号
	if strings.IndexByte(line, '@') == -1 {
		return false
	}
	// 必须包含 : 符号
	if strings.IndexByte(line, ':') == -1 {
		return false
	}
	return true
}

// RunStreamingProcessor starts the file processing pipeline for large datasets.
func RunStreamingProcessor(folderPath string, cfg config.Config, db *sql.DB, bf *bloom.BloomFilter) tea.Cmd {
	return func() tea.Msg {
		filePathsChan := make(chan string, 100)
		extractedPairsChan := make(chan string, 10000)
		var workersWg sync.WaitGroup

		go files.FindFiles(folderPath, filePathsChan)

		numWorkers := runtime.NumCPU()
		workersWg.Add(numWorkers)
		for i := 0; i < numWorkers; i++ {
			go processFileWorker(i, filePathsChan, extractedPairsChan, &workersWg)
		}

		go collectAndFilter(extractedPairsChan, cfg, db, bf)

		workersWg.Wait()
		close(extractedPairsChan)
		return nil // Command is done
	}
}

func RunInMemoryProcessor(filePaths []string, totalSize int64, cfg config.Config, db *sql.DB, bf *bloom.BloomFilter) tea.Cmd {
	return func() tea.Msg {
		linesChan := make(chan string, 20000)
		extractedPairsChan := make(chan string, 10000)
		var workersWg sync.WaitGroup

		go readAllFilesToChan(filePaths, totalSize, linesChan)

		numWorkers := runtime.NumCPU()
		workersWg.Add(numWorkers)
		for i := 0; i < numWorkers; i++ {
			go processLineWorker(linesChan, extractedPairsChan, &workersWg)
		}

		go collectAndFilter(extractedPairsChan, cfg, db, bf)

		workersWg.Wait()
		close(extractedPairsChan)
		return nil // Command is done
	}
}

func processFileWorker(id int, filePathsChan <-chan string, extractedPairsChan chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	for filePath := range filePathsChan {
		ext := strings.ToLower(filepath.Ext(filePath))

		if ext == ".zip" {
			processZipArchive(id, filePath, extractedPairsChan)
			tui.Send(tui.StatsUpdateMsg{FilesProcessed: 1})
			continue
		}

		fileInfo, err := os.Stat(filePath)
		if err != nil || fileInfo.Size() == 0 {
			if err == nil && fileInfo.Size() == 0 {
				tui.Send(tui.StatsUpdateMsg{FilesProcessed: 1, LogMessage: fmt.Sprintf("跳过空文件: %s", filepath.Base(filePath))})
			}
			continue
		}

		readerCloser, err := getDecompressedReader(filePath)
		if err != nil {
			tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("错误: 打开 %s 失败: %v", filePath, err)})
			continue
		}

		reader := bufio.NewReader(readerCloser)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				trimmed := strings.TrimSpace(line)
				if isPotentialMatch(trimmed) {
					match := pairPattern.FindStringSubmatch(trimmed)
					if len(match) == 3 && len(match[2]) >= 8 {
						extractedPairsChan <- fmt.Sprintf("%s:%s", match[1], match[2])
					}
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
		}
		readerCloser.Close()

		tui.Send(tui.ProgressMsg{WorkerID: id, FilePath: filepath.Base(filePath), Progress: 1.0})
		tui.Send(tui.StatsUpdateMsg{FilesProcessed: 1})
	}
}

func readAllFilesToChan(filePaths []string, totalSize int64, linesChan chan<- string) {
	defer close(linesChan)
	var totalBytesRead int64

	for i, filePath := range filePaths {
		fileInfo, _ := os.Stat(filePath)
		ext := strings.ToLower(filepath.Ext(filePath))

		processReader := func(r io.Reader, pathForLog string) {
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				linesChan <- scanner.Text()
			}
		}

		if ext == ".zip" {
			r, err := zip.OpenReader(filePath)
			if err != nil {
				tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("错误: 打开ZIP %s 失败: %v", filePath, err)})
				continue
			}
			for _, f := range r.File {
				if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".txt") {
					continue
				}
				rc, err := f.Open()
				if err != nil {
					continue
				}
				processReader(rc, f.Name)
				rc.Close()
			}
			r.Close()
		} else {
			rc, err := getDecompressedReader(filePath)
			if err != nil {
				tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("错误: 打开 %s 失败: %v", filePath, err)})
				continue
			}
			processReader(rc, filepath.Base(filePath))
			rc.Close()
		}

		totalBytesRead += fileInfo.Size()
		if totalSize > 0 {
			progress := float64(totalBytesRead) / float64(totalSize)
			tui.Send(tui.ProgressMsg{WorkerID: 0, Progress: progress})
		}
		tui.Send(tui.StatsUpdateMsg{FilesProcessed: 1, LogMessage: fmt.Sprintf("已读取 [%d/%d] %s", i+1, len(filePaths), filepath.Base(filePath))})
	}
	tui.Send(tui.ProgressMsg{WorkerID: 0, Progress: 1.0})
}

func processLineWorker(linesChan <-chan string, extractedPairsChan chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	for line := range linesChan {
		trimmed := strings.TrimSpace(line)
		if isPotentialMatch(trimmed) {
			match := pairPattern.FindStringSubmatch(trimmed)
			if len(match) == 3 && len(match[2]) >= 8 {
				extractedPairsChan <- fmt.Sprintf("%s:%s", match[1], match[2])
			}
		}
	}
}

func collectAndFilter(extractedPairsChan <-chan string, cfg config.Config, db *sql.DB, bloomFilter *bloom.BloomFilter) {
	if (!cfg.Database.UseRemoteMySQL() && !cfg.Database.UseRemotePostgreSQL()) || bloomFilter == nil {
		collectAndFilterOriginal(extractedPairsChan, cfg, db)
		return
	}
	collectAndFilterWithBloom(extractedPairsChan, cfg, db, bloomFilter)
}

// 使用布隆过滤器进行去重
func collectAndFilterWithBloom(extractedPairsChan <-chan string, cfg config.Config, db *sql.DB, bloomFilter *bloom.BloomFilter) {
	confirmedNewPairs := make(map[string]bool)
	pairsToDoubleCheck := make(map[string]bool)
	var pairsExtracted, newPairsFound, totalInDB int64
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	batchSizeForDBCheck := cfg.Database.GetConfigBatchSize() * 5
	batchSizeForWriting := cfg.Database.GetConfigBatchSize() * 10

	processBatch := func() {
		if len(pairsToDoubleCheck) > 0 {
			tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("数据库二次过滤 %d 个可能重复的配对...", len(pairsToDoubleCheck))})
			newlyConfirmed, err := database.FilterExistingPairs(db, pairsToDoubleCheck, cfg.Database.GetConfigBatchSize())
			if err != nil {
				tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("数据库过滤错误: %v", err)})
			} else {
				for pair := range newlyConfirmed {
					confirmedNewPairs[pair] = true
				}
			}
			pairsToDoubleCheck = make(map[string]bool)
		}

		if len(confirmedNewPairs) > 0 {
			count := int64(len(confirmedNewPairs))
			newPairsFound += count
			tui.Send(tui.StatsUpdateMsg{NewPairsFound: newPairsFound, LogMessage: fmt.Sprintf("发现 %d 个新配对，准备写入...", count)})

			database.SaveNewPairsInBatches(db, cfg.Database, confirmedNewPairs)
			saveToOutputFile(confirmedNewPairs, cfg.Files.OutputLog)
			confirmedNewPairs = make(map[string]bool)

			_ = db.QueryRow("SELECT COUNT(*) FROM blacklist").Scan(&totalInDB)
		}
	}

	for {
		select {
		case pair, ok := <-extractedPairsChan:
			if !ok {
				processBatch()
				if db != nil {
					_ = db.QueryRow("SELECT COUNT(*) FROM blacklist").Scan(&totalInDB)
				}
				tui.Send(tui.StatsUpdateMsg{TotalInDB: totalInDB})
				tui.Send(tui.FinishedMsg{})
				return
			}
			pairsExtracted++
			if !bloomFilter.TestString(pair) {
				confirmedNewPairs[pair] = true
			} else {
				pairsToDoubleCheck[pair] = true
			}

			if len(pairsToDoubleCheck) >= batchSizeForDBCheck || len(confirmedNewPairs) >= batchSizeForWriting {
				processBatch()
			}

		case <-ticker.C:
			if db != nil {
				_ = db.QueryRow("SELECT COUNT(*) FROM blacklist").Scan(&totalInDB)
			}
			tui.Send(tui.StatsUpdateMsg{
				PairsExtracted: pairsExtracted,
				NewPairsFound:  newPairsFound,
				TotalInDB:      totalInDB,
			})
		}
	}
}

func collectAndFilterOriginal(extractedPairsChan <-chan string, cfg config.Config, db *sql.DB) {
	uniquePairs := make(map[string]bool)
	var pairsExtracted, newPairsFound, totalInDB int64
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	batchSizeForProcessing := cfg.Database.GetConfigBatchSize() * 10

	processBatch := func() {
		if len(uniquePairs) == 0 {
			return
		}
		tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("开始过滤 %d 个唯一配对...", len(uniquePairs))})
		newPairs, err := database.FilterExistingPairs(db, uniquePairs, cfg.Database.GetConfigBatchSize())
		if err != nil {
			tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("数据库过滤错误: %v", err)})
		} else {
			count := int64(len(newPairs))
			newPairsFound += count
			tui.Send(tui.StatsUpdateMsg{NewPairsFound: newPairsFound, LogMessage: fmt.Sprintf("发现 %d 个新配对，准备写入...", count)})
			database.SaveNewPairsInBatches(db, cfg.Database, newPairs)
			saveToOutputFile(newPairs, cfg.Files.OutputLog)
		}
		uniquePairs = make(map[string]bool)
		if db != nil {
			_ = db.QueryRow("SELECT COUNT(*) FROM blacklist").Scan(&totalInDB)
		}
	}

	for {
		select {
		case pair, ok := <-extractedPairsChan:
			if !ok {
				processBatch()
				if db != nil {
					_ = db.QueryRow("SELECT COUNT(*) FROM blacklist").Scan(&totalInDB)
				}
				tui.Send(tui.StatsUpdateMsg{TotalInDB: totalInDB})
				tui.Send(tui.FinishedMsg{})
				return
			}
			if !uniquePairs[pair] {
				uniquePairs[pair] = true
				pairsExtracted++
			}
			if len(uniquePairs) >= batchSizeForProcessing {
				processBatch()
			}
		case <-ticker.C:
			if db != nil {
				_ = db.QueryRow("SELECT COUNT(*) FROM blacklist").Scan(&totalInDB)
			}
			tui.Send(tui.StatsUpdateMsg{
				PairsExtracted: pairsExtracted,
				NewPairsFound:  newPairsFound,
				TotalInDB:      totalInDB,
			})
		}
	}
}

func saveToOutputFile(newPairs map[string]bool, outputFilePath string) {
	if len(newPairs) == 0 {
		return
	}

	outputFile, err := os.OpenFile(outputFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("错误: 打开输出文件失败: %v", err)})
		return
	}
	defer outputFile.Close()

	writer := bufio.NewWriter(outputFile)
	var sortedPairs []string
	for pair := range newPairs {
		sortedPairs = append(sortedPairs, pair)
	}
	sort.Strings(sortedPairs)

	for _, pair := range sortedPairs {
		_, _ = writer.WriteString(pair + "\n")
	}
	_ = writer.Flush()
}

// processZipArchive 处理 ZIP 归档
func processZipArchive(id int, zipPath string, extractedPairsChan chan<- string) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("错误: 打开ZIP文件 %s 失败: %v", zipPath, err)})
		return
	}
	defer r.Close()

	tui.Send(tui.StatsUpdateMsg{LogMessage: fmt.Sprintf("正在扫描ZIP归档: %s...", filepath.Base(zipPath))})

	for _, f := range r.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(f.Name), ".txt") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if isPotentialMatch(trimmed) {
				match := pairPattern.FindStringSubmatch(trimmed)
				if len(match) == 3 && len(match[2]) >= 8 {
					extractedPairsChan <- fmt.Sprintf("%s:%s", match[1], match[2])
				}
			}
		}
		rc.Close()
	}
}

// getDecompressedReader 根据后缀名获取相应的解压 Reader
func getDecompressedReader(filePath string) (io.ReadCloser, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".gz":
		return gzip.NewReader(file)
	case ".xz":
		xzReader, err := xz.NewReader(file)
		if err != nil {
			file.Close()
			return nil, err
		}
		return io.NopCloser(xzReader), nil
	case ".txt":
		return file, nil
	default:
		file.Close()
		return nil, fmt.Errorf("不支持的文件类型: %s", ext)
	}
}
