package app_registry

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Kubernetes ServiceAccount-mounted credentials and in-cluster API
// endpoint locations. Paths match the kubelet default and are stable
// across K8s versions.
const (
	saTokenPath  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	saCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	// KUBERNETES_SERVICE_HOST / PORT are set by kubelet on every pod.
	// We read them at construction time to pick up service-proxy IPs
	// across clusters without touching env var defaults.
)

// K8sClient is a minimal Kubernetes API client scoped to the two
// operations the reconciler needs: read+patch Secrets in approved
// namespaces and trigger a rollout restart on a Deployment. Uses the
// pod's mounted ServiceAccount token — no client-go, no kubeconfig.
//
// Out-of-cluster (local dev) calls all return ErrNotInCluster so unit
// tests and `go run ./cmd/core` on a laptop don't accidentally hit a
// remote cluster with stale creds.
type K8sClient struct {
	apiBase string // https://kubernetes.default.svc:443
	token   string
	http    *http.Client
}

// ErrNotInCluster is returned by NewK8sClient when the ServiceAccount
// paths are missing; callers decide whether to disable app_registry or
// to log-and-continue.
var ErrNotInCluster = errors.New("app_registry: not running in a Kubernetes pod (no ServiceAccount token mounted)")

// NewK8sClient constructs a client that talks to the local kube-apiserver
// using the mounted ServiceAccount credentials. Returns ErrNotInCluster
// when the pod is not in K8s (e.g. running under docker-compose or
// locally for tests).
func NewK8sClient() (*K8sClient, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, ErrNotInCluster
	}
	tokenBytes, err := os.ReadFile(saTokenPath)
	if err != nil {
		return nil, ErrNotInCluster
	}
	caBytes, err := os.ReadFile(saCACertPath)
	if err != nil {
		return nil, fmt.Errorf("app_registry: read CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return nil, errors.New("app_registry: parse K8s CA cert failed")
	}

	return &K8sClient{
		apiBase: fmt.Sprintf("https://%s:%s", host, port),
		token:   strings.TrimSpace(string(tokenBytes)),
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs:    pool,
					MinVersion: tls.VersionTLS12,
				},
			},
		},
	}, nil
}

// GetSecretData reads a Secret and returns its data map decoded from
// base64. Absent secrets return (nil, nil) so callers can treat
// "doesn't exist yet" and "exists but empty" the same way.
func (k *K8sClient) GetSecretData(ctx context.Context, namespace, name string) (map[string][]byte, error) {
	path := fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, name)
	body, status, err := k.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("app_registry: get secret %s/%s returned %d: %s", namespace, name, status, body)
	}
	var res struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("app_registry: decode secret: %w", err)
	}
	out := make(map[string][]byte, len(res.Data))
	for k, v := range res.Data {
		dec, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("app_registry: decode secret key %q: %w", k, err)
		}
		out[k] = dec
	}
	return out, nil
}

// MergeSecretData writes (or creates) the given keys into Secret
// <namespace>/<name>. Existing keys not mentioned in `updates` are
// preserved. Returns true when the live Secret actually changed so
// callers can decide whether to trigger a rollout.
func (k *K8sClient) MergeSecretData(ctx context.Context, namespace, name string, updates map[string][]byte) (changed bool, err error) {
	existing, err := k.GetSecretData(ctx, namespace, name)
	if err != nil {
		return false, err
	}
	if existing == nil {
		// Create new Secret with just the updates.
		return true, k.createSecret(ctx, namespace, name, updates)
	}
	// Merge: copy existing, overlay updates, compare for change.
	merged := make(map[string][]byte, len(existing)+len(updates))
	for k2, v := range existing {
		merged[k2] = v
	}
	for k2, v := range updates {
		if prev, ok := merged[k2]; !ok || !bytes.Equal(prev, v) {
			changed = true
		}
		merged[k2] = v
	}
	if !changed {
		return false, nil
	}
	return true, k.patchSecretData(ctx, namespace, name, merged)
}

func (k *K8sClient) createSecret(ctx context.Context, namespace, name string, data map[string][]byte) error {
	encoded := map[string]string{}
	for k2, v := range data {
		encoded[k2] = base64.StdEncoding.EncodeToString(v)
	}
	payload, _ := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "Opaque",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"data": encoded,
	})
	path := fmt.Sprintf("/api/v1/namespaces/%s/secrets", namespace)
	body, status, err := k.do(ctx, http.MethodPost, path, payload, "application/json")
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("app_registry: create secret %s/%s returned %d: %s", namespace, name, status, body)
	}
	return nil
}

func (k *K8sClient) patchSecretData(ctx context.Context, namespace, name string, data map[string][]byte) error {
	encoded := map[string]string{}
	for k2, v := range data {
		encoded[k2] = base64.StdEncoding.EncodeToString(v)
	}
	payload, _ := json.Marshal(map[string]any{"data": encoded})
	path := fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", namespace, name)
	body, status, err := k.do(ctx, http.MethodPatch, path, payload, "application/strategic-merge-patch+json")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("app_registry: patch secret %s/%s returned %d: %s", namespace, name, status, body)
	}
	return nil
}

// TriggerRolloutRestart sets a `kubectl.kubernetes.io/restartedAt`
// annotation on the Deployment's pod template, mirroring what
// `kubectl rollout restart deployment/<name>` does. A present annotation
// is compared first so repeated calls within the same second collapse
// into a single write (and a single rollout).
func (k *K8sClient) TriggerRolloutRestart(ctx context.Context, namespace, name string) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	payload := fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`,
		ts,
	)
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s", namespace, name)
	body, status, err := k.do(ctx, http.MethodPatch, path, []byte(payload), "application/strategic-merge-patch+json")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("app_registry: restart deploy %s/%s returned %d: %s", namespace, name, status, body)
	}
	return nil
}

func (k *K8sClient) do(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.apiBase+path, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("app_registry: build k8s request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+k.token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := k.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("app_registry: k8s %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return respBody, resp.StatusCode, nil
}
