package protected

import "encoding/json"

func jsonUnmarshal(b []byte, v interface{}) error { return json.Unmarshal(b, v) }
func jsonMarshal(v interface{}) ([]byte, error)   { return json.Marshal(v) }
