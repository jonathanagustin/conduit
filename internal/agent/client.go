package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

var (
	ErrNoPlan        = errors.New("no approved plan available")
	ErrPlanCancelled = errors.New("plan was cancelled by the control plane")
)

type ControlPlane interface {
	Lease(context.Context, string, string, time.Duration) (controlplane.PlanRecord, error)
	Start(context.Context, string, string, string, string) error
	Renew(context.Context, string, string, string, string, time.Duration) error
	Finish(context.Context, string, string, string, string, bool, string, string, controlplane.PlanExecution, controlplane.EnvironmentReport) error
}

type HTTPControlPlane struct {
	BaseURL *url.URL
	Client  *http.Client
}

func (client *HTTPControlPlane) HealthTargets(
	ctx context.Context, environment, agentID string,
) ([]controlplane.HealthTarget, error) {
	var response struct {
		Targets []controlplane.HealthTarget `json:"targets"`
	}
	_, err := client.request(ctx, http.MethodGet,
		"/v1/environments/"+url.PathEscape(environment)+"/health-targets", nil,
		agentHeaders(environment, agentID), &response)
	return response.Targets, err
}

func (client *HTTPControlPlane) ReportHealth(
	ctx context.Context, environment, agentID string, observations []controlplane.HealthObservation,
) error {
	_, err := client.request(ctx, http.MethodPost,
		"/v1/environments/"+url.PathEscape(environment)+"/health-observations",
		map[string]any{"observations": observations}, agentHeaders(environment, agentID), nil)
	return err
}

func NewHTTPControlPlane(rawURL string, client *http.Client) (*HTTPControlPlane, error) {
	baseURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("control plane URL must be an absolute http(s) URL")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("control plane URL must use http or https")
	}
	// Every request path below is absolute, so ResolveReference discards a base
	// path rather than prefixing it: CONTROL_PLANE_URL=https://hub.example/platform
	// silently becomes https://hub.example/v1/.... A Spoke that misroutes leases
	// and reports fails in ways that look like the Hub is unreachable, so refuse
	// the URL instead.
	if strings.Trim(baseURL.Path, "/") != "" || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("control plane URL must be a scheme and host without a path, query, or fragment")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPControlPlane{BaseURL: baseURL, Client: client}, nil
}

func (client *HTTPControlPlane) Lease(
	ctx context.Context, environment, agentID string, duration time.Duration,
) (controlplane.PlanRecord, error) {
	var record controlplane.PlanRecord
	status, err := client.request(ctx, http.MethodPost,
		"/v1/environments/"+url.PathEscape(environment)+"/maintenance-admission/leases",
		map[string]int{"durationSeconds": int(duration.Seconds())},
		agentHeaders(environment, agentID), &record)
	if err != nil {
		return record, err
	}
	if status == http.StatusNoContent {
		return record, ErrNoPlan
	}
	return record, nil
}

func (client *HTTPControlPlane) Start(ctx context.Context, environment, agentID, planID, token string) error {
	_, err := client.request(ctx, http.MethodPost, "/v1/plans/"+url.PathEscape(planID)+"/maintenance-admission/start", nil,
		leaseHeaders(environment, agentID, token), nil)
	return err
}

func (client *HTTPControlPlane) Renew(
	ctx context.Context, environment, agentID, planID, token string, duration time.Duration,
) error {
	status, err := client.request(ctx, http.MethodPost, "/v1/plans/"+url.PathEscape(planID)+"/heartbeat",
		map[string]int{"durationSeconds": int(duration.Seconds())},
		leaseHeaders(environment, agentID, token), nil)
	if status == http.StatusGone {
		return ErrPlanCancelled
	}
	return err
}

func (client *HTTPControlPlane) Finish(
	ctx context.Context, environment, agentID, planID, token string,
	succeeded bool, failure, failedInstallationID string, execution controlplane.PlanExecution, report controlplane.EnvironmentReport,
) error {
	status := controlplane.PlanFailed
	if succeeded {
		status = controlplane.PlanSucceeded
	}
	body := struct {
		Status               controlplane.PlanStatus       `json:"status"`
		Failure              string                        `json:"failure,omitempty"`
		FailedInstallationID string                        `json:"failedInstallationId,omitempty"`
		Execution            controlplane.PlanExecution    `json:"execution"`
		Entities             []controlplane.ReportedEntity `json:"entities,omitempty"`
		Secrets              []controlplane.ReportedSecret `json:"secrets,omitempty"`
	}{Status: status, Failure: failure, FailedInstallationID: failedInstallationID, Execution: execution, Entities: report.Entities, Secrets: report.Secrets}
	responseStatus, err := client.request(ctx, http.MethodPost, "/v1/plans/"+url.PathEscape(planID)+"/finish", body,
		leaseHeaders(environment, agentID, token), nil)
	if responseStatus == http.StatusGone {
		return ErrPlanCancelled
	}
	return err
}

func agentHeaders(environment, agentID string) map[string]string {
	return map[string]string{
		"X-Platform-Environment-ID": environment,
		"X-Platform-Agent-ID":       agentID,
	}
}

func leaseHeaders(environment, agentID, token string) map[string]string {
	headers := agentHeaders(environment, agentID)
	headers["X-Platform-Lease-Token"] = token
	return headers
}

func (client *HTTPControlPlane) request(
	ctx context.Context, method, path string, body any, headers map[string]string, output any,
) (int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	target := client.BaseURL.ResolveReference(&url.URL{Path: path})
	request, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("control plane request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, fmt.Errorf("read control plane response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(contents, &problem) == nil && problem.Error != "" {
			return response.StatusCode, fmt.Errorf("control plane returned %d: %s", response.StatusCode, problem.Error)
		}
		return response.StatusCode, fmt.Errorf("control plane returned %d", response.StatusCode)
	}
	if output != nil {
		if err := json.Unmarshal(contents, output); err != nil {
			return response.StatusCode, fmt.Errorf("decode control plane response: %w", err)
		}
	}
	return response.StatusCode, nil
}
