package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/n0nuser/leroymerlin-cli/internal/config"
	"github.com/n0nuser/leroymerlin-cli/internal/leroymerlin"
)

// TestLiveSearch is the only test that talks to leroymerlin.es. It is off
// unless LEROYMERLIN_LIVE_TEST=1, and it needs a stored session.
//
//	LEROYMERLIN_LIVE_TEST=1 go test ./internal/cli -run TestLiveSearch -count=1
func TestLiveSearch(t *testing.T) {
	if os.Getenv("LEROYMERLIN_LIVE_TEST") != "1" {
		t.Skip("live test: set LEROYMERLIN_LIVE_TEST=1 to run it")
	}
	paths := config.DefaultPaths()
	cfg, _ := config.Load(paths.ConfigFile)
	profile := config.ResolveProfile("", cfg)
	session, err := config.LoadSession(paths, profile)
	if err != nil {
		t.Fatalf("the stored session is unusable: %v. Re-import it with `leroymerlin auth harvest`", err)
	}
	if session == nil {
		t.Skipf("live test: no stored session for profile %q; run `leroymerlin auth harvest` first", profile)
	}
	c := leroymerlin.New()
	c.Session = &session.Session
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	q := leroymerlin.SearchQuery{Keywords: "cemento cola"}
	p, err := c.GetHTML(ctx, q.Path())
	if err != nil {
		t.Fatalf("live search failed: %v", err)
	}
	if p.Status != 200 {
		t.Fatalf("live search status = %d, want 200", p.Status)
	}
	res, err := leroymerlin.ParseSearch(p.Body)
	if err != nil {
		t.Fatalf("live search did not parse: %v", err)
	}
	if len(res.Products) == 0 {
		t.Fatalf("live search returned no products (total %d)", res.Total)
	}
	t.Logf("live search: %d products, %d total, first %s", len(res.Products), res.Total, res.Products[0].Name)
}
