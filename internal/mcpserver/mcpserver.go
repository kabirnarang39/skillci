// Package mcpserver implements skillci's MCP (Model Context Protocol) stdio
// server — `skillci mcp-serve` — so an agent calls skillci as a native tool
// instead of shelling out to a CLI it has to remember exists. Wire format
// verified against the MCP spec (2025-06-18) directly: JSON-RPC 2.0
// messages, one per line, newline-delimited, no Content-Length framing
// (that's LSP, not MCP).
//
// This package is deliberately tool-agnostic: it only implements the
// JSON-RPC/MCP protocol plumbing (initialize handshake, tools/list,
// tools/call dispatch, error shapes). The actual tools — which real
// skillci CLI commands they wrap, with which flags — are supplied by the
// caller as a []Tool, so the protocol engine can be tested in isolation
// from any specific command's business logic. See cmd/skillci/mcptools.go
// for the concrete registry (every skillci command except mcp-serve
// itself, each invoked as the real cobra command in-process rather than
// reimplemented — the only way "expose every feature" can't drift from
// what the CLI actually does).
package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const protocolVersion = "2025-06-18"

// Tool is one MCP tool this server exposes over tools/list and tools/call.
type Tool struct {
	Name        string
	Title       string
	Description string
	// InputSchema is the tool's JSON Schema (the exact map that goes into
	// the "inputSchema" field of a tools/list response).
	InputSchema map[string]interface{}
	// Handler runs the tool against a tools/call request's raw
	// "arguments" object. text/isError are MCP's own tool-result shape
	// (isError marks a business-logic failure the caller should act on —
	// lint findings, a failed eval case — same as the CLI's own non-zero
	// exit code). A non-nil err instead means a protocol-level problem
	// (malformed arguments) and produces a JSON-RPC error response, not a
	// tool result.
	Handler func(arguments json.RawMessage) (text string, isError bool, err error)
}

// rpcEnvelope is used to inspect an incoming line before deciding how to
// respond. ID is a pointer so a JSON-RPC *notification* (no "id" member at
// all, e.g. "notifications/initialized") is distinguishable from a
// *request* with a null id — both parse to a non-nil Params, but only the
// former must never receive a response, per the JSON-RPC 2.0 spec.
type rpcEnvelope struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes this server can return.
const (
	errCodeParseError     = -32700
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
)

// Serve runs the MCP stdio message loop until in reaches EOF or a fatal
// write error occurs. toolsetVersion is reported in initialize's
// serverInfo so a client can tell which skillci build it's talking to.
func Serve(in io.Reader, out io.Writer, toolsetVersion string, tools []Tool) error {
	toolsByName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		toolsByName[t.Name] = t
	}
	d := &dispatcher{version: toolsetVersion, tools: tools, toolsByName: toolsByName}

	// bufio.Reader.ReadString has no line-length ceiling (unlike
	// bufio.Scanner's default 64KB token limit) — a check/eval result for
	// a large skill could exceed that, and MCP messages must never be
	// silently truncated.
	reader := bufio.NewReader(in)

	for {
		line, readErr := reader.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if err := handleLine(trimmed, out, d); err != nil {
				return err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

// dispatcher closes over the version string and tool registry so no
// package-level mutable state is needed — each Serve call gets its own,
// which also makes concurrent/repeated tests of Serve safe.
type dispatcher struct {
	version     string
	tools       []Tool
	toolsByName map[string]Tool
}

// handleLine parses one JSON-RPC message and, for requests (not
// notifications), writes exactly one response line. A fatal error here
// means the response itself couldn't be written — a malformed *request*
// from the client is reported back as a JSON-RPC error response, not
// returned as a Go error, since the server must keep running.
func handleLine(line string, out io.Writer, d *dispatcher) error {
	var env rpcEnvelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		// Per JSON-RPC 2.0: a Parse error has no way to know the
		// request's id, so id is null in the response.
		return writeResponse(out, rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: errCodeParseError, Message: "invalid JSON: " + err.Error()},
		})
	}

	isNotification := env.ID == nil
	result, rpcErr := d.dispatch(env.Method, env.Params)

	if isNotification {
		// The client never expects a response to a notification
		// (e.g. "notifications/initialized"), regardless of whether
		// dispatch succeeded — writing one would itself violate "the
		// server MUST NOT write anything to stdout that is not a
		// valid MCP message" by sending an unsolicited response.
		return nil
	}

	resp := rpcResponse{JSONRPC: "2.0", ID: *env.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	return writeResponse(out, resp)
}

func writeResponse(out io.Writer, resp rpcResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshaling MCP response: %w", err)
	}
	// One write call for the full line (data + newline) so a concurrent
	// writer to the same stream — not expected in practice, since stdio
	// MCP is single-threaded request/response, but cheap to guarantee —
	// can never interleave a partial line.
	_, err = out.Write(append(data, '\n'))
	return err
}

func (d *dispatcher) dispatch(method string, params json.RawMessage) (interface{}, *rpcError) {
	switch method {
	case "initialize":
		return d.handleInitialize(params)
	case "tools/list":
		return d.handleToolsList()
	case "tools/call":
		return d.handleToolsCall(params)
	case "ping":
		return map[string]interface{}{}, nil
	default:
		return nil, &rpcError{Code: errCodeMethodNotFound, Message: "method not found: " + method}
	}
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func (d *dispatcher) handleInitialize(params json.RawMessage) (interface{}, *rpcError) {
	var p initializeParams
	_ = json.Unmarshal(params, &p) // a missing/malformed protocolVersion just falls through to our own version below

	// Version negotiation per spec: respond with the client's version if
	// we support it, otherwise our own latest supported version. skillci
	// only implements one version, so this is the whole negotiation.
	negotiated := protocolVersion
	if p.ProtocolVersion == protocolVersion {
		negotiated = p.ProtocolVersion
	}

	version := d.version
	if version == "" {
		version = "unknown"
	}

	return map[string]interface{}{
		"protocolVersion": negotiated,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "skillci",
			"version": version,
		},
	}, nil
}

func (d *dispatcher) handleToolsList() (interface{}, *rpcError) {
	list := make([]map[string]interface{}, 0, len(d.tools))
	for _, t := range d.tools {
		list = append(list, map[string]interface{}{
			"name":        t.Name,
			"title":       t.Title,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return map[string]interface{}{"tools": list}, nil
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (d *dispatcher) handleToolsCall(params json.RawMessage) (interface{}, *rpcError) {
	var p toolCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: errCodeInvalidParams, Message: "invalid tools/call params: " + err.Error()}
	}

	tool, ok := d.toolsByName[p.Name]
	if !ok {
		return nil, &rpcError{Code: errCodeMethodNotFound, Message: "unknown tool: " + p.Name}
	}

	text, isErr, err := tool.Handler(p.Arguments)
	if err != nil {
		return nil, &rpcError{Code: errCodeInvalidParams, Message: err.Error()}
	}

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
		"isError": isErr,
	}, nil
}
