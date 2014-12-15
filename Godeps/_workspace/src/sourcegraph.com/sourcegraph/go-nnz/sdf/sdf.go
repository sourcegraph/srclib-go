// Package sdf defines sane defaults for primitive types where null
// values are deserialized to the type's zero value, but values are
// never converted to null when serialized, they are always returned
// as their value. ('sdf' stands for 'sane defaults'. And before you
// ask, yes, one letter per syllable is a sane choice.)
package sdf

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type Bool bool

func (b *Bool) Scan(v interface{}) error {
	if v == nil {
		*b = false
		return nil
	}
	switch v := v.(type) {
	case bool:
		*b = Bool(v)
	default:
		return fmt.Errorf("sdf: scanning %T, got %T", b, v)
	}
	return nil
}

func (b Bool) Value() (driver.Value, error) {
	return bool(b), nil
}

func (b *Bool) UnmarshalJSON(data []byte) error {
	var v interface{}
	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}
	if v == nil {
		*b = false
	} else if v, ok := v.(bool); ok {
		*b = Bool(v)
	} else {
		return fmt.Errorf("sdf: unmarshaling %T, got %T", b, v)
	}
	return nil
}

// Int is a wrapper around int where SQL/JSON null deserializes to Go
// int(0).
type Int int

// Scan implements the database/sql/driver.Scanner interface.
func (i *Int) Scan(v interface{}) error {
	if v == nil {
		*i = 0
		return nil
	}
	switch v := v.(type) {
	case int64:
		*i = Int(v)
	default:
		return fmt.Errorf("sdf: scanning %T, got %T", i, v)
	}
	return nil
}

// Value implements the database/sql/driver.Valuer interface.
func (i Int) Value() (driver.Value, error) {
	return int64(i), nil
}

// UnmarshalJSON implements the encoding/json.Unmarshaler interface.
func (i *Int) UnmarshalJSON(data []byte) error {
	var v interface{}
	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}
	if v == nil {
		*i = 0
	} else if v, ok := v.(float64); ok {
		*i = Int(v)
	} else {
		return fmt.Errorf("sdf: unmarshaling %T, got %T", i, v)
	}
	return nil
}

// Int64 is a wrapper around int64 where SQL/JSON null deserializes to
// Go int64(0).
type Int64 int64

// Scan implements the database/sql/driver.Scanner interface.
func (i *Int64) Scan(v interface{}) error {
	if v == nil {
		*i = 0
		return nil
	}
	switch v := v.(type) {
	case int64:
		*i = Int64(v)
	default:
		return fmt.Errorf("sdf: scanning %T, got %T", i, v)
	}
	return nil
}

// Value implements the database/sql/driver.Valuer interface.
func (i Int64) Value() (driver.Value, error) {
	return int64(i), nil
}

// UnmarshalJSON implements the encoding/json.Unmarshaler interface.
func (i *Int64) UnmarshalJSON(data []byte) error {
	var v interface{}
	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}
	if v == nil {
		*i = 0
	} else if v, ok := v.(float64); ok {
		*i = Int64(v)
	} else {
		return fmt.Errorf("sdf: unmarshaling %T, got %T", i, v)
	}
	return nil
}

// String is a wrapper around string where SQL/JSON null deserializes
// to Go "".
type String string

// Scan implements the database/sql/driver.Scanner interface.
func (s *String) Scan(v interface{}) error {
	if v == nil {
		*s = ""
		return nil
	}
	switch v := v.(type) {
	case []byte:
		*s = String(v)
	case string:
		*s = String(v)
	default:
		return fmt.Errorf("sdf: scanning %T, got %T", s, v)
	}
	return nil
}

// Value implements the database/sql/driver.Valuer interface.
func (s String) Value() (driver.Value, error) {
	return string(s), nil
}

// UnmarshalJSON implements the encoding/json.Unmarshaler interface.
func (s *String) UnmarshalJSON(data []byte) error {
	var v interface{}
	err := json.Unmarshal(data, &v)
	if err != nil {
		return err
	}
	if v == nil {
		*s = ""
	} else if v, ok := v.(string); ok {
		*s = String(v)
	} else {
		return fmt.Errorf("sdf: unmarshaling %T, got %T", s, v)
	}
	return nil
}
