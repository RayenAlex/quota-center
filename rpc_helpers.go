package main

import "encoding/json"

func decodeRPCResult(raw []byte, out any) error {
	var payload envelope
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if !payload.OK {
		return json.Unmarshal(payload.Result, out)
	}
	return json.Unmarshal(payload.Result, out)
}
