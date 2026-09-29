package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func call(t *testing.T, msg map[string]any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(msg)
	var out bytes.Buffer
	old := getStdout()
	setStdout(&out)
	defer setStdout(old)
	var rm rpcMsg
	if err := json.Unmarshal(data, &rm); err != nil {
		t.Fatal(err)
	}
	handle(rm)
	var res map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &res); err != nil {
		t.Fatalf("response = %q: %v", out.String(), err)
	}
	return res
}

func TestInitializeAndToolsList(t *testing.T) {
	res := call(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18"}})
	r := res["result"].(map[string]any)
	if r["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize = %v", res)
	}
	res = call(t, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	list := res["result"].(map[string]any)["tools"].([]any)
	if len(list) != 2 {
		t.Fatalf("tools = %v", list)
	}
	names := map[string]bool{}
	for _, raw := range list {
		names[raw.(map[string]any)["name"].(string)] = true
	}
	if !names["docker_containers"] || !names["docker_container"] {
		t.Fatalf("tools = %v", names)
	}
}

func TestUnknownMethodAndTool(t *testing.T) {
	res := call(t, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "nope"})
	if res["error"] == nil {
		t.Fatalf("nope = %v, want an error", res)
	}
	res = call(t, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": map[string]any{"name": "docker_moonwalk", "arguments": map[string]any{}}})
	if res["error"] == nil {
		t.Fatalf("unknown tool = %v, want an error", res)
	}
}

// A notification (no id) gets no answer — the MCP protocol's own rule.
func TestNotificationGetsNoAnswer(t *testing.T) {
	var out bytes.Buffer
	old := getStdout()
	setStdout(&out)
	defer setStdout(old)
	handle(rpcMsg{JSONRPC: "2.0", Method: "ping"})
	if out.Len() != 0 {
		t.Fatalf("notification answered: %q", out.String())
	}
}

// docker_container without a Docker socket answers a tool error (isError:
// true inside the result), never a JSON-RPC protocol error — the call
// itself was well formed.
func TestToolFailureIsNotAProtocolError(t *testing.T) {
	t.Setenv("PICODE_DOCKER_HOST", "unix:///nonexistent/engine.sock")
	res := call(t, map[string]any{"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": map[string]any{"name": "docker_container", "arguments": map[string]any{"containerId": "not-an-id"}}})
	if res["error"] != nil {
		t.Fatalf("tool failure = %v, want it inside result", res)
	}
	r := res["result"].(map[string]any)
	if r["isError"] != true {
		t.Fatalf("result = %v, want isError", r)
	}
}
