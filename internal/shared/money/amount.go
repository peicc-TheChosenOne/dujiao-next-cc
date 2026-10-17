// Package money provides the shared, database-safe monetary value object used
// across bounded contexts.
package money

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"github.com/shopspring/decimal"
)

// Amount stores monetary values at two-decimal precision.
type Amount struct {
	decimal.Decimal
}

// FromDecimal creates an Amount rounded to two decimal places.
func FromDecimal(amount decimal.Decimal) Amount {
	return Amount{Decimal: amount.Round(2)}
}

// MarshalJSON emits a fixed two-decimal string to avoid floating-point loss.
func (m Amount) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Decimal.Round(2).StringFixed(2))
}

// UnmarshalJSON accepts either a JSON string or a JSON number.
func (m *Amount) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		parsed, err := decimal.NewFromString(value)
		if err != nil {
			return err
		}
		m.Decimal = parsed.Round(2)
		return nil
	}
	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	m.Decimal = decimal.NewFromFloat(value).Round(2)
	return nil
}

// Value implements driver.Valuer.
func (m Amount) Value() (driver.Value, error) {
	return m.Decimal.Round(2).Value()
}

// Scan implements sql.Scanner.
func (m *Amount) Scan(value any) error {
	if err := m.Decimal.Scan(value); err != nil {
		return err
	}
	m.Decimal = m.Decimal.Round(2)
	return nil
}

// String returns a fixed two-decimal representation.
func (m Amount) String() string {
	return m.Decimal.Round(2).StringFixed(2)
}

// OptionalAmount distinguishes an omitted field from an explicit null.
type OptionalAmount struct {
	Set   bool
	Value *Amount
}

func (m *OptionalAmount) UnmarshalJSON(data []byte) error {
	m.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		m.Value = nil
		return nil
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("amount must be a decimal string or null")
	}
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return err
	}
	if !value.Equal(value.Round(2)) || value.Abs().GreaterThanOrEqual(decimal.NewFromInt(1).Shift(18)) {
		return fmt.Errorf("amount exceeds decimal(20,2) precision")
	}
	amount := FromDecimal(value)
	m.Value = &amount
	return nil
}

func (m OptionalAmount) Apply(current *Amount) *Amount {
	if m.Set {
		return m.Value
	}
	return current
}
