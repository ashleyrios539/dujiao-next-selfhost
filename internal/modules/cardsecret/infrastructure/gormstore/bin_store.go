package gormstore

import (
	"errors"
	"strings"
	"time"

	cardsecretcontract "github.com/dujiao-next/internal/modules/cardsecret/contract"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BatchStore 是卡密导入批次端口的 GORM 实现。
var _ cardsecretcontract.CardBinRepository = (*BinStore)(nil)

// BinStore 是 BIN 库端口的 GORM 实现。
type BinStore struct {
	db *gorm.DB
}

// NewBin 创建 BIN 库存储。
func NewBin(db *gorm.DB) *BinStore {
	return &BinStore{db: db}
}

// UpsertBins 批量写入 BIN 条目（存在则覆盖属性）。
func (r *BinStore) UpsertBins(bins []cardsecretdomain.CardBin) error {
	if len(bins) == 0 {
		return nil
	}
	now := time.Now()
	for i := range bins {
		bins[i].UpdatedAt = now
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "bin"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"country", "brand", "raw_brand", "card_type", "issuer", "updated_at",
		}),
	}).CreateInBatches(&bins, 500).Error
}

// FindByBins 按前 6 位查询 BIN 条目。
func (r *BinStore) FindByBins(bins []string) ([]cardsecretdomain.CardBin, error) {
	if len(bins) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(bins))
	for _, bin := range bins {
		trimmed := strings.TrimSpace(bin)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	var rows []cardsecretdomain.CardBin
	if err := r.db.Where("bin IN ?", normalized).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Count 统计 BIN 库条目总数。
func (r *BinStore) Count() (int64, error) {
	var count int64
	if err := r.db.Model(&cardsecretdomain.CardBin{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteAll 清空 BIN 库。
func (r *BinStore) DeleteAll() error {
	return r.db.Where("1 = 1").Delete(&cardsecretdomain.CardBin{}).Error
}

// List 查询 BIN 库列表。
func (r *BinStore) List(filter cardsecretcontract.CardBinFilter) ([]cardsecretdomain.CardBin, int64, error) {
	if r.db == nil {
		return nil, 0, errors.New("nil database")
	}
	query := r.db.Model(&cardsecretdomain.CardBin{})
	if country := strings.TrimSpace(filter.Country); country != "" {
		query = query.Where("country = ?", strings.ToUpper(country))
	}
	if brand := strings.TrimSpace(filter.Brand); brand != "" {
		query = query.Where("brand = ?", brand)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		query = query.Where("(bin LIKE ? OR raw_brand LIKE ? OR issuer LIKE ?)",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	var rows []cardsecretdomain.CardBin
	if err := query.Order("bin asc").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
