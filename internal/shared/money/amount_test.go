package money

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

func TestFromDecimalRoundsToTwoPlaces(t *testing.T) {
	amount := FromDecimal(decimal.RequireFromString("12.345"))
	if got := amount.String(); got != "12.35" {
		t.Fatalf("String() = %q, want 12.35", got)
	}
}

func TestAmountJSONContract(t *testing.T) {
	amount := FromDecimal(decimal.RequireFromString("7.1"))
	encoded, err := json.Marshal(amount)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != `"7.10"` {
		t.Fatalf("marshal = %s, want quoted fixed precision", encoded)
	}

	for _, input := range []string{`"9.876"`, `9.876`} {
		var decoded Amount
		if err := json.Unmarshal([]byte(input), &decoded); err != nil {
			t.Fatalf("unmarshal %s: %v", input, err)
		}
		if got := decoded.String(); got != "9.88" {
			t.Fatalf("unmarshal %s = %q, want 9.88", input, got)
		}
	}
}

func TestAmountDatabaseValueAndScanRoundTrip(t *testing.T) {
	amount := FromDecimal(decimal.RequireFromString("123.456"))
	value, err := amount.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	var decoded Amount
	if err := decoded.Scan(value); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if decoded.String() != "123.46" {
		t.Fatalf("round trip = %q, want 123.46", decoded.String())
	}
}

func TestOptionalQuoteAmountDistinguishesOmittedNullAndPreciseMoney(t *testing.T) {
	previous := FromDecimal(decimal.RequireFromString("19.90"))
	for _, tc := range []struct {
		json    string
		set     bool
		value   string
		invalid bool
	}{
		{`{}`, false, "19.90", false}, {`{"price":null}`, true, "", false},
		{`{"price":"12.34"}`, true, "12.34", false}, {`{"price":12.34}`, true, "", true},
		{`{"price":"12.345"}`, true, "", true}, {`{"price":"1000000000000000000"}`, true, "", true},
	} {
		var obj struct {
			Price OptionalAmount `json:"price"`
		}
		err := json.Unmarshal([]byte(tc.json), &obj)
		if tc.invalid {
			if err == nil {
				t.Fatalf("accepted invalid amount %s", tc.json)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		actual := obj.Price.Apply(&previous)
		if obj.Price.Set != tc.set {
			t.Fatal(tc.json)
		}
		if tc.value == "" {
			if actual != nil {
				t.Fatal(tc.json)
			}
		} else if actual == nil || actual.String() != tc.value {
			t.Fatal(tc.json, actual)
		}
	}
}
