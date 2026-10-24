package ai

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"

	"github.com/opensawce/kaasu/internal/recon"
)

var mappingSchema = map[string]any{
	"type":     "object",
	"required": []string{"date_col", "desc_col", "credit_col", "debit_col", "utr_col", "date_layout", "delimiter"},
	"properties": map[string]any{
		"date_col":    map[string]any{"type": "string", "maxLength": 60},
		"desc_col":    map[string]any{"type": "string", "maxLength": 60},
		"credit_col":  map[string]any{"type": "string", "maxLength": 60},
		"debit_col":   map[string]any{"type": "string", "maxLength": 60},
		"utr_col":     map[string]any{"type": "string", "maxLength": 60},
		"date_layout": map[string]any{"type": "string", "maxLength": 30},
		"delimiter":   map[string]any{"type": "string", "maxLength": 1},
	},
}

type mappingAnswer struct {
	DateCol    string `json:"date_col"`
	DescCol    string `json:"desc_col"`
	CreditCol  string `json:"credit_col"`
	DebitCol   string `json:"debit_col"`
	UTRCol     string `json:"utr_col"`
	DateLayout string `json:"date_layout"`
	Delimiter  string `json:"delimiter"`
}

const statementSystem = "Given a bank statement header and three sample rows, name the columns for date, description, credit, debit and UTR, the Go time layout of the date, and the delimiter. Answer only with JSON."

// InferMapping asks the model for a column layout. Only the header and at most three rows are sent, masked.
// The caller must still run recon.ValidateMapping on the whole file before anything is imported.
func InferMapping(ctx context.Context, s *Service, data []byte) (recon.Mapping, error) {
	delimiter := detectDelimiter(data)
	cr := csv.NewReader(strings.NewReader(string(data)))
	cr.Comma = delimiter
	rows, err := cr.ReadAll()
	if err != nil || len(rows) < 2 {
		return recon.Mapping{}, ErrSchema
	}
	sample := rows[:1]
	for i := 1; i < len(rows) && i <= 3; i++ {
		sample = append(sample, rows[i])
	}
	var b strings.Builder
	w := csv.NewWriter(&b) // keeps quoting, so fields containing the delimiter stay in their column
	w.Comma = delimiter
	if err := w.WriteAll(sample); err != nil {
		return recon.Mapping{}, ErrSchema
	}
	resp, err := s.Run(ctx, "statement", statementSystem, b.String(), mappingSchema)
	if err != nil {
		return recon.Mapping{}, err
	}
	var a mappingAnswer
	if err := json.Unmarshal(resp.JSON, &a); err != nil {
		return recon.Mapping{}, ErrSchema
	}
	delim := ','
	if a.Delimiter != "" {
		delim = []rune(a.Delimiter)[0]
	}
	return recon.Mapping{DateCol: a.DateCol, DescCol: a.DescCol, CreditCol: a.CreditCol, DebitCol: a.DebitCol,
		UTRCol: a.UTRCol, DateLayout: a.DateLayout, Delimiter: delim}, nil
}

// detectDelimiter picks the most frequent of the usual separators in the header line, defaulting to a comma.
func detectDelimiter(data []byte) rune {
	line, _, _ := strings.Cut(string(data), "\n")
	best, bestN := ',', 0
	for _, d := range []rune{',', ';', '\t', '|'} {
		if n := strings.Count(line, string(d)); n > bestN {
			best, bestN = d, n
		}
	}
	return best
}
