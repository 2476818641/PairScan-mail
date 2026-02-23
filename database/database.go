package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"PairScan/config"

	mySQLDriver "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/proxy"
)

// Init 根据配置初始化并返回数据库连接。
func Init(cfg config.Config) (*sql.DB, error) {
	var db *sql.DB
	var err error
	dbCfg := cfg.Database

	dbType := dbCfg.GetDBType()

	switch dbType {
	case config.DBTypeMySQL:
		if cfg.Proxy.Enabled && cfg.Proxy.Type == "socks5" {
			dialer, err := proxy.SOCKS5("tcp", cfg.Proxy.Address, nil, proxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("创建SOCKS5代理失败: %w", err)
			}
			mySQLDriver.RegisterDialContext("tcp", func(_ context.Context, addr string) (net.Conn, error) {
				return dialer.Dial("tcp", addr)
			})
		}
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&compress=true",
			dbCfg.User, dbCfg.Password, dbCfg.Host, dbCfg.Port, dbCfg.DBName)
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("连接MySQL失败: %w", err)
		}
		createTableSQL := `CREATE TABLE IF NOT EXISTS blacklist (pair VARCHAR(512) PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`
		_, err = db.Exec(createTableSQL)
		if err != nil {
			return nil, fmt.Errorf("在MySQL中创建表失败: %w", err)
		}
		fmt.Println("成功连接到 MySQL。")

	case config.DBTypePostgres:
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			dbCfg.Host, dbCfg.Port, dbCfg.User, dbCfg.Password, dbCfg.DBName, dbCfg.SSLMode)
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			return nil, fmt.Errorf("连接PostgreSQL失败: %w", err)
		}
		createTableSQL := `CREATE TABLE IF NOT EXISTS blacklist (pair TEXT PRIMARY KEY);`
		_, err = db.Exec(createTableSQL)
		if err != nil {
			return nil, fmt.Errorf("在PostgreSQL中创建表失败: %w", err)
		}
		fmt.Println("成功连接到 PostgreSQL。")

	case config.DBTypeSQLite:
		dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-20000", dbCfg.SQLiteDBPath)
		db, err = sql.Open("sqlite3", dsn)
		if err != nil {
			return nil, fmt.Errorf("连接SQLite失败: %w", err)
		}
		createTableSQL := `CREATE TABLE IF NOT EXISTS blacklist (pair TEXT PRIMARY KEY);`
		_, err = db.Exec(createTableSQL)
		if err != nil {
			return nil, fmt.Errorf("在SQLite中创建表失败: %w", err)
		}
		fmt.Println("成功连接到 本地SQLite。")

	default:
		return nil, fmt.Errorf("未知的数据库类型: %s", dbType)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库Ping失败: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return db, nil
}

// SaveNewPairsInBatches 使用事务将新配对批量保存到数据库。
func SaveNewPairsInBatches(db *sql.DB, cfg config.DBConfig, newPairs map[string]bool) {
	if db == nil || len(newPairs) == 0 {
		return
	}

	// 将 map 转换为排序后的切片，确保插入顺序一致，减少数据库索引分裂
	var newPairList []string
	for pair := range newPairs {
		newPairList = append(newPairList, pair)
	}
	sort.Strings(newPairList)

	batchSize := cfg.GetConfigBatchSize()
	totalNew := len(newPairList)

	tx, err := db.Begin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "开启事务失败: %v\n", err)
		return
	}

	dbType := cfg.GetDBType()
	var stmtTpl string
	switch dbType {
	case config.DBTypeMySQL:
		stmtTpl = "INSERT IGNORE INTO blacklist (pair) VALUES "
	case config.DBTypePostgres:
		stmtTpl = "INSERT INTO blacklist (pair) VALUES "
	case config.DBTypeSQLite:
		stmtTpl = "INSERT OR IGNORE INTO blacklist (pair) VALUES "
	}

	for i := 0; i < totalNew; i += batchSize {
		end := i + batchSize
		if end > totalNew {
			end = totalNew
		}
		batch := newPairList[i:end]
		if len(batch) == 0 {
			continue
		}

		placeholders := strings.Repeat("(?),", len(batch)-1) + "(?)"
		args := make([]interface{}, len(batch))
		for j, v := range batch {
			args[j] = v
		}

		var fullSQL string
		if dbType == config.DBTypePostgres {
			fullSQL = stmtTpl + placeholders + " ON CONFLICT (pair) DO NOTHING"
		} else {
			fullSQL = stmtTpl + placeholders
		}

		_, err := tx.Exec(fullSQL, args...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n批量插入数据失败: %v\n", err)
			_ = tx.Rollback()
			return
		}
	}

	if err = tx.Commit(); err != nil {
		fmt.Fprintf(os.Stderr, "提交事务失败: %v\n", err)
	}
}

// FilterExistingPairs 检查哪些配对已经存在于数据库中，并返回数据库中不存在的新配对。
func FilterExistingPairs(db *sql.DB, pairsToCheck map[string]bool, batchSize int) (map[string]bool, error) {
	if db == nil {
		return nil, fmt.Errorf("数据库连接未建立")
	}
	if len(pairsToCheck) == 0 {
		return make(map[string]bool), nil
	}

	var allPairs []string
	for pair := range pairsToCheck {
		allPairs = append(allPairs, pair)
	}
	existingPairs := make(map[string]bool)

	// 分批查询以避免超出数据库的占位符限制（如 SQLite 默认 999）
	for i := 0; i < len(allPairs); i += batchSize {
		end := i + batchSize
		if end > len(allPairs) {
			end = len(allPairs)
		}
		batch := allPairs[i:end]
		if len(batch) == 0 {
			continue
		}

		placeholders := strings.Repeat("?,", len(batch)-1) + "?"
		query := fmt.Sprintf("SELECT pair FROM blacklist WHERE pair IN (%s)", placeholders)
		args := make([]interface{}, len(batch))
		for j, v := range batch {
			args[j] = v
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("数据库批量检查失败: %w", err)
		}

		for rows.Next() {
			var existingPair string
			if err := rows.Scan(&existingPair); err != nil {
				fmt.Fprintf(os.Stderr, "\n扫描查询结果行时出错: %v\n", err)
				continue
			}
			existingPairs[existingPair] = true
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, fmt.Errorf("遍历查询结果后出错: %w", err)
		}
	}

	// 找出那些在 existingPairs 映射表中不存在的配对
	newPairs := make(map[string]bool)
	for pair := range pairsToCheck {
		if !existingPairs[pair] {
			newPairs[pair] = true
		}
	}
	return newPairs, nil
}
