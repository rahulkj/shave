package shave

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// reposFromStdin reads `gh search repos --json ...` / `gh api user/repos`
// output. The binary itself still does zero network: the caller pipes JSON in.
func reposFromStdin(redact bool) []ghRepo {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shave:", err)
		return nil
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(b, &arr); err != nil {
		fmt.Fprintln(os.Stderr, "shave: expected a JSON array of repos on stdin:", err)
		return nil
	}
	var out []ghRepo
	for _, m := range arr {
		r := ghRepo{
			Name:        jsonString(m, "name"),
			Path:        jsonString(m, "html_url"),
			Description: cleanText(jsonString(m, "description"), redact, 160),
			Updated:     jsonString(m, "updated_at"),
		}
		if owner, ok := m["owner"].(map[string]interface{}); ok {
			r.Owner = jsonString(owner, "login")
		}
		switch v := m["language"].(type) {
		case string:
			r.Language = v
		case map[string]interface{}:
			r.Language = jsonString(v, "name")
		}
		if topics, ok := m["topics"].([]interface{}); ok {
			for _, t := range topics {
				if s, ok := t.(string); ok {
					r.Topics = append(r.Topics, s)
				}
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func jsonString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
