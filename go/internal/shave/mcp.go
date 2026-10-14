package shave

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func cmdMCP(args []string) int {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var req mcpRequest
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return 0
			}
			return 1
		}
		if isNotification(req) {
			continue
		}
		resp := handleMCP(req)
		if resp != nil {
			enc.Encode(resp)
		}
	}
}

func isNotification(r mcpRequest) bool {
	return len(r.ID) == 0 && r.Method != ""
}

func handleMCP(req mcpRequest) map[string]interface{} {
	switch req.Method {
	case "initialize":
		return okResp(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "shave",
				"version": version,
			},
		})
	case "tools/list":
		return okResp(req.ID, map[string]interface{}{
			"tools": mcpTools(),
		})
	case "tools/call":
		var p struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errResp(req.ID, -32602, err.Error())
		}
		out, isErr := callMCPTool(p.Name, p.Arguments)
		return okResp(req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": out},
			},
			"isError": isErr,
		})
	case "ping":
		return okResp(req.ID, map[string]interface{}{})
	default:
		return errResp(req.ID, -32601, "method not found: "+req.Method)
	}
}

func okResp(id json.RawMessage, result interface{}) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      rawID(id),
		"result":  result,
	}
}

func errResp(id json.RawMessage, code int, msg string) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      rawID(id),
		"error": map[string]interface{}{
			"code":    code,
			"message": msg,
		},
	}
}

func rawID(id json.RawMessage) interface{} {
	if len(id) == 0 {
		return nil
	}
	s := strings.TrimSpace(string(id))
	if s == "null" {
		return nil
	}
	return json.RawMessage(id)
}

func mcpTools() []mcpTool {
	strProp := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	intProp := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "integer", "description": desc}
	}
	boolProp := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "boolean", "description": desc}
	}
	return []mcpTool{
		{
			Name:        "shave_logs",
			Description: "Compress a log stream: redact secrets, de-dupe, focus on errors.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":    strProp("Raw log text to compress"),
					"errors":  boolProp("Keep only error/warn-relevant lines (+context)"),
					"context": intProp("Context lines around matches (errors mode)"),
					"max":     intProp("Max output lines"),
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "shave_tokens",
			Description: "Count tokens of text and estimate USD cost for a model.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":  strProp("Text to count"),
					"model": strProp("Model name (e.g. claude-sonnet-4)"),
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "shave_k8s_pods",
			Description: "Compact pod status table from kubectl.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": strProp("Namespace; empty = current/default"),
					"all":       boolProp("All namespaces"),
				},
			},
		},
		{
			Name:        "shave_k8s_events",
			Description: "Deduped compact events from kubectl.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": strProp("Namespace; empty = current/default"),
					"all":       boolProp("All namespaces"),
					"max":       intProp("Max events"),
				},
			},
		},
		{
			Name:        "shave_ctx",
			Description: "Prune code context to relevant snippets or declarations.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"paths":  strProp("Comma-separated file or directory paths"),
					"query":  strProp("Topic query to focus on (optional)"),
					"budget": intProp("Max output chars"),
				},
				"required": []string{"paths"},
			},
		},
	}
}

func callMCPTool(name string, args map[string]interface{}) (string, bool) {
	switch name {
	case "shave_logs":
		text := strArg(args, "text")
		cli := []string{"logs"}
		if boolArg(args, "errors") {
			cli = append(cli, "--errors")
		}
		if v := intArg(args, "context"); v > 0 {
			cli = append(cli, "--context", strconv.Itoa(v))
		}
		if v := intArg(args, "max"); v > 0 {
			cli = append(cli, "--max", strconv.Itoa(v))
		}
		return runShave(cli, text)
	case "shave_tokens":
		model := strArg(args, "model")
		argsCLI := []string{"tokens"}
		if model != "" {
			argsCLI = append(argsCLI, "-m", model)
		}
		return runShave(argsCLI, strArg(args, "text"))
	case "shave_k8s_pods":
		argsCLI := []string{"k8s", "pods"}
		if ns := strArg(args, "namespace"); ns != "" {
			argsCLI = append(argsCLI, "-n", ns)
		}
		if boolArg(args, "all") {
			argsCLI = append(argsCLI, "-A")
		}
		return runShave(argsCLI, "")
	case "shave_k8s_events":
		argsCLI := []string{"k8s", "events"}
		if ns := strArg(args, "namespace"); ns != "" {
			argsCLI = append(argsCLI, "-n", ns)
		}
		if boolArg(args, "all") {
			argsCLI = append(argsCLI, "-A")
		}
		if v := intArg(args, "max"); v > 0 {
			argsCLI = append(argsCLI, "--max", strconv.Itoa(v))
		}
		return runShave(argsCLI, "")
	case "shave_ctx":
		argsCLI := []string{"ctx"}
		if q := strArg(args, "query"); q != "" {
			argsCLI = append(argsCLI, "-q", q)
		}
		if v := intArg(args, "budget"); v > 0 {
			argsCLI = append(argsCLI, "--budget", strconv.Itoa(v))
		}
		paths := strArg(args, "paths")
		for _, p := range strings.Split(paths, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				argsCLI = append(argsCLI, p)
			}
		}
		return runShave(argsCLI, "")
	default:
		return "unknown tool: " + name, true
	}
}

func runShave(args []string, stdin string) (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return err.Error(), true
	}
	cmd := exec.Command(exe, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)) + "\n" + err.Error(), true
	}
	return string(out), false
}

func strArg(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok && v != nil {
		switch x := v.(type) {
		case string:
			return x
		default:
			return fmt.Sprintf("%v", x)
		}
	}
	return ""
}
func boolArg(m map[string]interface{}, k string) bool {
	if v, ok := m[k]; ok {
		b, _ := v.(bool)
		return b
	}
	return false
}
func intArg(m map[string]interface{}, k string) int {
	if v, ok := m[k]; ok {
		switch x := v.(type) {
		case float64:
			return int(x)
		case int:
			return x
		case json.Number:
			n, _ := x.Int64()
			return int(n)
		}
	}
	return 0
}
