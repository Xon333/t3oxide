package ui

import (
	"encoding/json"
	"testing"
)

func TestBatchReceiptTokenKeepsExactIdentity(t *testing.T) {
	want := batchReceiptToken{"receipt/界\x1fwith spaces", 19}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got batchReceiptToken
	if err = json.Unmarshal(raw, &got); err != nil || got != want {
		t.Fatalf("identity changed: %+v %v", got, err)
	}
}
