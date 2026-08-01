package application

import (
	"encoding/csv"
	"errors"
	"io"
	"mime/multipart"
	"sort"
	"strings"

	cardsecretcontract "github.com/dujiao-next/internal/modules/cardsecret/contract"
	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	cardcheck "github.com/dujiao-next/internal/upstream/cardcheck"
)

// 卡密服务错误。
var (
	ErrNotFound            = errors.New("not found")
	ErrInsufficient        = errors.New("card secret insufficient")
	ErrInvalid             = errors.New("card secret invalid")
	ErrCreateFailed        = errors.New("card secret create failed")
	ErrFetchFailed         = errors.New("card secret fetch failed")
	ErrUpdateFailed        = errors.New("card secret update failed")
	ErrDeleteFailed        = errors.New("card secret delete failed")
	ErrBatchCreateFailed   = errors.New("card secret batch create failed")
	ErrBatchFetchFailed    = errors.New("card secret batch fetch failed")
	ErrImportFailed        = errors.New("card secret import failed")
	ErrStatsFailed         = errors.New("card secret stats failed")
	ErrProductFetchFailed  = errors.New("product fetch failed")
	ErrProductNotFound     = errors.New("product not found")
	ErrProductSKURequired  = errors.New("product sku required")
	ErrProductSKUInvalid   = errors.New("product sku invalid")
	ErrBinStoreUnavailable = errors.New("bin store unavailable")
	ErrBinImportFailed     = errors.New("bin import failed")
	ErrBinFetchFailed      = errors.New("bin fetch failed")
	ErrBinDeleteFailed     = errors.New("bin delete failed")
)

// BinColumnMap 描述 BIN CSV 列名到字段的映射。
type BinColumnMap struct {
	BIN     string `json:"bin"`
	Country string `json:"country"`
	Brand   string `json:"brand"`
	Type    string `json:"type"`
	Prepaid string `json:"prepaid"`
}

// BinTypeRule 描述「BIN 库种类列值 → 挑卡种类三值」的显式映射规则。
type BinTypeRule map[string]string

// DefaultBinColumnMap 默认列映射（适配常见 BIN 库表头）。
func DefaultBinColumnMap() BinColumnMap {
	return BinColumnMap{
		BIN:     "BIN",
		Country: "isoCode2",
		Brand:   "Brand",
		Type:    "Type",
		Prepaid: "Category",
	}
}

// DefaultBinTypeRules 默认显式种类映射：Credit/Charge 直接标记为 C。
func DefaultBinTypeRules() BinTypeRule {
	return BinTypeRule{
		"CREDIT": cardsecretdomain.CardTypeC,
		"CHARGE": cardsecretdomain.CardTypeC,
	}
}

// DefaultPrepaidKeywords 默认「含预付」标记：命中即归入 D（含预付）。
func DefaultPrepaidKeywords() []string {
	return []string{"PREPAID"}
}

// ImportCardBinsInput BIN 库导入输入。
type ImportCardBinsInput struct {
	File             *multipart.FileHeader
	ColumnMap        *BinColumnMap
	TypeRules        *BinTypeRule
	PrepaidKeywords  []string
}

// ImportCardBinsResult BIN 库导入结果。
type ImportCardBinsResult struct {
	Total     int      `json:"total"`
	Inserted  int      `json:"inserted"`
	Skipped   int      `json:"skipped"`
	Countries []string `json:"countries"`
}

// ImportCardBins 解析 BIN 库 CSV 并全量重建（upsert）BIN 表。
func (s *Service) ImportCardBins(input ImportCardBinsInput) (*ImportCardBinsResult, error) {
	if s.binRepo == nil {
		return nil, ErrBinStoreUnavailable
	}
	if input.File == nil {
		return nil, ErrInvalid
	}
	file, err := input.File.Open()
	if err != nil {
		return nil, ErrBinImportFailed
	}
	defer file.Close()

	columnMap := DefaultBinColumnMap()
	if input.ColumnMap != nil {
		columnMap = *input.ColumnMap
	}
	typeRules := DefaultBinTypeRules()
	if input.TypeRules != nil {
		typeRules = *input.TypeRules
	}
	prepaidKeywords := DefaultPrepaidKeywords()
	if len(input.PrepaidKeywords) > 0 {
		prepaidKeywords = input.PrepaidKeywords
	}

	rows, err := parseCardBinsCSV(file, columnMap, typeRules, prepaidKeywords)
	if err != nil {
		return nil, ErrBinImportFailed
	}

	if err := s.binRepo.UpsertBins(rows); err != nil {
		return nil, ErrBinImportFailed
	}

	countries := make(map[string]struct{})
	inserted := 0
	skipped := 0
	for _, row := range rows {
		if strings.TrimSpace(row.BIN) == "" {
			skipped++
			continue
		}
		inserted++
		if country := strings.TrimSpace(row.Country); country != "" {
			countries[strings.ToUpper(country)] = struct{}{}
		}
	}
	countryList := make([]string, 0, len(countries))
	for country := range countries {
		countryList = append(countryList, country)
	}
	sort.Strings(countryList)

	return &ImportCardBinsResult{
		Total:     len(rows),
		Inserted:  inserted,
		Skipped:   skipped,
		Countries: countryList,
	}, nil
}

// BinStats BIN 库统计。
type BinStats struct {
	Total int64 `json:"total"`
}

// GetBinStats 返回 BIN 库条目总数。
func (s *Service) GetBinStats() (*BinStats, error) {
	if s.binRepo == nil {
		return nil, ErrBinStoreUnavailable
	}
	count, err := s.binRepo.Count()
	if err != nil {
		return nil, ErrBinFetchFailed
	}
	return &BinStats{Total: count}, nil
}

