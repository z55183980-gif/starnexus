package model

import (
	"database/sql/driver"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

func marshalJSONDatabaseValue(value any) (driver.Value, error) {
	data, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func scanJSONDatabaseValue[T any](value any, target *T) error {
	var data []byte
	switch typed := value.(type) {
	case nil:
		var zero T
		*target = zero
		return nil
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("unsupported JSON database value type %T", value)
	}

	if len(data) == 0 {
		var zero T
		*target = zero
		return nil
	}
	return common.Unmarshal(data, target)
}
