package i18n

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A text kept for later -- in the database, say -- before anyone knows
// who will read it in which language: Ref(key, args) now, Resolve(lang,
// ref) when it's shown. Plain text resolves to itself.

const refPrefix = "i18n:"

type ref struct {
	Key  string            `json:"k"`
	Args map[string]string `json:"a,omitempty"`
}

// Ref is key with its placeholders' values (name, value pairs), to
// resolve later.
func Ref(key string, args ...any) string {
	r := ref{Key: key}
	for i := 0; i+1 < len(args); i += 2 {
		if r.Args == nil {
			r.Args = map[string]string{}
		}
		r.Args[fmt.Sprint(args[i])] = fmt.Sprint(args[i+1])
	}
	b, _ := json.Marshal(r)
	return refPrefix + string(b)
}

// Resolve is s in lang when it's a Ref, else s itself.
func Resolve(lang, s string) string {
	body, ok := strings.CutPrefix(s, refPrefix)
	if !ok {
		return s
	}
	var r ref
	if json.Unmarshal([]byte(body), &r) != nil {
		return s
	}
	args := make([]any, 0, 2*len(r.Args))
	for k, v := range r.Args {
		args = append(args, k, v)
	}
	return T(lang, r.Key, args...)
}
