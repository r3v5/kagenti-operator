/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package mlflow provides a minimal REST API client for MLflow experiment management.
// The client authenticates using a Kubernetes ServiceAccount token and sets the
// X-MLFLOW-WORKSPACE header to scope operations to a specific namespace/workspace.
package mlflow

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultTokenPath is the projected SA token path in a pod.
	DefaultTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// DefaultCACertPath is the service-serving CA certificate path.
	// On OpenShift, the service-ca.crt is projected into the SA token volume.
	DefaultCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"

	// WorkspaceHeader is the MLflow workspace header (namespace-based isolation).
	WorkspaceHeader = "X-MLFLOW-WORKSPACE"
)

// Client is a minimal MLflow REST API client for experiment management.
type Client struct {
	// BaseURL is the MLflow tracking server URL
	BaseURL string

	// TokenPath is the path to the SA token file. Defaults to DefaultTokenPath.
	TokenPath string

	// CACertPath is the path to the CA certificate for TLS verification.
	// Defaults to the in-cluster SA CA cert.
	CACertPath string

	// HTTPClient is the HTTP client to use. If nil, a default client with 30s timeout is used.
	HTTPClient *http.Client

	httpOnce sync.Once
}

// createExperimentRequest is the request body for POST /api/2.0/mlflow/experiments/create.
type createExperimentRequest struct {
	Name string `json:"name"`
}

// createExperimentResponse is the response body from experiments/create.
type createExperimentResponse struct {
	ExperimentID string `json:"experiment_id"`
}

// getExperimentByNameResponse is the response body from experiments/get-by-name.
type getExperimentByNameResponse struct {
	Experiment struct {
		ExperimentID   string `json:"experiment_id"`
		LifecycleStage string `json:"lifecycle_stage"`
	} `json:"experiment"`
}

// restoreExperimentRequest is the request body for POST /api/2.0/mlflow/experiments/restore.
type restoreExperimentRequest struct {
	ExperimentID string `json:"experiment_id"`
}

// mlflowError represents an MLflow API error response.
type mlflowError struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

func (e *mlflowError) Error() string {
	return fmt.Sprintf("mlflow: %s: %s", e.ErrorCode, e.Message)
}

// IsResourceAlreadyExists returns true if the error is a RESOURCE_ALREADY_EXISTS error.
func IsResourceAlreadyExists(err error) bool {
	var e *mlflowError
	if errors.As(err, &e) {
		return e.ErrorCode == "RESOURCE_ALREADY_EXISTS"
	}
	return false
}

func (c *Client) httpClient() *http.Client {
	c.httpOnce.Do(func() {
		if c.HTTPClient == nil {
			tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
			if caCert, err := os.ReadFile(c.caCertPath()); err == nil {
				pool, err := x509.SystemCertPool()
				if err != nil {
					// Fall back to an empty pool; the service-CA cert will still be appended.
					pool = x509.NewCertPool()
				}
				pool.AppendCertsFromPEM(caCert)
				tlsCfg.RootCAs = pool
			}
			c.HTTPClient = &http.Client{
				Timeout:   30 * time.Second,
				Transport: &http.Transport{TLSClientConfig: tlsCfg},
			}
		}
	})
	return c.HTTPClient
}

func (c *Client) caCertPath() string {
	if c.CACertPath != "" {
		return c.CACertPath
	}
	return DefaultCACertPath
}

func (c *Client) tokenPath() string {
	if c.TokenPath != "" {
		return c.TokenPath
	}
	return DefaultTokenPath
}

func (c *Client) readToken() (string, error) {
	data, err := os.ReadFile(c.tokenPath())
	if err != nil {
		return "", fmt.Errorf("reading SA token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// CreateExperiment creates an MLflow experiment and returns the experiment ID.
// If the experiment already exists, it falls back to GetExperimentByName.
func (c *Client) CreateExperiment(ctx context.Context, name, workspace string) (string, error) {
	body, err := json.Marshal(createExperimentRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	result, err := doJSONRequest[createExperimentResponse](c, ctx, http.MethodPost,
		"/api/2.0/mlflow/experiments/create", workspace, bytes.NewReader(body))
	if err != nil {
		if IsResourceAlreadyExists(err) {
			return c.getOrRestoreExperiment(ctx, name, workspace)
		}
		return "", err
	}
	return result.ExperimentID, nil
}

// GetExperimentByName retrieves an experiment by name and returns the experiment ID.
func (c *Client) GetExperimentByName(ctx context.Context, name, workspace string) (string, error) {
	id, _, err := c.getExperimentByName(ctx, name, workspace)
	return id, err
}

func (c *Client) getExperimentByName(ctx context.Context, name, workspace string) (string, string, error) {
	path := "/api/2.0/mlflow/experiments/get-by-name?experiment_name=" + url.QueryEscape(name)

	result, err := doJSONRequest[getExperimentByNameResponse](c, ctx, http.MethodGet, path, workspace, nil)
	if err != nil {
		return "", "", err
	}
	return result.Experiment.ExperimentID, result.Experiment.LifecycleStage, nil
}

// getOrRestoreExperiment fetches an existing experiment and restores it if deleted.
func (c *Client) getOrRestoreExperiment(ctx context.Context, name, workspace string) (string, error) {
	id, lifecycle, err := c.getExperimentByName(ctx, name, workspace)
	if err != nil {
		return "", err
	}
	if lifecycle == "deleted" {
		if err := c.restoreExperiment(ctx, id, workspace); err != nil {
			return "", fmt.Errorf("restoring deleted experiment %s: %w", id, err)
		}
	}
	return id, nil
}

// restoreExperiment restores a deleted MLflow experiment.
func (c *Client) restoreExperiment(ctx context.Context, experimentID, workspace string) error {
	body, err := json.Marshal(restoreExperimentRequest{ExperimentID: experimentID})
	if err != nil {
		return fmt.Errorf("marshaling restore request: %w", err)
	}
	return c.doRequestExpectOK(ctx, http.MethodPost, "/api/2.0/mlflow/experiments/restore", workspace, bytes.NewReader(body))
}

// doJSONRequest executes an HTTP request and decodes the JSON response.
func doJSONRequest[T any](c *Client, ctx context.Context, method, path, workspace string, body io.Reader) (*T, error) {
	respBody, err := c.doAndReadResponse(ctx, method, path, workspace, body)
	if err != nil {
		return nil, err
	}

	var result T
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}

// doRequestExpectOK executes an HTTP request and returns an error for non 200 responses.
func (c *Client) doRequestExpectOK(ctx context.Context, method, path, workspace string, body io.Reader) error {
	_, err := c.doAndReadResponse(ctx, method, path, workspace, body)
	return err
}

// doAndReadResponse executes an HTTP request, reads the response body, and checks
// for error status codes. Returns the raw response body on success.
func (c *Client) doAndReadResponse(ctx context.Context, method, path, workspace string, body io.Reader) ([]byte, error) {
	resp, err := c.doRequest(ctx, method, path, workspace, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var mlErr mlflowError
		if json.Unmarshal(respBody, &mlErr) == nil && mlErr.ErrorCode != "" {
			return nil, &mlErr
		}
		return nil, fmt.Errorf("mlflow: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *Client) doRequest(ctx context.Context, method, path, workspace string, body io.Reader) (*http.Response, error) {
	reqURL := strings.TrimRight(c.BaseURL, "/") + path

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	token, err := c.readToken()
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(WorkspaceHeader, workspace)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	return resp, nil
}
