package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The tools Claude sees.
//
// Each description says when to reach for the tool, not just what it
// does — a tool description is the only thing the model has when deciding
// whether this is the moment to call it, and "lists tasks" does not
// answer that question.
//
// Deliberately absent: anything that deletes. A task somebody deleted by
// mistake is gone with its comments and its history, and nothing in the
// stated purpose of this server — writing down work so it does not have
// to be typed by hand — needs it. Closing a task is what "finished"
// means here.

type tool struct {
	name        string
	description string
	schema      map[string]any
	run         func(args map[string]any) (string, error)
}

type toolset struct {
	api   *apiClient
	tools []tool
}

// describe renders the list MCP's tools/list returns.
func (t *toolset) describe() []map[string]any {
	out := make([]map[string]any, 0, len(t.tools))
	for _, tl := range t.tools {
		out = append(out, map[string]any{
			"name":        tl.name,
			"description": tl.description,
			"inputSchema": tl.schema,
		})
	}
	return out
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// call runs one tool and renders the MCP result.
//
// A tool that fails returns isError with the reason as text, rather than
// a JSON-RPC error. That is MCP's own distinction and it matters: a
// protocol error says the server is broken, while an error result says
// this attempt did not work — which the model can read, understand, and
// act on ("the login expired, run devplatform-login").
func (t *toolset) call(raw json.RawMessage) map[string]any {
	var p callParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return errorResult("araç çağrısı çözümlenemedi: " + err.Error())
	}

	for _, tl := range t.tools {
		if tl.name != p.Name {
			continue
		}
		text, err := tl.run(p.Arguments)
		if err != nil {
			return errorResult(err.Error())
		}
		return textResult(text)
	}
	return errorResult("bilinmeyen araç: " + p.Name)
}

func textResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func errorResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}

// ---------------------------------------------------------------- args

// Arguments arrive as whatever JSON the model produced. These readers
// treat a missing value and an empty one alike, and never fail on a
// wrong type: a tool that refuses to run because a number arrived as a
// string spends a turn teaching the model JSON instead of doing the work.

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch v := args[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%g", v))
	case bool:
		return fmt.Sprintf("%t", v)
	}
	return ""
}

func argStrings(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// hasKey distinguishes "the model did not mention this field" from "the
// model set it to empty". Clearing a due date and leaving one alone are
// different requests, and the API takes an empty string to mean the
// former — so the difference has to survive all the way here.
func hasKey(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	_, ok := args[key]
	return ok
}

// schema is a small helper so each tool's input schema reads as a table
// rather than four levels of map literal.
func schema(required []string, properties map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func enumProp(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}
