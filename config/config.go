package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ============================================
// 常量定义
// ============================================

const (
	InMemoryThreshold      = 2 * 1024 * 1024 * 1024 // 2GB - 处理模式选择阈值
	MinPasswordLength      = 8                      // 最小密码长度要求
	BloomFalsePositiveRate = 0.001                  // 布隆过滤器误报率 0.1%
)

// ============================================
// 配置结构体
// ============================================

// Config defines the application's entire configuration.
type Config struct {
	Database DBConfig    `yaml:"database"`
	Proxy    ProxyConfig `yaml:"proxy"`
	Files    FileConfig  `yaml:"files"`
}

// DBConfig holds all database-related settings.
type DBConfig struct {
	UseRemoteMySQL  bool   `yaml:"use_remote_mysql"`
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	DBName          string `yaml:"dbname"`
	SQLiteDBPath    string `yaml:"sqlite_db_path"`
	BatchSize       int    `yaml:"batch_size"`
	MySQLBatchSize  int    `yaml:"mysql_batch_size"`
	SQLiteBatchSize int    `yaml:"sqlite_batch_size"`
}

// ProxyConfig holds proxy settings.
type ProxyConfig struct {
	Enabled bool   `yaml:"enabled"`
	Type    string `yaml:"type"`
	Address string `yaml:"address"`
}

// FileConfig holds file-related settings.
type FileConfig struct {
	OutputLog string `yaml:"output_log"`
}

// GetConfigBatchSize selects the appropriate batch size based on the current database.
func (dbc DBConfig) GetConfigBatchSize() int {
	if dbc.UseRemoteMySQL {
		if dbc.MySQLBatchSize > 0 {
			return dbc.MySQLBatchSize
		}
	} else {
		if dbc.SQLiteBatchSize > 0 {
			return dbc.SQLiteBatchSize
		}
	}
	if dbc.BatchSize > 0 {
		return dbc.BatchSize
	}
	return 1000 // Default fallback
}

// Load reads the YAML configuration file from a given path.
func Load(path string) (Config, error) {
	var cfg Config
	f, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, createDefaultConfig(path)
		}
		return cfg, fmt.Errorf("读取配置文件失败: %w", err)
	}

	err = yaml.Unmarshal(f, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("解析配置文件失败: %w", err)
	}

	applyDefaults(&cfg)
	applyEnvironmentOverrides(&cfg)

	// Ensure SQLite directory exists
	if !cfg.Database.UseRemoteMySQL && cfg.Database.SQLiteDBPath != "" {
		dbDir := filepath.Dir(cfg.Database.SQLiteDBPath)
		if dbDir != "." && dbDir != "" {
			if err := os.MkdirAll(dbDir, 0755); err != nil {
				fmt.Printf("警告: 无法创建 SQLite 数据库目录 '%s': %v\n", dbDir, err)
			}
		}
	}
	return cfg, nil
}

// applyDefaults sets sane defaults for missing configuration values.
func applyDefaults(cfg *Config) {
	if cfg.Database.BatchSize <= 0 {
		cfg.Database.BatchSize = 1000
	}
	if cfg.Database.MySQLBatchSize <= 0 {
		cfg.Database.MySQLBatchSize = cfg.Database.BatchSize
	}
	if cfg.Database.SQLiteBatchSize <= 0 {
		cfg.Database.SQLiteBatchSize = cfg.Database.BatchSize
	}
	// 设置默认代理类型
	if cfg.Proxy.Enabled && cfg.Proxy.Type == "" {
		cfg.Proxy.Type = "socks5"
	}
}

