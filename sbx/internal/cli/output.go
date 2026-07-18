// sbx/internal/cli/output.go
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gustavocarvalho/sbx/internal/driver"
)

type Printer struct {
	W    io.Writer
	JSON bool
}

func (p Printer) enc(v any) error {
	e := json.NewEncoder(p.W)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func (p Printer) Env(e driver.Env) error {
	if p.JSON {
		return p.enc(e)
	}
	_, err := fmt.Fprintf(p.W, "%s\t%s\t%s\n", e.ID, e.Status, e.Namespace)
	return err
}

func (p Printer) Envs(list []driver.Env) error {
	if p.JSON {
		return p.enc(list)
	}
	for _, e := range list {
		if err := p.Env(e); err != nil {
			return err
		}
	}
	return nil
}

func (p Printer) Exec(r driver.ExecResult) error {
	if p.JSON {
		return p.enc(r)
	}
	if r.Stdout != "" {
		fmt.Fprint(p.W, r.Stdout)
	}
	if r.Stderr != "" {
		fmt.Fprint(p.W, r.Stderr)
	}
	return nil
}

func (p Printer) Raw(s string) error {
	_, err := io.WriteString(p.W, s)
	return err
}
