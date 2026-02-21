# PairScan

PairScan 是一个高效的邮箱:密码配对提取与去重工具，专门用于处理大量压缩文本文件（.txt, .gz, .xz, .zip），将提取的配对存储到数据库中并进行去重。

## 功能特性

- **多格式支持**：支持 .txt, .gz, .xz, .zip 格式的文本文件
- **智能处理模式**：
  - 单文件模式：处理单个文件
  - 文件夹模式：递归处理整个文件夹
  - 自动选择：根据文件大小自动选择内存模式或流式模式
- **高效去重**：
  - 使用布隆过滤器减少数据库查询（可选）
  - 批量插入优化性能
- **多种数据库支持**：
  - SQLite（本地，默认）
  - MySQL（远程，可选）
- **代理支持**：通过 SOCKS5 代理连接 MySQL
- **实时监控**：TUI 界面显示处理进度、系统资源使用情况
- **高并发处理**：基于 CPU 核心数的多线程处理

## 安装

### 前置要求

- Go 1.24.0 或更高版本
- （可选）MySQL 服务器用于远程数据库

### 编译

```bash
# 克隆或下载项目
cd C:\Users\liuasd\Desktop\ago\sql-cl\v8

# 下载依赖
go mod download

# 编译
go build -o pairscan main.go

# 或者运行
go run main.go
```

## 配置

创建或编辑 `config.yaml` 文件：

```yaml
database:
  # 是否使用远程 MySQL
  use_remote_mysql: false
  
  # MySQL 配置（仅在使用 MySQL 时需要）
  host: "127.0.0.1"
  port: 3306
  user: "your_mysql_user"
  password: "your_mysql_password"
  dbname: "blacklist_db"
  
  # SQLite 配置（默认使用）
  sqlite_db_path: "./blacklist.db"
  
  # 批量插入大小
  batch_size: 1000           # 默认批量大小
  mysql_batch_size: 20000    # MySQL 专用批量大小
  sqlite_batch_size: 5000    # SQLite 专用批量大小

proxy:
  # 是否启用代理
  enabled: false
  type: "socks5"             # 仅支持 socks5
  address: "127.0.0.1:1080"

files:
  # 输出日志文件
  output_log: "log-ts.txt"
```

### 环境变量（可选）

支持通过环境变量覆盖配置：

```bash
# 覆盖数据库密码
export DB_PASSWORD="your_secure_password"

# 覆盖 MySQL 主机
export DB_HOST="192.168.1.100"

# 覆盖代理地址
export PROXY_ADDRESS="127.0.0.1:1080"
```

## 使用方法

### 运行程序

```bash
./pairscan
# 或
go run main.go
```

### 选择处理模式

程序启动后会提示选择处理模式：

1. **单文件模式**：处理单个文件
2. **文件夹模式**：处理整个文件夹

### 界面说明

程序运行时会显示 TUI（终端图形界面）：

- **状态信息**：当前处理路径、处理模式、运行状态
- **统计信息**：文件进度、提取的配对数、新增配对数、数据库总数
- **资源监控**：CPU 使用率、内存使用情况
- **进度条**：显示处理进度
- **日志**：实时显示处理日志

### 快捷键

- `q` 或 `Ctrl+C`：退出程序

## 项目结构

```
.
├── main.go          # 程序入口
├── config.go        # 配置管理
├── database.go      # 数据库操作
├── processor.go     # 文件处理逻辑
├── files.go         # 文件扫描
├── bloom.go         # 布隆过滤器
├── tui.go           # 终端界面
├── config.yaml      # 配置文件
└── tests/           # 测试文件
```

## 配置说明

### 批量大小设置

- **SQLite**：建议 5000，SQLite 对大批量插入处理较慢
- **MySQL**：建议 20000，支持更大的批量插入
- **默认值**：1000 适用于所有情况

布隆过滤器误报率设置为 0.001 (0.1%)，提供性能和精度的良好平衡。

### 处理模式选择

- **文件总大小 < 2GB**：使用高速内存模式，快速处理
- **文件总大小 >= 2GB**：使用流式处理模式，节省内存使用

## 性能优化

1. **WAL 模式**：SQLite 使用 WAL 模式提高并发性能
2. **连接池**：数据库使用连接池，最大连接数 25
3. **批量插入**：使用事务批量插入，减少网络 I/O
4. **布隆过滤器**：减少数据库查询次数（MySQL 模式）
5. **多线程**：基于 CPU 核心数的并发处理

## 测试

运行测试：

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./config
go test ./database
go test ./processor

# 运行测试并显示覆盖率
go test -cover ./...

# 查看详细覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 正则表达式说明

程序使用以下正则表达式匹配邮箱:密码配对：

```regex
^(?:https?:\/\/|android:\/\/.*?:|[^/:]+\.[^/:]+:)?.*?([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})\s*:\s*([^:]+)$
```

匹配格式示例：
- `user@example.com:password123`
- `https://site.com:8080/user@example.com:password123`
- `user@example.com : password123`

**注意**：密码长度必须 >= 8 个字符

## 故障排除

### SQLite 连接错误

确保程序有权限读写数据库文件和目录。

### MySQL 连接错误

1. 检查 MySQL 服务器是否运行
2. 确认用户名和密码正确
3. 检查防火墙设置
4. 如使用代理，确认代理地址正确

### 内存不足

使用文件夹模式时，程序会自动选择流式处理模式。如仍遇到内存问题，尝试减少批量大小。

## 许可证

本项目仅供学习和研究使用。

## 作者

PairScan Development Team
