package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var errRequiredFieldMissing = errors.New("required field missing")

func decodeLines(t *testing.T, out *bytes.Buffer) []map[string]interface{} {
	t.Helper()
	var results []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("output line is not valid JSON: %v; line = %q", err, line)
		}
		results = append(results, m)
	}
	return results
}

func TestServeHandlesInitialize(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "1.2.3", nil); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	lines := decodeLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("got %d response line(s), want 1", len(lines))
	}
	result, ok := lines[0]["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("response = %+v, want a result object", lines[0])
	}
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want 2025-06-18", result["protocolVersion"])
	}
	serverInfo, _ := result["serverInfo"].(map[string]interface{})
	if serverInfo["version"] != "1.2.3" {
		t.Errorf("serverInfo.version = %v, want 1.2.3", serverInfo["version"])
	}
}

// TestServeInitializeUnsupportedVersionFallsBackToOwn covers version
// negotiation: a client requesting a version skillci doesn't support must
// get skillci's own supported version back, not an error or the
// unsupported version echoed.
func TestServeInitializeUnsupportedVersionFallsBackToOwn(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", nil); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	result := lines[0]["result"].(map[string]interface{})
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v, want skillci's own %q", result["protocolVersion"], protocolVersion)
	}
}

// TestServeNotificationProducesNoResponse is the reachability test for the
// JSON-RPC 2.0 rule this server must never violate: a message with no
// "id" field is a notification and must never receive a response line —
// an unsolicited response would itself be an invalid MCP message on
// stdout.
func TestServeNotificationProducesNoResponse(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", nil); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want empty — a notification must never get a response", out.String())
	}
}

func TestServeToolsListReturnsRegisteredTools(t *testing.T) {
	tools := []Tool{
		{Name: "alpha", Title: "Alpha", Description: "does alpha things", InputSchema: map[string]interface{}{"type": "object"}},
		{Name: "beta", Title: "Beta", Description: "does beta things", InputSchema: map[string]interface{}{"type": "object"}},
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", tools); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	result := lines[0]["result"].(map[string]interface{})
	listedTools := result["tools"].([]interface{})
	if len(listedTools) != 2 {
		t.Fatalf("got %d tools, want 2", len(listedTools))
	}
	names := map[string]bool{}
	for _, raw := range listedTools {
		tm := raw.(map[string]interface{})
		names[tm["name"].(string)] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("tool names = %v, want alpha and beta", names)
	}
}

func TestServeToolsCallSuccess(t *testing.T) {
	tools := []Tool{
		{Name: "echo", Handler: func(arguments json.RawMessage) (string, bool, error) {
			return "echoed: " + string(arguments), false, nil
		}},
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", tools); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	result := lines[0]["result"].(map[string]interface{})
	if result["isError"] != false {
		t.Errorf("isError = %v, want false", result["isError"])
	}
	content := result["content"].([]interface{})[0].(map[string]interface{})
	if !strings.Contains(content["text"].(string), `echoed: {"x":1}`) {
		t.Errorf("content.text = %q, want it to contain the echoed arguments", content["text"])
	}
}

// TestServeToolsCallToolReportsIsError proves a Handler's own isError
// return value (a business-logic failure, e.g. lint findings) reaches the
// client, distinct from a protocol-level error.
func TestServeToolsCallToolReportsIsError(t *testing.T) {
	tools := []Tool{
		{Name: "always-fails", Handler: func(arguments json.RawMessage) (string, bool, error) {
			return "something went wrong", true, nil
		}},
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"always-fails","arguments":{}}}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", tools); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	result := lines[0]["result"].(map[string]interface{})
	if result["isError"] != true {
		t.Errorf("isError = %v, want true", result["isError"])
	}
}

func TestServeToolsCallUnknownToolReturnsProtocolError(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", nil); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	errObj, ok := lines[0]["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("response = %+v, want an error object", lines[0])
	}
	if errObj["code"] != float64(errCodeMethodNotFound) {
		t.Errorf("error.code = %v, want %d", errObj["code"], errCodeMethodNotFound)
	}
}

