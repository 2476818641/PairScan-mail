# PairScan-mail

PairScan-mail 是一个高效的邮件日志数据处理工具，专门用于扫描、解析和去重大型邮件日志文件。

## 功能特性

- 支持多种文件格式：`.txt`, `.gz`, `.xz`, `.zip`
- 双模式处理：
  - 单文件处理
  - 文件夹批量处理
- 智能处理策略：
  - **高速内存模式**：文件总大小 < 2GB 时自动启用
  - **流式处理模式**：文件总大小 ≥ 2GB 时自动使用
- 多数据库支持：
  - SQLite（本地）
  - MySQL（远程）
  - PostgreSQL（远程）
- 布隆过滤器优化：减少远程 MySQL/PostgreSQL 查询次数，提升去重性能
- 美观的终端用户界面（TUI）：实时显示处理进度、统计信息

## 快速开始

### 环境要求

- Go 1.24.0+

### 安装

```bash
git clone https://github.com/2476818641/PairScan-mail.git
cd PairScan-mail
go mod download
go build -o PairScan-mail
```

### 配置

 编辑 `config.yaml` 文件：

```yaml
database:
  db_type: "sqlite"           # 数据库类型: sqlite, mysql, postgres
  
  # SQLite 配置（默认使用）
  sqlite_db_path: "./blacklist.db"
  
  # MySQL 配置（仅在使用 MySQL 时需要）
  # db_type: "mysql"
  # host: "127.0.0.1"
  # port: 3306
  # user: "your_mysql_user"
  # password: "your_mysql_password"
  # dbname: "blacklist_db"
  
  # PostgreSQL 配置（仅在使用 PostgreSQL 时需要）
  # db_type: "postgres"
  # host: "127.0.0.1"
  # port: 5432
  # user: "your_postgres_user"
  # password: "your_postgres_password"
  # dbname: "blacklist_db"
  # ssl_mode: "disable"
```

**使用环境变量（推荐用于敏感信息）：**

```bash
# Linux/Mac
export DB_TYPE="postgres"
export DB_PASSWORD="your_password"

# Windows
set DB_TYPE=postgres
set DB_PASSWORD=your_password
```

### 运行

```bash
./PairScan-mail
```

按提示选择处理模式并输入文件路径。

## 技术栈

- 编程语言：Go
- UI 框架：Bubbletea (charmbracelet)
- 数据库：SQLite3 / MySQL / PostgreSQL
- 优化算法：布隆过滤器 (Bloom Filter)

## 许可证

[MIT License](https://opensource.org/licenses/MIT)
