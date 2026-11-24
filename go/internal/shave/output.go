package shave

import (
	"encoding/json"
	"os"
)

// writeJSON is the single JSON emitter for every command. Keeping it here
// avoids repeating the encoder setup in each command (and drifting shapes).
func writeJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
