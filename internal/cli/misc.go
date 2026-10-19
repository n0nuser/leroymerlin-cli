package cli

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/n0nuser/leroymerlin-cli/internal/config"
	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
	"github.com/n0nuser/leroymerlin-cli/internal/output"
)

// config

func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write config.toml",
		Long: `Read and write config.toml. Keys are dotted paths; unknown keys are refused.
Sessions are not in this file: they live 0600 under the sessions directory.

Examples:
  leroymerlin config path
  leroymerlin config set default_profile madrid
  leroymerlin config get default_profile`,
	}
	cmd.AddCommand(
		&cobra.Command{Use: "path", Short: "Print where config, sessions and state live", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			return a.Printer.Print(map[string]string{"config": a.Paths.ConfigFile, "sessions": a.Paths.SessionsDir, "state": a.Paths.StateDB})
		}},
		&cobra.Command{Use: "list", Short: "Print the whole config", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			return a.Printer.Print(a.Cfg)
		}},
		&cobra.Command{Use: "get KEY", Short: "Print one key", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := a.configDoc()
			if err != nil {
				return err
			}
			var cur any = doc
			for _, part := range strings.Split(args[0], ".") {
				m, ok := cur.(map[string]any)
				if !ok {
					return output.Usagef("config has no key %q", args[0])
				}
				if cur, ok = m[part]; !ok {
					return output.Usagef("config has no key %q", args[0])
				}
			}
			return a.Printer.Print(cur)
		}},
		&cobra.Command{Use: "set KEY VALUE", Short: "Set one key", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := a.configDoc()
			if err != nil {
				return err
			}
			parts := strings.Split(args[0], ".")
			m := doc
			for _, part := range parts[:len(parts)-1] {
				next, ok := m[part].(map[string]any)
				if !ok {
					next = map[string]any{}
					m[part] = next
				}
				m = next
			}
			m[parts[len(parts)-1]] = args[1]
			raw, err := toml.Marshal(doc)
			if err != nil {
				return err
			}
			// Validate against the schema before anything reaches the disk.
			dec := toml.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			var next config.Config
			if err := dec.Decode(&next); err != nil {
				return output.Usagef("%q is not a valid config key or value: %v", args[0], err)
			}
			if next.DefaultProfile != "" {
				if err := config.ValidProfile(next.DefaultProfile); err != nil {
					return output.Usagef("%v", err)
				}
			}
			if err := config.Save(a.Paths.ConfigFile, next); err != nil {
				return err
			}
			a.Cfg = next
			return a.Printer.Print(map[string]any{args[0]: args[1]})
		}},
	)
	return cmd
}

func (a *App) configDoc() (map[string]any, error) {
	doc := map[string]any{}
	raw, err := os.ReadFile(a.Paths.ConfigFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", a.Paths.ConfigFile, err)
	}
	return doc, nil
}

// doctor

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type doctorReport struct {
	OK          bool          `json:"ok"`
	Profile     string        `json:"profile"`
	SessionOK   *bool         `json:"session_valid,omitempty"`
	Challenged  bool          `json:"challenged"`
	Checks      []doctorCheck `json:"checks"`
	failureKind leroymerlin.Kind
}

func (r doctorReport) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(r.Checks))
	for _, c := range r.Checks {
		mark := "ok"
		if !c.OK {
			mark = "FAIL"
		}
		rows = append(rows, []string{mark, c.Name, c.Detail})
	}
	output.Table(w, color, nil, rows)
}

func (a *App) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check config, session, sitemaps and whether the storefront accepts the session",
		Long: `Check that everything the cli needs works, in order: config, the stored
session (cookie names, the datadome cookie and its expiry), the public
sitemaps, and one request to the storefront with the session.

The last check tells a working session from one the bot protection
challenges. Exit 0 when all pass, 3 when there is no session, 5 when the
storefront challenges the session, 1 for anything else.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rep := doctorReport{OK: true, Profile: a.Profile}
			add := func(name string, ok bool, detail string, kind leroymerlin.Kind) {
				rep.Checks = append(rep.Checks, doctorCheck{Name: name, OK: ok, Detail: detail})
				if !ok && rep.OK {
					rep.OK, rep.failureKind = false, kind
				}
			}
			add("config", true, a.Paths.ConfigFile, 0)
			switch s := a.Session; {
			case s == nil:
				add("session", false, fmt.Sprintf("profile %q has none; run `leroymerlin auth login --cookies FILE`", a.Profile), leroymerlin.KindAuth)
			default:
				sum := s.Summarize()
				add("session", true, fmt.Sprintf("%s, %d cookies (%s)", s.Source, sum.Cookies, s.Path), 0)
				switch {
				case !sum.HasDataDome:
					add("datadome cookie", false, "missing; export the cookies again with a leroymerlin.es tab open", leroymerlin.KindAuth)
				case sum.Expires != nil:
					add("datadome cookie", true, "expires "+sum.Expires.Format(time.DateOnly), 0)
				default:
					add("datadome cookie", true, "present", 0)
				}
				ua := sum.UserAgent
				if ua == "" {
					ua = "default (" + leroymerlin.DefaultUserAgent + "); a HAR import or --user-agent records your browser's"
				}
				add("user agent", true, ua, 0)
			}
			if stores, err := a.Client.Stores(ctx); err != nil {
				add("sitemaps", false, err.Error(), kindOf(err))
			} else {
				add("sitemaps", true, fmt.Sprintf("%d stores", len(stores)), 0)
			}
			if a.Session != nil {
				ok, err := a.checkStorefront(ctx)
				rep.SessionOK = &ok
				if err != nil {
					var le *leroymerlin.Error
					rep.Challenged = errors.As(err, &le) && le.Kind == leroymerlin.KindBlocked
					add("storefront", false, err.Error(), kindOf(err))
				} else {
					add("storefront", true, a.Client.BaseURL+" accepts the session", 0)
				}
			}
			if err := a.Printer.Print(rep); err != nil {
				return err
			}
			if !rep.OK {
				return &leroymerlin.Error{Kind: rep.failureKind, Msg: "doctor found problems"}
			}
			return nil
		},
	}
}

func kindOf(err error) leroymerlin.Kind {
	var le *leroymerlin.Error
	if errors.As(err, &le) {
		return le.Kind
	}
	return leroymerlin.KindGeneric
}

// skills: embedded agent-facing docs

//go:embed skills/*.md
var skillFiles embed.FS

func (a *App) skillsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "skills", Short: "Embedded usage docs for agents and humans"}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List the docs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
			names, err := skillNames()
			if err != nil {
				return err
			}
			return a.Printer.Print(names)
		}},
		&cobra.Command{Use: "get NAME", Short: "Print one doc as markdown", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := skillFiles.ReadFile("skills/" + args[0] + ".md")
			if err != nil {
				names, _ := skillNames()
				return output.Usagef("no skill %q. Available: %s", args[0], strings.Join(names, ", "))
			}
			_, err = a.Stdout.Write(raw)
			return err
		}},
	)
	return cmd
}

func skillNames() ([]string, error) {
	entries, err := fs.ReadDir(skillFiles, "skills")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names, nil
}
