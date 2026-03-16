// Package temporal provides Temporal client, worker, workflows and activities
// for lurus-platform subscription lifecycle management.
package temporal

import (
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/client"
)

// NewClient creates a Temporal client connected to the given host:port.
// Returns (nil, nil) when hostPort is empty, allowing graceful degradation.
func NewClient(hostPort, namespace string) (client.Client, error) {
	if hostPort == "" {
		slog.Info("temporal: disabled (TEMPORAL_HOST_PORT not set)")
		return nil, nil
	}
	if namespace == "" {
		namespace = "default"
	}

	c, err := client.Dial(client.Options{
		HostPort:  hostPort,
		Namespace: namespace,
		Logger:    newSlogAdapter(),
	})
	if err != nil {
		return nil, fmt.Errorf("temporal: dial %s: %w", hostPort, err)
	}
	slog.Info("temporal: connected", "host", hostPort, "namespace", namespace)
	return c, nil
}
