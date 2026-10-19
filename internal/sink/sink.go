// Package sink delivers watch events somewhere other than stdout: an ntfy
// topic, a JSON webhook, or a local executable. Sinks are declared in
// config.toml; delivery is best effort, so a dead sink never blocks a check.
package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/n0nuser/leroymerlin-cli/internal/config"
	"github.com/n0nuser/leroymerlin-cli/internal/store"
)

// Validate checks a sink config without sending anything.
func Validate(name string, cfg config.SinkConfig) error {
	switch cfg.Type {
	case "ntfy":
		if cfg.URL == "" || cfg.Topic == "" {
			return fmt.Errorf("sink %q: ntfy needs url and topic", name)
		}
	case "webhook":
		if cfg.URL == "" {
			return fmt.Errorf("sink %q: webhook needs url", name)
		}
	case "exec":
		if len(cfg.Command) == 0 {
			return fmt.Errorf("sink %q: exec needs command", name)
		}
	default:
		return fmt.Errorf("sink %q has unknown type %q (want ntfy, webhook or exec)", name, cfg.Type)
	}
	return nil
}

// Deliver sends one event to one sink.
func Deliver(ctx context.Context, name string, cfg config.SinkConfig, ev store.Event) error {
	switch cfg.Type {
	case "ntfy":
		return ntfy(ctx, cfg, ev)
	case "webhook":
		return webhook(ctx, cfg, ev)
	case "exec":
		return execSink(ctx, cfg, ev)
	}
	return fmt.Errorf("sink %q has unknown type %q", name, cfg.Type)
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func ntfy(ctx context.Context, cfg config.SinkConfig, ev store.Event) error {
	title, body, _ := Summary(ev)
	url := strings.TrimRight(cfg.URL, "/") + "/" + cfg.Topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	if tok, err := token(cfg.TokenFile); err == nil && tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy answered HTTP %d", resp.StatusCode)
	}
	return nil
}

func webhook(ctx context.Context, cfg config.SinkConfig, ev store.Event) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook answered HTTP %d", resp.StatusCode)
	}
	return nil
}

func execSink(ctx context.Context, cfg config.SinkConfig, ev store.Event) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Stdin = bytes.NewReader(append(raw, '\n'))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func token(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// Summary is the one-line human text used by ntfy and chat webhooks.
func Summary(ev store.Event) (title, body, link string) {
	var p store.ProductPayload
	_ = json.Unmarshal(ev.Product, &p)
	var change struct {
		From any `json:"from"`
		To   any `json:"to"`
	}
	_ = json.Unmarshal(ev.Change, &change)
	name := p.Name
	if name == "" {
		name = p.Ref
	}
	switch ev.Type {
	case "product.new":
		title = "Nuevo: " + name
		body = price(p)
	case "product.price_changed":
		title = "Precio: " + name
		body = fmt.Sprintf("%v -> %v", change.From, change.To)
	case "product.in_stock":
		title = "Disponible: " + name
		body = price(p)
	case "product.out_of_stock":
		title = "Agotado: " + name
		body = price(p)
	case "product.stock_changed":
		title = "Stock: " + name
		body = fmt.Sprintf("%v -> %v", change.From, change.To)
	case "product.removed":
		title = "Fuera del resultado: " + name
		body = price(p)
	default:
		title = ev.Type
		body = name
	}
	return title, body, p.URL
}

func price(p store.ProductPayload) string {
	if p.Price == nil {
		return ""
	}
	return fmt.Sprintf("%.2f %s", p.Price.Amount, p.Price.Currency)
}
