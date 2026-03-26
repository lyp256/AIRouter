package sqlite

import (
	"github.com/lyp256/airouter/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Init 初始化数据库
func Init(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	// 自动迁移
	if err := db.AutoMigrate(
		&model.User{},
		&model.UserKey{},
		&model.Provider{},
		&model.ProviderKey{},
		&model.Upstream{},
		&model.Model{},
		&model.UsageLog{},
	); err != nil {
		return nil, err
	}

	// 执行数据迁移
	if err := migrateProviderKeysTableName(db); err != nil {
		return nil, err
	}
	if err := migrateAPIPath(db); err != nil {
		return nil, err
	}

	return db, nil
}

// migrateProviderKeysTableName 迁移 provider_apikeys 表名和字段名
func migrateProviderKeysTableName(db *gorm.DB) error {
	// 检查旧表是否存在
	var hasOldTable int
	err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='provider_apikeys'").Scan(&hasOldTable).Error
	if err != nil {
		return err
	}

	// 检查新表是否已存在
	var hasNewTable int
	err = db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='provider_keys'").Scan(&hasNewTable).Error
	if err != nil {
		return err
	}

	// 如果旧表存在且新表不存在，执行表重命名
	if hasOldTable > 0 && hasNewTable == 0 {
		err = db.Exec("ALTER TABLE provider_apikeys RENAME TO provider_keys").Error
		if err != nil {
			return err
		}
	}

	// 重命名 upstreams 表中的列 provider_api_key_id -> provider_key_id
	var hasOldColumnInUpstreams int
	err = db.Raw("SELECT COUNT(*) FROM pragma_table_info('upstreams') WHERE name = 'provider_api_key_id'").Scan(&hasOldColumnInUpstreams).Error
	if err != nil {
		return err
	}
	if hasOldColumnInUpstreams > 0 {
		err = db.Exec("ALTER TABLE upstreams RENAME COLUMN provider_api_key_id TO provider_key_id").Error
		if err != nil {
			return err
		}
	}

	// 重命名 usage_logs 表中的列 provider_apikey_id -> provider_key_id
	var hasOldColumnInUsageLogs int
	err = db.Raw("SELECT COUNT(*) FROM pragma_table_info('usage_logs') WHERE name = 'provider_apikey_id'").Scan(&hasOldColumnInUsageLogs).Error
	if err != nil {
		return err
	}
	if hasOldColumnInUsageLogs > 0 {
		err = db.Exec("ALTER TABLE usage_logs RENAME COLUMN provider_apikey_id TO provider_key_id").Error
		if err != nil {
			return err
		}
	}

	return nil
}

// migrateAPIPath 将 api_path 从 upstreams 迁移到 providers
func migrateAPIPath(db *gorm.DB) error {
	// 检查 upstreams 表是否还有 api_path 列
	var hasColumn int
	err := db.Raw("SELECT COUNT(*) FROM pragma_table_info('upstreams') WHERE name = 'api_path'").Scan(&hasColumn).Error
	if err != nil {
		return err
	}

	if hasColumn == 0 {
		// 列已不存在，无需迁移
		return nil
	}

	// 将 upstreams.api_path 迁移到 providers.api_path
	// 对于同一个 provider，取第一个非空的 api_path
	err = db.Exec(`
		UPDATE providers SET api_path = (
			SELECT api_path FROM upstreams
			WHERE upstreams.provider_id = providers.id
			AND upstreams.api_path != ''
			AND upstreams.api_path IS NOT NULL
			LIMIT 1
		)
		WHERE EXISTS (
			SELECT 1 FROM upstreams
			WHERE upstreams.provider_id = providers.id
			AND upstreams.api_path != ''
			AND upstreams.api_path IS NOT NULL
		)
	`).Error
	if err != nil {
		return err
	}

	// 删除 upstreams 表的 api_path 列
	// SQLite 3.35.0+ 支持 ALTER TABLE DROP COLUMN
	// 检查 SQLite 版本是否支持
	var sqliteVersion string
	db.Raw("SELECT sqlite_version()").Scan(&sqliteVersion)

	// 如果版本 >= 3.35.0，直接删除列
	// 否则跳过（保留列但不使用）
	if sqliteVersion >= "3.35" {
		err = db.Exec("ALTER TABLE upstreams DROP COLUMN api_path").Error
		if err != nil {
			// 忽略删除失败的情况，不影响功能
			return nil
		}
	}

	return nil
}
