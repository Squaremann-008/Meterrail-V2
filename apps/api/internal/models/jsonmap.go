package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

// JSONMap is a jsonb column that round-trips as a plain map. GORM needs the
// Valuer/Scanner pair to know how to hand it to the driver.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}

	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("JSONMap: cannot scan %T", src)
	}
	if len(raw) == 0 {
		*m = nil
		return nil
	}

	decoded := JSONMap{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return errors.Join(errors.New("JSONMap: unmarshal"), err)
	}
	*m = decoded
	return nil
}

// GormDataType tells AutoMigrate to emit a jsonb column.
func (JSONMap) GormDataType() string { return "jsonb" }
