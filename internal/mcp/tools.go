package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Param is one tool argument. Flag is the CLI flag it becomes; empty Flag
// means a positional argument.
type Param struct {
	Name        string
	Type        string // string, integer, boolean
	Description string
	Flag        string
	Required    bool
}

// Tool is an MCP tool backed by one CLI command.
type Tool struct {
	Name        string
	Description string
	Command     []string
	Params      []Param
}

// Tools is every tool offered. All are read-only, like the CLI.
var Tools = []Tool{
	{
		Name: "store_list", Command: []string{"store", "list"},
		Description: "List Leroy Merlin stores in Spain (public sitemap, no session). Mirrors `leroymerlin store list`; returns [{slug,name,compact,url}].",
		Params: []Param{
			{Name: "query", Type: "string", Flag: "--query", Description: "only stores whose name or slug contains this"},
			{Name: "compact", Type: "boolean", Flag: "--compact", Description: "only Compact-format stores"},
		},
	},
	{
		Name: "category_list", Command: []string{"category", "list"},
		Description: "List product categories (public sitemap, no session). Mirrors `leroymerlin category list`; returns [{path,name,depth,parent,url}].",
		Params: []Param{
			{Name: "parent", Type: "string", Flag: "--parent", Description: "only categories below this path"},
			{Name: "depth", Type: "integer", Flag: "--depth", Description: "levels below parent to include, 0 for all"},
			{Name: "query", Type: "string", Flag: "--query", Description: "only categories whose name or path contains this"},
		},
	},
	{
		Name: "doctor", Command: []string{"doctor"},
		Description: "Check the session and whether the storefront accepts it or challenges it. Mirrors `leroymerlin doctor`.",
	},
}

// ToolNames lists the tool names in order.
func ToolNames() []string {
	out := make([]string, len(Tools))
	for i, t := range Tools {
		out[i] = t.Name
	}
	return out
}

func lookup(name string) (Tool, bool) {
	for _, t := range Tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// MarshalJSON renders the tool as MCP's {name, description, inputSchema}.
func (t Tool) MarshalJSON() ([]byte, error) {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		props[p.Name] = map[string]string{"type": p.Type, "description": p.Description}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return json.Marshal(map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema})
}

// argv validates the arguments and builds the command line. Positionals go
// after "--", so a value starting with a dash is never read as a flag.
func (t Tool) argv(args map[string]any) ([]string, error) {
	known := map[string]Param{}
	for _, p := range t.Params {
		known[p.Name] = p
	}
	for k := range args {
		if _, ok := known[k]; !ok {
			return nil, fmt.Errorf("tool %s has no argument %q", t.Name, k)
		}
	}
	out := append([]string{}, t.Command...)
	var rest []string
	for _, p := range t.Params {
		v, ok := args[p.Name]
		if !ok {
			if p.Required {
				return nil, fmt.Errorf("tool %s needs %q", t.Name, p.Name)
			}
			continue
		}
		var s string
		switch p.Type {
		case "string":
			str, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string", p.Name)
			}
			s = str
		case "integer":
			f, ok := v.(float64)
			if !ok || f != math.Trunc(f) {
				return nil, fmt.Errorf("%s must be an integer", p.Name)
			}
			s = fmt.Sprint(int64(f))
		case "boolean":
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%s must be a boolean", p.Name)
			}
			if !b {
				continue
			}
			out = append(out, p.Flag)
			continue
		}
		if p.Flag == "" {
			rest = append(rest, s)
		} else {
			out = append(out, p.Flag+"="+s)
		}
	}
	if len(rest) > 0 {
		out = append(append(out, "--"), rest...)
	}
	return out, nil
}

// Describe is a one-line summary for help text.
func Describe() string { return strings.Join(ToolNames(), ", ") }
