# PairScan v8.1 - Agent Development Guide

## Build, Lint, and Test Commands

### Build
```bash
# Build for current platform
go build -o pairscan main.go

# Build with optimizations
go build -ldflags="-s -w" -o pairscan main.go

# Cross-compile (example: Windows)
GOOS=windows GOARCH=amd64 go build -o pairscan.exe main.go

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o pairscan main.go
```

### Lint
```bash
# Run gofmt to check formatting
gofmt -l .

# Auto-format all code
gofmt -w .

# Run go vet for static analysis
go vet ./...

# Run golint if installed
golint ./...
```

### Test
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage report
go test -cover ./...

# Run coverage with detailed percentage
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Run tests for a specific package
go test ./config
go test ./database
go test ./processor

# Run a single specific test
go test -v ./processor -run TestProcessLineWorker

# Run tests with race condition detection
go test -race ./...

# Run benchmark tests
go test -bench=. -benchmem ./...
```

### Dependency Management
```bash
# Download dependencies
go mod download

# Tidy up go.mod
go mod tidy

# Verify dependencies
go mod verify

# Add a new dependency
go get github.com/example/package
```

## Code Style Guidelines

### Import Organization
Imports must be grouped in this exact order:
1. Standard library imports
2. Third-party imports
3. Local imports (PairScan/*)

Example:
```go
import (
    "context"
    "fmt"
    "time"                      // Standard library

    "github.com/lib/pq"         // Third-party imports
    _ "github.com/mattn/go-sqlite3"

    "PairScan/config"           // Local imports
    "PairScan/database"
)
```

### Naming Conventions
- **Package names**: Short, lowercase, single word (e.g., `config`, `database`, `processor`)
- **Exported functions/variables**: PascalCase (first letter capitalized)
- **Private functions/variables**: camelCase (first letter lowercase)
- **Constants**: PascalCase or UPPER_SNAKE_CASE for important values
- **Interfaces**: Should be named with `-er` suffix where applicable
- **Acronyms**: Use all caps (URL, HTTP, TUI) or all lowercase (db, id) consistently

### Error Handling
- **Never ignore errors**: Avoid using `_` for error returns unless explicitly documented
- **Wrap errors**: Use `fmt.Errorf` with `%w` verb to wrap errors with context
- **Provide context**: Error messages should explain what operation failed and why
- **Check all returns**: Always check function return values, especially for I/O operations

Example:
```go
// Good
if err := os.Open(path); err != nil {
    return nil, fmt.Errorf("failed to open file %s: %w", path, err)
}

// Bad - ignoring error
file, _ := os.Open(path)
```

### Documentation Comments
- **Exported functions**: Must have documentation comments explaining purpose, parameters, and return values
- **Comments language**: Use Chinese for user-facing comments, English for technical comments where appropriate
- **Thread safety**: Document concurrent behavior for functions that use goroutines
- **Examples**: Provide usage examples for complex functions

Example:
```go
// FilterExistingPairs 检查哪些配对已经存在于数据库中，并返回数据库中不存在的新配对。
// 并发安全：此函数是非阻塞的，调用者需要确保 db 参数在使用期间不被关闭。
// batchSize 参数建议值：MySQL 20000, PostgreSQL 10000, SQLite 5000
func FilterExistingPairs(db *sql.DB, pairsToCheck map[string]bool, batchSize int) (map[string]bool, error) {
    // implementation
}
```

### Concurrent Programming
- **Channel ownership**: Always document which goroutine owns a channel (read-only/write-only)
- **Avoid shared mutable state**: Use channels for communication, not shared memory
- **Use pattern**: Producer-consumer pattern with explicit channels
- **Documentation**: Add Chinese comments explaining thread-safety guarantees

Example:
```go
// processFileWorker 处理单个文件的 Worker goroutine
// 并发安全：每个 Worker 只读取自己的数据，通过 channels 与其他 goroutines 通信
// - filePathsChan: 文件路径输入通道（只读）
// - extractedPairsChan: 提取的配对输出通道（只写）
// - wg: WaitGroup 指针，用于通知完成
func processFileWorker(id int, filePathsChan <-chan string, extractedPairsChan chan<- string, wg *sync.WaitGroup) {
    defer wg.Done()
    // implementation
}
```

### Constants and Configuration
- **Define constants** for all magic numbers and configuration values
- **Centralize configuration**: Use `config` package for application-wide settings
- **Batch sizes**: Use appropriate defaults per database type (MySQL: 20000, PostgreSQL: 10000, SQLite: 5000)
- **Thresholds**: Define in `config` package (e.g., `InMemoryThreshold = 2GB`)

Example:
```go
const (
    InMemoryThreshold      = 2 * 1024 * 1024 * 1024  // 2GB
    MinPasswordLength      = 8                        // Minimum password length
    BloomFalsePositiveRate = 0.001                    // 0.1% false positive rate
)
```

### Type System
- **Use type aliases** for domain-specific types (e.g., `DBType` with values `sqlite`, `mysql`, `postgres`)
- **Strong typing**: Prefer explicit types over empty interfaces
- **Pointer semantics**: Use pointers for large structs or when mutability is needed

### Database-Specific Guidelines
- **Database abstraction**: Always use `cfg.GetDBType()` to determine database type
- **SQL dialect differences**: 
  - MySQL: `INSERT IGNORE`
  - PostgreSQL: `INSERT ... ON CONFLICT DO NOTHING`
  - SQLite: `INSERT OR IGNORE`
- **Batch insertion**: Use parameterized queries with prepared statements to prevent SQL injection
- **Connection pooling**: Keep connection pool limits reasonable (MaxOpenConns: 25, MaxIdleConns: 25)

### File Structure
- **One main file per package**: `config/config.go`, `database/database.go`
- **Test files**: `*_test.go` next to implementation files
- **Module path**: Use `PairScan` as the module name in go.mod

### Performance Considerations
- **Streaming for large files**: Use streaming processing for files ≥ 2GB
- **In-memory for small files**: Load entire files into memory when < 2GB
- **Bloom filters**: Use for remote databases (MySQL/PostgreSQL) to reduce queries
- **Batch operations**: Always batch database operations using configured batch sizes

### Project-Specific Rules
1. **Module name**: Always use `PairScan` as the module prefix
2. **Three database support**: Code must support SQLite, MySQL, and PostgreSQL
3. **Environment variables**: Support environment variable overrides for sensitive config (passwords)
4. **TUI communication**: Use `tui.Send()` for all TUI communications from background goroutines
5. **Resource monitoring**: Monitor CPU and memory usage and display in TUI

### Security
- **Never commit secrets**: Database passwords should be in environment variables, not config.yaml
- **SQL injection prevention**: Always use parameterized queries
- **File validation**: Always validate file paths and check file existence/accessibility
- **Error messages**: Avoid exposing system paths or sensitive information in error messages

### Testing Guidelines
- **Write tests for all public functions**: Each exported function should have test coverage
- **Test error cases**: Always test error paths and edge cases
- **Use table-driven tests**: For multiple similar test cases
- **Mock heavy dependencies**: For database interactions, use in-memory databases or mocks
