package cli

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/spf13/cobra"

	"github.com/gauthier-se/verve/internal/config"
)

// healthcheckTimeout is how long healthcheck waits for the server by default.
const healthcheckTimeout = 3 * time.Second

func newHealthcheckCommand(loadConfig func() (config.Config, error), timeout time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "healthcheck",
		Short: "Call /healthz on the configured listen address and exit non-zero on failure",
		Args:  cobra.NoArgs,
		RunE: withConfig(loadConfig, func(cmd *cobra.Command, cfg config.Config) error {
			client := &http.Client{Timeout: cmp.Or(timeout, healthcheckTimeout)}

			return healthcheck(cmd.Context(), client, cfg.ListenAddr)
		}),
	}
}

// healthcheck calls /healthz on the server listening on listenAddr and fails unless it answers 200.
func healthcheck(ctx context.Context, client *http.Client, listenAddr string) error {
	url, err := healthzURL(listenAddr)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}

	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck %s: unexpected status %s", url, res.Status)
	}

	return nil
}

// healthzURL returns the URL of /healthz on the server listening on listenAddr. A server
// listening on every address, with an empty or unspecified host, is reached on the IPv4
// loopback: Go listens on both IPv4 and IPv6 there, and IPv6 may be disabled, as in Docker.
func healthzURL(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", listenAddr, err)
	}

	if ip, err := netip.ParseAddr(host); host == "" || (err == nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}