// applyEnvironmentOverrides applies environment variable overrides to the configuration.
// Supported environment variables:
// - DB_HOST: Override MySQL host
// - DB_PORT: Override MySQL port
// - DB_USER: Override MySQL user
// - DB_PASSWORD: Override MySQL password
// - DB_NAME: Override MySQL database name
// - SQLITE_PATH: Override SQLite database path
// - PROXY_ENABLED: Enable/disable proxy (true/false)
// - PROXY_TYPE: Override proxy type
// - PROXY_ADDRESS: Override proxy address
// - OUTPUT_LOG: Override output log file path
func applyEnvironmentOverrides(cfg *Config) {
	if host := os.Getenv("DB_HOST"); host != "" {
		cfg.Database.Host = host
	}
	if port := os.Getenv("DB_PORT"); port != "" {
		var portNum int
		_, err := fmt.Sscanf(port, "%d", &portNum)
		if err == nil && portNum > 0 {
			cfg.Database.Port = portNum
		}
	}
	if user := os.Getenv("DB_USER"); user != "" {
		cfg.Database.User = user
	}
	if password := os.Getenv("DB_PASSWORD"); password != "" {
		cfg.Database.Password = password
	}
	if dbname := os.Getenv("DB_NAME"); dbname != "" {
		cfg.Database.DBName = dbname
	}
	if sqlitePath := os.Getenv("SQLITE_PATH"); sqlitePath != "" {
		cfg.Database.SQLiteDBPath = sqlitePath
	}
	if proxyEnabled := os.Getenv("PROXY_ENABLED"); proxyEnabled != "" {
		cfg.Proxy.Enabled = strings.ToLower(proxyEnabled) == "true"
	}
	if proxyType := os.Getenv("PROXY_TYPE"); proxyType != "" {
		cfg.Proxy.Type = proxyType
	}
	if proxyAddress := os.Getenv("PROXY_ADDRESS"); proxyAddress != "" {
		cfg.Proxy.Address = proxyAddress
	}
	if outputLog := os.Getenv("OUTPUT_LOG"); outputLog != "" {
		cfg.Files.OutputLog = outputLog
	}
}

// createDefaultConfig writes a template config file if one does not exist.
func createDefaultConfig(path string) error {
	fmt.Printf("配置文件 '%s' 未找到，正在创建一个默认模板。\n", path)
	fmt.Println("请根据你的实际情况修改该文件中的数据库和代理信息。")
	fmt.Println("提示: 可以使用环境变量覆盖敏感配置（如 DB_PASSWORD）")
	defaultConfig := `
# ============================================
# PairScan 配置文件
# ============================================
# 
# 说明：
# 1. 可以直接修改此文件，或使用环境变量覆盖（推荐用于敏感信息）
# 2. 支持的环境变量：
#    - DB_PASSWORD: 覆盖 MySQL 密码
#    - DB_HOST: 覆盖 MySQL 主机
#    - DB_PORT: 覆盖 MySQL 端口
#    - DB_USER: 覆盖 MySQL 用户名
#    - DB_NAME: 覆盖 MySQL 数据库名
#    - SQLITE_PATH: 覆盖 SQLite 路径
#    - PROXY_ENABLED: 覆盖代理启用状态 (true/false)
#    - PROXY_ADDRESS: 覆盖代理地址
#    - OUTPUT_LOG: 覆盖输出日志文件路径
# 3. batch_size, mysql_batch_size, sqlite_batch_size 说明：
#    - batch_size: 通用默认批量大小（值 ≤ 0 表示不设置，使用默认值）
#    - mysql_batch_size: MySQL 数据库专用批量大小（值 ≤ 0 表示不设置，使用 batch_size）
#    - sqlite_batch_size: SQLite 数据库专用批量大小（值 ≤ 0 表示不设置，使用 batch_size）

# ============================================
# 数据库配置
# ============================================
database:
  # 是否使用远程 MySQL
  use_remote_mysql: false
  
  # MySQL 配置（仅在使用 MySQL 时需要）
  host: "127.0.0.1"
  port: 3306
  user: "your_mysql_user"
  password: "your_mysql_password"  # 提示: 可通过环境变量 DB_PASSWORD 覆盖
  dbname: "blacklist_db"
  
  # SQLite 配置（默认使用）
  sqlite_db_path: "./blacklist.db"  # 提示: 可通过环境变量 SQLITE_PATH 覆盖
  
  # 批量插入大小配置
  batch_size: 1000           # 默认批量大小
  mysql_batch_size: 20000    # MySQL 专用批量大小（建议值）
  sqlite_batch_size: 5000    # SQLite 专用批量大小（建议值）

# ============================================
# 代理配置
# ============================================
proxy:
  # 是否启用代理（仅用于连接 MySQL）
  enabled: false              # 提示: 可通过环境变量 PROXY_ENABLED 覆盖
  type: "socks5"              # 目前仅支持 socks5
  address: "127.0.0.1:1080"  # 提示: 可通过环境变量 PROXY_ADDRESS 覆盖

# ============================================
# 文件配置
# ============================================
files:
  # 新发现的配对将追加到此文件
  output_log: "log-ts.txt"   # 提示: 可通过环境变量 OUTPUT_LOG 覆盖
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(defaultConfig)), 0644); err != nil {
		return fmt.Errorf("创建默认配置文件 '%s' 失败: %w", path, err)
	}
	return fmt.Errorf("请先配置好 '%s' 文件，然后重新启动程序", path)
}
