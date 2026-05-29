package watchlist

import (
	"encoding/csv"
	"fmt"
	"os"
	"slices"
	"strings"
)

type Symbol struct {
	Symbol string `json:"symbol"`
}

func LoadCSV(path string) ([]Symbol, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}

	symbolCol := 0
	for i, name := range rows[0] {
		if strings.EqualFold(strings.TrimSpace(name), "symbol") {
			symbolCol = i
			break
		}
	}

	seen := make(map[string]struct{})
	out := make([]Symbol, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if symbolCol >= len(row) {
			continue
		}
		symbol := strings.ToUpper(strings.TrimSpace(row[symbolCol]))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, Symbol{Symbol: symbol})
	}
	slices.SortFunc(out, func(a, b Symbol) int {
		return strings.Compare(a.Symbol, b.Symbol)
	})
	return out, nil
}

func Symbols(items []Symbol) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Symbol) != "" {
			out = append(out, item.Symbol)
		}
	}
	return out
}
