// Command readmecmds prints the table of commands and MCP tools in docs/developer/commands.md
// from the command registry, so that the table never lists a command the registry does not have:
// the "All N commands" summary line, then one row per command, sorted by id, with its MCP tool
// name, danger, parameters (optional ones marked ?) and the first sentence of its description.
// Its output replaces the content of that page's details block.
//
//	go run ./hack/readmecmds > /tmp/table.md
package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

func main() {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cmds := r.Commands()
	fmt.Printf("<summary>All %d commands</summary>\n\n", len(cmds))
	fmt.Println("| Command | MCP tool | Danger | Parameters | What it does |")
	fmt.Println("|---|---|---|---|---|")
	for _, c := range cmds {
		var notes []string
		if c.Escalates {
			notes = append(notes, "destructive for some calls")
		}
		if c.AgentsAsk {
			notes = append(notes, "agents always ask for some calls")
		}
		if c.GUIOnly {
			notes = append(notes, "GUI only")
		}
		danger := string(c.Danger)
		if len(notes) > 0 {
			danger += " (" + strings.Join(notes, "; ") + ")"
		}
		fmt.Printf("| `%s` | `%s` | %s | %s | %s |\n", c.ID, c.ToolName(), danger, params(c), firstSentence(c.Description))
	}
}

// params lists the schema's properties, sorted, the optional ones marked with "?".
func params(c *engine.Command) string {
	if c.Schema == nil {
		return ""
	}
	names := make([]string, 0, len(c.Schema.Properties))
	for n := range c.Schema.Properties {
		names = append(names, n)
	}
	slices.Sort(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		p := "`" + n + "`"
		if !slices.Contains(c.Schema.Required, n) {
			p += "?"
		}
		out = append(out, p)
	}
	return strings.Join(out, ", ")
}

// firstSentence is the description up to the period that ends its first sentence (one followed
// by a space and a capital letter, not part of "...", "e.g." or "i.e."), pipes escaped.
func firstSentence(d string) string {
	for i := 0; i+2 < len(d); i++ {
		if d[i] != '.' || d[i+1] != ' ' || d[i+2] < 'A' || d[i+2] > 'Z' {
			continue
		}
		if (i > 0 && d[i-1] == '.') || strings.HasSuffix(d[:i], "e.g") || strings.HasSuffix(d[:i], "i.e") {
			continue
		}
		d = d[:i]
		break
	}
	d = strings.TrimSuffix(strings.TrimSpace(d), ".")
	return strings.ReplaceAll(d, "|", `\|`)
}
