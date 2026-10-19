// Package output renders results (json, jsonl, pretty) and errors (text or a
// JSON envelope), and owns the mapping from errors to exit codes.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
)

// Format is the stdout rendering.
type Format string

const (
	JSON   Format = "json"
	JSONL  Format = "jsonl"
	Pretty Format = "pretty"
)

// ParseFormat validates --format.
func ParseFormat(s string) (Format, error) {
	switch f := Format(s); f {
	case JSON, JSONL, Pretty:
		return f, nil
	}
	return "", Usagef("unknown format %q. Use json, jsonl or pretty", s)
}

// Prettier is implemented by results that have a human rendering. Anything
// else prints as indented JSON under --format pretty.
type Prettier interface {
	Pretty(w io.Writer, color bool)
}

// Printer writes one result per command to stdout.
type Printer struct {
	W      io.Writer
	Format Format
	Color  bool
}

// Print renders v. Under jsonl a value that marshals to an array is written
// one element per line.
func (p Printer) Print(v any) error {
	switch p.Format {
	case JSONL:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var items []json.RawMessage
		if len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &items) == nil {
			for _, it := range items {
				if _, err := fmt.Fprintf(p.W, "%s\n", it); err != nil {
					return err
				}
			}
			return nil
		}
		_, err = fmt.Fprintf(p.W, "%s\n", raw)
		return err
	case Pretty:
		if pr, ok := v.(Prettier); ok {
			pr.Pretty(p.W, p.Color)
			return nil
		}
	}
	enc := json.NewEncoder(p.W)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table writes tab-aligned columns; the header is bold when colour is on.
func Table(w io.Writer, color bool, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if len(header) > 0 {
		h := strings.Join(header, "\t")
		if color {
			h = "\x1b[1m" + h + "\x1b[0m"
		}
		fmt.Fprintln(tw, h)
	}
	for _, r := range rows {
		for i := range r {
			r[i] = Clean(r[i])
		}
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// Clean strips control characters from remote text before it reaches a
// terminal, so a product name cannot carry escape sequences.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
}

// Truncate cuts s to n runes with an ellipsis.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Dim wraps s in the faint style when colour is on.
func Dim(s string, color bool) string {
	if !color {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

// ErrorFormat selects the stderr rendering of errors.
type ErrorFormat string

const (
	ErrText ErrorFormat = "text"
	ErrJSON ErrorFormat = "json"
)

// Envelope is the machine-readable error printed with --error-format json.
type Envelope struct {
	Code              int      `json:"code"`
	Category          string   `json:"category"`
	Retryable         bool     `json:"retryable"`
	Message           string   `json:"message"`
	HTTPStatus        int      `json:"http_status,omitempty"`
	Endpoint          string   `json:"endpoint,omitempty"`
	SuggestedCommands []string `json:"suggested_commands,omitempty"`
	IssueURL          string   `json:"issue_url,omitempty"`
}

// UsageError marks a caller mistake found in the CLI layer.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Usagef builds a UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// Repo is the project home, used for issue links and the updater.
const Repo = "https://github.com/n0nuser/leroymerlin-cli"

// Classify turns any error into its envelope. command is the command path
// without argument values.
func Classify(err error, version, command string) Envelope {
	env := Envelope{Code: 1, Category: "generic", Message: err.Error()}
	var le *leroymerlin.Error
	var ue *UsageError
	switch {
	case errors.As(err, &ue):
		env.Code, env.Category = 2, "usage"
	case errors.As(err, &le):
		env.Code = le.Kind.ExitCode()
		env.Category = le.Kind.String()
		env.Retryable = le.Retryable
		env.HTTPStatus = le.Status
		env.Endpoint = le.Endpoint
		switch le.Kind {
		case leroymerlin.KindAuth:
			env.SuggestedCommands = []string{"leroymerlin auth harvest", "leroymerlin auth status"}
		case leroymerlin.KindBlocked:
			env.SuggestedCommands = []string{"leroymerlin auth harvest", "leroymerlin doctor"}
		case leroymerlin.KindAPIChanged:
			env.IssueURL = IssueURL(version, command, le)
			env.SuggestedCommands = []string{"leroymerlin doctor", "rerun with --debug"}
		}
	}
	return env
}

// IssueURL prefills .github/ISSUE_TEMPLATE/api-change.yml. Argument values
// (search terms, store names) are never included.
func IssueURL(version, command string, e *leroymerlin.Error) string {
	q := url.Values{}
	q.Set("template", "api-change.yml")
	q.Set("title", "[api change] "+strings.TrimSpace(e.Endpoint+" "+command))
	q.Set("version", version)
	q.Set("os", runtime.GOOS+"/"+runtime.GOARCH)
	q.Set("command", command)
	q.Set("endpoint", e.Endpoint)
	if e.Status != 0 {
		q.Set("status", fmt.Sprint(e.Status))
	}
	q.Set("body_excerpt", e.Body)
	return Repo + "/issues/new?" + q.Encode()
}

// PrintError writes err to w and returns the exit code.
func PrintError(w io.Writer, err error, format ErrorFormat, version, command string) int {
	env := Classify(err, version, command)
	if format == ErrJSON {
		_ = json.NewEncoder(w).Encode(env)
		return env.Code
	}
	fmt.Fprintf(w, "leroymerlin: %s\n", env.Message)
	switch {
	case env.IssueURL != "":
		fmt.Fprintf(w, "\nleroymerlin.es may have changed. Please report it (the form is prefilled):\n  %s\n", env.IssueURL)
		fmt.Fprintln(w, "rerun with --debug for the request log.")
	case len(env.SuggestedCommands) > 0:
		fmt.Fprintf(w, "  try: %s\n", strings.Join(env.SuggestedCommands, "  |  "))
	}
	return env.Code
}

// ColorEnabled honours --no-color, NO_COLOR, TERM=dumb and whether f is a TTY.
func ColorEnabled(noColor bool, f *os.File) bool {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(f)
}

// IsTerminal reports whether f is a character device.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
