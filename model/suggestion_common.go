package model

import (
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (
	suggestionHardLimit  = 20
	suggestionScanWindow = 50000
)

type suggestionStringRow struct {
	Value string `gorm:"column:value"`
}

type suggestionIntRow struct {
	Value int `gorm:"column:value"`
}

func ensureSuggestionColumnsInitialized() {
	if commonGroupCol == "" || logGroupCol == "" {
		initCol()
	}
}

func clampSuggestionLimit(limit int) int {
	if limit <= 0 || limit > suggestionHardLimit {
		return suggestionHardLimit
	}
	return limit
}

func escapeLikeLiteral(input string) string {
	replacer := strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	)
	return replacer.Replace(strings.TrimSpace(input))
}

func buildContainsLikePattern(keyword string) string {
	trimmed := escapeLikeLiteral(keyword)
	if trimmed == "" {
		return ""
	}
	return "%" + trimmed + "%"
}

func recentSuggestionBase(tx *gorm.DB, column string, timeColumn string, nonEmptyCondition string) *gorm.DB {
	return tx.Select(column + " AS value, " + timeColumn + " AS suggestion_time").
		Where(nonEmptyCondition).
		Order(timeColumn + " DESC").
		Limit(suggestionScanWindow)
}

func recentSuggestionQuery(tx *gorm.DB, recent *gorm.DB) *gorm.DB {
	return tx.Session(&gorm.Session{NewDB: true}).
		Table("(?) AS recent_suggestions", recent)
}

// scanStringSuggestions is internal-only. Callers must pass validated constant
// column names from get*SuggestionFieldColumn and a fixed timestamp column.
func scanStringSuggestions(tx *gorm.DB, column string, timeColumn string, keyword string, limit int) ([]string, error) {
	pattern := buildContainsLikePattern(keyword)
	recent := recentSuggestionBase(tx, column, timeColumn, column+" <> ''")
	query := recentSuggestionQuery(tx, recent)
	if pattern != "" {
		query = query.Where("value LIKE ? ESCAPE '!'", pattern)
	}

	rows := make([]suggestionStringRow, 0, limit)
	err := query.
		Select("value").
		Group("value").
		Order("MAX(suggestion_time) DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Value == "" {
			continue
		}
		result = append(result, row.Value)
	}
	return result, nil
}

// scanIntSuggestions is internal-only. Callers must pass validated constant
// column names from get*SuggestionFieldColumn and a fixed timestamp column.
func scanIntSuggestions(tx *gorm.DB, column string, timeColumn string, keyword string, limit int) ([]string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword != "" && !isDigitsOnly(keyword) {
		return []string{}, nil
	}

	recent := recentSuggestionBase(tx, column, timeColumn, column+" <> 0")
	query := recentSuggestionQuery(tx, recent)
	if keyword != "" {
		query = query.Where(intColumnCastExpression(tx, "value")+" LIKE ?", keyword+"%")
	}

	rows := make([]suggestionIntRow, 0, limit)
	err := query.
		Select("value").
		Group("value").
		Order("MAX(suggestion_time) DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, limit)
	for _, row := range rows {
		result = append(result, strconv.Itoa(row.Value))
	}
	return result, nil
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// intColumnCastExpression is internal-only and expects a validated numeric
// column name selected from the suggestion field maps.
func intColumnCastExpression(tx *gorm.DB, column string) string {
	switch tx.Dialector.Name() {
	case "mysql":
		return "CAST(" + column + " AS CHAR)"
	default:
		return "CAST(" + column + " AS TEXT)"
	}
}
