package synthexport

import (
	"bufio"
	"context"
	"strings"
)

// spaces indents an element: a real export adds one space per level.
const spaces = "     "

// escaper leaves apostrophes as they are, as a real export does. encoding/xml would escape them.
var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// document writes XML the way the Health app does: one element per line, and attributes in the
// order they are given, as name and value pairs. An empty value leaves its attribute out.
type document struct {
	w *bufio.Writer
}

// write adds raw text. A bufio.Writer keeps its first error, which flush returns.
func (d document) write(parts ...string) {
	for _, part := range parts {
		_, _ = d.w.WriteString(part)
	}
}

// empty writes an element without content.
func (d document) empty(depth int, name string, attrs ...string) {
	d.write(spaces[:depth], "<", name)

	for i := 0; i+1 < len(attrs); i += 2 {
		if attrs[i+1] != "" {
			d.write(" ", attrs[i], `="`, escaper.Replace(attrs[i+1]), `"`)
		}
	}

	d.write("/>\n")
}

// closeTag writes an end tag.
func (d document) closeTag(depth int, name string) {
	d.write(spaces[:depth], "</", name, ">\n")
}

// flush passes on what is buffered, and stops the export when ctx is done or a write has failed.
func (d document) flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return d.w.Flush()
}
