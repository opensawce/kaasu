package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/opensawce/kaasu/internal/recon"
)

var stmtOn = map[string]string{"ai.enabled": "1", "ai.feature.statement": "1", "ai.notice_ack_at": "x", "ai.monthly_cap_inr": "500"}

const statement = "Txn Date,Details,Money In,Money Out,Ref\n23/10/2026,UPI-RAVI,499.07,,629812345678\n24/10/2026,UPI-ASHA,1000.00,,400000000004\n24/10/2026,SHOP,,250.00,\n25/10/2026,MORE,5.00,,\n"

func TestInferredMappingIsCheckedAgainstWholeFile(t *testing.T) {
	answer := `{"date_col":"Txn Date","desc_col":"Details","credit_col":"Money In","debit_col":"Money Out","utr_col":"Ref","date_layout":"02/01/2006","delimiter":","}`
	s, _, _ := setup(t, stmtOn, answer)
	m, err := InferMapping(context.Background(), s, []byte(statement))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := recon.ValidateMapping([]byte(statement), m)
	if err != nil || rows != 4 {
		t.Fatalf("rows=%d err=%v", rows, err)
	}
}

func TestWrongMappingFailsWithRowNumber(t *testing.T) {
	answer := `{"date_col":"Txn Date","desc_col":"Details","credit_col":"Ref","debit_col":"Money Out","utr_col":"Ref","date_layout":"02/01/2006","delimiter":","}`
	s, _, _ := setup(t, stmtOn, answer)
	m, _ := InferMapping(context.Background(), s, []byte(statement))
	if _, err := recon.ValidateMapping([]byte(statement), m); err == nil || !strings.Contains(err.Error(), "row 1") {
		t.Errorf("wrong credit column should fail at row 1: %v", err)
	}
}

func TestOnlyHeaderAndSampleRowsSent(t *testing.T) {
	answer := `{"date_col":"Txn Date","desc_col":"Details","credit_col":"Money In","debit_col":"Money Out","utr_col":"Ref","date_layout":"02/01/2006","delimiter":","}`
	s, stub, _ := setup(t, stmtOn, answer)
	if _, err := InferMapping(context.Background(), s, []byte(statement)); err != nil {
		t.Fatal(err)
	}
	sent := stub.Seen[0].User
	if strings.Contains(sent, "MORE") || strings.Contains(sent, "629812345678") {
		t.Errorf("more than the header and three rows was sent: %s", sent)
	}
	if strings.Count(sent, "\n") > 4 {
		t.Errorf("sent %d lines", strings.Count(sent, "\n"))
	}
}

func TestValidationKeepsAmountsAsPaise(t *testing.T) {
	m := recon.Mapping{DateCol: "Txn Date", DescCol: "Details", CreditCol: "Money In", DebitCol: "Money Out", UTRCol: "Ref", DateLayout: "02/01/2006", Delimiter: ','}
	if _, err := recon.ValidateMapping([]byte("Txn Date,Details,Money In,Money Out,Ref\n23/10/2026,x,1.2.3,,\n"), m); err == nil {
		t.Error("a float-like amount must fail")
	}
}
