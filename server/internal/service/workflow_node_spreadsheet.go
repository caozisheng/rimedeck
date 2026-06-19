package service

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/utils/maps"
)

// SpreadsheetNode converts a JSON array of objects into CSV format.
type SpreadsheetNode struct {
	Config SpreadsheetNodeConfig
}

type SpreadsheetNodeConfig struct {
	Columns   []string `json:"columns"`   // explicit column order; empty = auto-detect from keys
	Separator string   `json:"separator"` // field separator, default ","
}

func (n *SpreadsheetNode) Type() string   { return "spreadsheet" }
func (n *SpreadsheetNode) New() types.Node { return &SpreadsheetNode{} }

func (n *SpreadsheetNode) Init(_ types.Config, configuration types.Configuration) error {
	return maps.Map2Struct(configuration, &n.Config)
}

func (n *SpreadsheetNode) Destroy() {}

func (n *SpreadsheetNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	// Parse input JSON array
	var rows []map[string]any
	if err := json.Unmarshal([]byte(msg.Data.Get()), &rows); err != nil {
		ctx.TellFailure(msg, fmt.Errorf("x/spreadsheet: failed to parse JSON array: %w", err))
		return
	}

	if len(rows) == 0 {
		result := map[string]any{
			"csv":       "",
			"row_count": 0,
			"columns":   []string{},
		}
		resultJSON, _ := json.Marshal(result)
		msg.Data.Set(string(resultJSON))
		ctx.TellSuccess(msg)
		return
	}

	// Determine columns
	columns := n.Config.Columns
	if len(columns) == 0 {
		// Auto-detect from all keys across all rows
		seen := make(map[string]struct{})
		for _, row := range rows {
			for k := range row {
				seen[k] = struct{}{}
			}
		}
		columns = make([]string, 0, len(seen))
		for k := range seen {
			columns = append(columns, k)
		}
		sort.Strings(columns) // deterministic order
	}

	// Write CSV
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	if n.Config.Separator != "" {
		runes := []rune(n.Config.Separator)
		if len(runes) > 0 {
			w.Comma = runes[0]
		}
	}

	// Header row
	if err := w.Write(columns); err != nil {
		ctx.TellFailure(msg, fmt.Errorf("x/spreadsheet: csv write header: %w", err))
		return
	}

	// Data rows
	record := make([]string, len(columns))
	for _, row := range rows {
		for i, col := range columns {
			val, ok := row[col]
			if !ok {
				record[i] = ""
				continue
			}
			record[i] = fmt.Sprintf("%v", val)
		}
		if err := w.Write(record); err != nil {
			ctx.TellFailure(msg, fmt.Errorf("x/spreadsheet: csv write row: %w", err))
			return
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		ctx.TellFailure(msg, fmt.Errorf("x/spreadsheet: csv flush: %w", err))
		return
	}

	result := map[string]any{
		"csv":       buf.String(),
		"row_count": len(rows),
		"columns":   columns,
	}
	resultJSON, _ := json.Marshal(result)
	msg.Data.Set(string(resultJSON))
	ctx.TellSuccess(msg)
}
