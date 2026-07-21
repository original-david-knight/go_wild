package gowild_polymarket

import (
	"encoding/json"
	"io"
)

// decodeJSON decodes a JSON response body into the target.
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
