package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// backupPrefix 是备份文件名的固定前缀，与时间戳共同构成备份文件名。
const backupPrefix = "aurora-"

// backupTimeLayout 嵌入备份文件名的时间戳格式，保证字典序 == 时间序，
// 保留策略据此排序取最新 N 份即可，无需再解析文件名。
const backupTimeLayout = "20060102-150405"

// BackupTo 将数据库在线备份到 dir 下，文件名形如 aurora-20260810-153000.db。
//
// 使用 SQLite 的 VACUUM INTO：它在数据库外生成一份经过压缩的完整副本，
// WAL 模式下的在线备份不需要停服，也不影响当前进程的读写。VACUUM INTO
// 要求目标文件不存在，因此先备份到临时文件再 rename，避免并发触发时
// 因目标已存在而报错（重试同一时间戳第二次会失败）。
//
// 备份完成后按"保留最近 maxKeep 份"清理更旧的备份。maxKeep <= 0 视为
// 不清理（保留全部）。
func (r *Database) BackupTo(dir string, maxKeep int) (string, error) {
	if r == nil || r.DB == nil {
		return "", fmt.Errorf("database is not open")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}

	ts := time.Now().Format(backupTimeLayout)
	// 同秒内多次备份：先去掉可能已存在的同名文件，避免 VACUUM INTO 报错。
	final := filepath.Join(dir, backupPrefix+ts+".db")
	_ = os.Remove(final)

	tmp, err := os.CreateTemp(dir, backupPrefix+"tmp-*.db")
	if err != nil {
		return "", fmt.Errorf("create temp backup: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	_ = os.Remove(tmpPath) // VACUUM INTO 要求目标不存在

	if err := r.DB.Exec(fmt.Sprintf("VACUUM INTO '%s'", escapeSQLiteString(tmpPath))).Error; err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("vacuum into backup failed: %w", err)
	}
	if err := os.Rename(tmpPath, final); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("finalize backup failed: %w", err)
	}

	if maxKeep > 0 {
		// 备份本身已成功：保留策略清理失败只吞掉，绝不能把
		// 成功路径变成 API 层的 500（否则调用方会以为备份失败，
		// 实际文件已落盘，且自升级前的自动备份会被误伤）。
		_ = r.pruneBackups(dir, maxKeep)
	}
	return final, nil
}

// pruneBackups 删除目录下超过 maxKeep 份的旧备份文件。
// 只清理本产品命名约定（aurora-*.db）的文件，不碰其它内容。
func (r *Database) pruneBackups(dir string, maxKeep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read backup dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), backupPrefix) && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // 字典序 == 时间序（时间戳格式固定）

	excess := len(names) - maxKeep
	if excess <= 0 {
		return nil
	}
	for _, n := range names[:excess] {
		_ = os.Remove(filepath.Join(dir, n))
	}
	return nil
}

// escapeSQLiteString 把字符串转义成可安全嵌入单引号 SQL 字面量的形式。
// 路径来自本进程生成，但路径可以包含单引号（Windows 允许），
// 转义后避免注入面（把单引号双写即可，VACUUM INTO 不支持参数绑定）。
func escapeSQLiteString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