// TestServeToolsCallHandlerErrorIsProtocolLevel proves a Handler's
// returned error (malformed arguments) produces a JSON-RPC error
// response, not a tool result with isError:true — the two are meant to
// be distinct per the package doc comment.
func TestServeToolsCallHandlerErrorIsProtocolLevel(t *testing.T) {
	tools := []Tool{
		{Name: "strict", Handler: func(arguments json.RawMessage) (string, bool, error) {
			return "", false, errRequiredFieldMissing
		}},
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"strict","arguments":{}}}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", tools); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	if _, ok := lines[0]["error"]; !ok {
		t.Fatalf("response = %+v, want a JSON-RPC error object, not a result", lines[0])
	}
	if _, ok := lines[0]["result"]; ok {
		t.Errorf("response = %+v, want no result field alongside an error", lines[0])
	}
}

func TestServeUnknownMethodReturnsMethodNotFound(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"bogus/method"}` + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", nil); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	errObj := lines[0]["error"].(map[string]interface{})
	if errObj["code"] != float64(errCodeMethodNotFound) {
		t.Errorf("error.code = %v, want %d", errObj["code"], errCodeMethodNotFound)
	}
}

func TestServeMalformedJSONReturnsParseError(t *testing.T) {
	in := strings.NewReader("not even json\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", nil); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	errObj := lines[0]["error"].(map[string]interface{})
	if errObj["code"] != float64(errCodeParseError) {
		t.Errorf("error.code = %v, want %d", errObj["code"], errCodeParseError)
	}
	if lines[0]["id"] != nil {
		t.Errorf("id = %v, want null — a parse error has no way to know the request's real id", lines[0]["id"])
	}
}

// TestServeProcessesMultipleMessagesInOrder proves the loop keeps running
// across many lines in one stream and never mixes up which response
// belongs to which request.
func TestServeProcessesMultipleMessagesInOrder(t *testing.T) {
	tools := []Tool{
		{Name: "echo", Handler: func(arguments json.RawMessage) (string, bool, error) {
			return string(arguments), false, nil
		}},
	}
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":"first"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":"second"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":"third"}}`,
	}, "\n") + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", tools); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	if len(lines) != 3 {
		t.Fatalf("got %d response lines, want 3", len(lines))
	}
	for i, want := range []string{"first", "second", "third"} {
		id := lines[i]["id"]
		if id != float64(i+1) {
			t.Errorf("line %d: id = %v, want %d", i, id, i+1)
		}
		result := lines[i]["result"].(map[string]interface{})
		content := result["content"].([]interface{})[0].(map[string]interface{})
		if !strings.Contains(content["text"].(string), want) {
			t.Errorf("line %d: text = %q, want it to contain %q", i, content["text"], want)
		}
	}
}

// TestServeHandlesLineLargerThan64KB proves ReadString has no
// bufio.Scanner-style token-size ceiling — a large check/eval result must
// never be silently truncated or dropped.
func TestServeHandlesLineLargerThan64KB(t *testing.T) {
	bigArg := strings.Repeat("x", 100_000) // well beyond bufio.Scanner's default 64KB limit
	tools := []Tool{
		{Name: "echo-len", Handler: func(arguments json.RawMessage) (string, bool, error) {
			return string(rune(len(arguments))), false, nil
		}},
	}
	reqJSON, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]interface{}{"name": "echo-len", "arguments": bigArg},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(string(reqJSON) + "\n")
	var out bytes.Buffer

	if err := Serve(in, &out, "", tools); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	lines := decodeLines(t, &out)
	if len(lines) != 1 {
		t.Fatalf("got %d response line(s), want 1 — the large line must not be split or dropped", len(lines))
	}
}
