package leroymerlin

import "fmt"

// Kind classifies a failure. Each kind owns one process exit code, so the CLI
// layer never has to guess from an error string.
type Kind int

const (
	KindGeneric Kind = iota
	KindUsage
	KindAuth
	KindNotFound
	KindBlocked
	KindNetwork
	KindAPIChanged
)

// ExitCode maps the kind to the documented exit code (docs/cli-spec.md §7).
func (k Kind) ExitCode() int {
	switch k {
	case KindUsage:
		return 2
	case KindAuth:
		return 3
	case KindNotFound:
		return 4
	case KindBlocked:
		return 5
	case KindNetwork:
		return 6
	case KindAPIChanged:
		return 7
	}
	return 1
}

func (k Kind) String() string {
	switch k {
	case KindUsage:
		return "usage"
	case KindAuth:
		return "auth"
	case KindNotFound:
		return "not_found"
	case KindBlocked:
		return "blocked"
	case KindNetwork:
		return "network"
	case KindAPIChanged:
		return "api_changed"
	}
	return "generic"
}

// Error is every failure the client reports. Endpoint is a path without query
// values, Body a redacted excerpt; both feed the api-change issue link.
type Error struct {
	Kind      Kind
	Msg       string
	Status    int
	Endpoint  string
	Body      string
	Retryable bool
}

func (e *Error) Error() string { return e.Msg }

// Usage builds a caller-mistake error.
func Usage(format string, args ...any) error {
	return &Error{Kind: KindUsage, Msg: fmt.Sprintf(format, args...)}
}
