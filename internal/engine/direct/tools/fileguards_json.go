package tools

import "encoding/json"

// jsonValid wraps encoding/json's Valid in a single-package function
// so the settings validator in fileguards.go does not have to pull
// the json import itself (keeps the dependency graph of that file
// TOML-focused).
func jsonValid(s string) bool {
	return json.Valid([]byte(s))
}