// ListCardBins 查询 BIN 库列表。
func (s *Service) ListCardBins(filter cardsecretcontract.CardBinFilter) ([]cardsecretdomain.CardBin, int64, error) {
	if s.binRepo == nil {
		return nil, 0, ErrBinStoreUnavailable
	}
	return s.binRepo.List(filter)
}

// ClearCardBins 清空 BIN 库。
func (s *Service) ClearCardBins() error {
	if s.binRepo == nil {
		return ErrBinStoreUnavailable
	}
	if err := s.binRepo.DeleteAll(); err != nil {
		return ErrBinDeleteFailed
	}
	return nil
}

// annotateCardSecrets 根据卡号前 6 位匹配 BIN 库，自动标注国家/品牌/种类。
// 匹配失败静默跳过，不影响卡密入库。
func (s *Service) annotateCardSecrets(items []cardsecretdomain.Secret) {
	if s.binRepo == nil || len(items) == 0 {
		return
	}
	type index struct {
		item *cardsecretdomain.Secret
		bin  string
	}
	var targets []index
	binSet := make(map[string]struct{})
	for i := range items {
		card, ok := cardcheck.ParseCard(items[i].Secret)
		if !ok || len(card.Number) < 6 {
			continue
		}
		bin := card.Number[:6]
		if _, exists := binSet[bin]; !exists {
			binSet[bin] = struct{}{}
		}
		targets = append(targets, index{item: &items[i], bin: bin})
	}
	if len(binSet) == 0 {
		return
	}
	bins := make([]string, 0, len(binSet))
	for bin := range binSet {
		bins = append(bins, bin)
	}
	rows, err := s.binRepo.FindByBins(bins)
	if err != nil {
		return
	}
	byBin := make(map[string]cardsecretdomain.CardBin, len(rows))
	for _, row := range rows {
		byBin[row.BIN] = row
	}
	for _, target := range targets {
		bin, found := byBin[target.bin]
		if !found {
			continue
		}
		target.item.Country = bin.Country
		target.item.Brand = bin.Brand
		target.item.CardType = bin.CardType
	}
}

// parseCardBinsCSV 解析 BIN 库 CSV，按列映射、种类显式规则与预付标记归一化。
func parseCardBinsCSV(reader io.Reader, columnMap BinColumnMap, typeRules BinTypeRule, prepaidKeywords []string) ([]cardsecretdomain.CardBin, error) {
	csvReader := csv.NewReader(reader)
	csvReader.TrimLeadingSpace = true

	var (
		header     []string
		headerRead bool
		indexes    map[string]int
		rows       []cardsecretdomain.CardBin
	)
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(record) == 0 {
			continue
		}
		cleaned := make([]string, len(record))
		nonEmpty := false
		for i, col := range record {
			cleaned[i] = strings.TrimSpace(strings.TrimPrefix(col, "\ufeff"))
			if cleaned[i] != "" {
				nonEmpty = true
			}
		}
		if !nonEmpty {
			continue
		}
		if !headerRead {
			header = cleaned
			headerRead = true
			indexes = buildBinColumnIndexes(header, columnMap)
			continue
		}
		row := buildCardBinFromRecord(cleaned, indexes, typeRules, prepaidKeywords)
		if row != nil {
			rows = append(rows, *row)
		}
	}
	if !headerRead {
		return nil, errors.New("empty bin csv")
	}
	return rows, nil
}

func buildBinColumnIndexes(header []string, columnMap BinColumnMap) map[string]int {
	indexes := make(map[string]int)
	lookup := make(map[string]int, len(header))
	for i, col := range header {
		key := strings.ToUpper(strings.TrimSpace(col))
		lookup[key] = i
	}
	for _, field := range []struct {
		key  string
		name string
	}{
		{columnMap.BIN, "bin"},
		{columnMap.Country, "country"},
		{columnMap.Brand, "brand"},
		{columnMap.Type, "type"},
		{columnMap.Prepaid, "prepaid"},
	} {
		if field.name == "" {
			continue
		}
		if index, ok := lookup[strings.ToUpper(field.key)]; ok {
			indexes[field.name] = index
		}
	}
	return indexes
}

func buildCardBinFromRecord(record []string, indexes map[string]int, typeRules BinTypeRule, prepaidKeywords []string) *cardsecretdomain.CardBin {
	binIndex, hasBin := indexes["bin"]
	if !hasBin || binIndex >= len(record) {
		return nil
	}
	bin := strings.TrimSpace(record[binIndex])
	if bin == "" {
		return nil
	}
	country := ""
	if index, ok := indexes["country"]; ok && index < len(record) {
		country = strings.ToUpper(strings.TrimSpace(record[index]))
	}
	rawBrand := ""
	if index, ok := indexes["brand"]; ok && index < len(record) {
		rawBrand = strings.TrimSpace(record[index])
	}
	typeValue := ""
	if index, ok := indexes["type"]; ok && index < len(record) {
		typeValue = record[index]
	}
	prepaidValue := ""
	if index, ok := indexes["prepaid"]; ok && index < len(record) {
		prepaidValue = record[index]
	}
	return &cardsecretdomain.CardBin{
		BIN:      bin,
		Country:  country,
		Brand:    cardsecretdomain.NormalizePickBrand(rawBrand),
		RawBrand: rawBrand,
		CardType: cardsecretdomain.NormalizeCardType(typeValue, prepaidValue, typeRules, prepaidKeywords),
	}
}
