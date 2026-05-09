package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	aitaskv1 "github.com/iwen-conf/aitask-cli/internal/rpc/gen/aitask/v1"
	"github.com/iwen-conf/aitask-cli/internal/rpc/gen/aitask/v1/aitaskv1connect"
)

const (
	defaultServerURL = "http://127.0.0.1:8080"
)

type Client struct {
	serverURL string
	http      *http.Client
	token     string

	agentRPC     aitaskv1connect.AgentServiceClient
	bootstrapRPC aitaskv1connect.BootstrapServiceClient
	taskRPC      aitaskv1connect.TaskServiceClient
	contextRPC   aitaskv1connect.ContextServiceClient
}

func NewClient(serverURL string, timeout time.Duration, token string) *Client {
	base := strings.TrimSpace(serverURL)
	if base == "" {
		base = defaultServerURL
	}
	base = strings.TrimRight(base, "/")
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	hc := &http.Client{Timeout: timeout}
	return &Client{
		serverURL:    base,
		http:         hc,
		token:        strings.TrimSpace(token),
		agentRPC:     aitaskv1connect.NewAgentServiceClient(hc, base),
		bootstrapRPC: aitaskv1connect.NewBootstrapServiceClient(hc, base),
		taskRPC:      aitaskv1connect.NewTaskServiceClient(hc, base),
		contextRPC:   aitaskv1connect.NewContextServiceClient(hc, base),
	}
}

func (c *Client) ServerURL() string {
	return c.serverURL
}

func (c *Client) WebSocketURL(projectID string) (string, error) {
	u, err := url.Parse(c.serverURL)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = path.Join(u.Path, "/ws/projects/", strings.TrimSpace(projectID), "agent-room")
	return u.String(), nil
}

func (c *Client) WhoAmI(ctx context.Context) (*aitaskv1.WhoAmIResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.WhoAmIResponse], error) {
		req := connect.NewRequest(&aitaskv1.WhoAmIRequest{})
		c.applyToken(req.Header())
		return c.agentRPC.WhoAmI(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) Bootstrap(ctx context.Context, projectID string) (*aitaskv1.BootstrapResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.BootstrapResponse], error) {
		req := connect.NewRequest(&aitaskv1.BootstrapRequest{ProjectId: strings.TrimSpace(projectID)})
		c.applyToken(req.Header())
		return c.bootstrapRPC.Bootstrap(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) GetCurrentTask(ctx context.Context, projectID string) (*aitaskv1.GetCurrentTaskResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.GetCurrentTaskResponse], error) {
		req := connect.NewRequest(&aitaskv1.GetCurrentTaskRequest{ProjectId: strings.TrimSpace(projectID)})
		c.applyToken(req.Header())
		return c.taskRPC.GetCurrentTask(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) StartTaskRPC(ctx context.Context, projectID string, taskID string, runID string) (*aitaskv1.StartTaskResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.StartTaskResponse], error) {
		req := connect.NewRequest(&aitaskv1.StartTaskRequest{ProjectId: strings.TrimSpace(projectID), TaskId: strings.TrimSpace(taskID), RunId: strings.TrimSpace(runID)})
		c.applyToken(req.Header())
		return c.taskRPC.StartTask(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) SubmitTaskRPC(ctx context.Context, projectID string, taskID string, runID string, markdown string, artifacts []*aitaskv1.ArtifactRef) (*aitaskv1.SubmitTaskResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.SubmitTaskResponse], error) {
		req := connect.NewRequest(&aitaskv1.SubmitTaskRequest{
			ProjectId:      strings.TrimSpace(projectID),
			TaskId:         strings.TrimSpace(taskID),
			RunId:          strings.TrimSpace(runID),
			ResultMarkdown: markdown,
			Artifacts:      artifacts,
		})
		c.applyToken(req.Header())
		return c.taskRPC.SubmitTask(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) ReportContext(ctx context.Context, input *aitaskv1.ReportRequest) (*aitaskv1.ReportResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.ReportResponse], error) {
		req := connect.NewRequest(input)
		c.applyToken(req.Header())
		return c.contextRPC.Report(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) CreateHandoffRPC(ctx context.Context, input *aitaskv1.CreateHandoffRequest) (*aitaskv1.CreateHandoffResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.CreateHandoffResponse], error) {
		req := connect.NewRequest(input)
		c.applyToken(req.Header())
		return c.contextRPC.CreateHandoff(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) GetCurrentHandoffRPC(ctx context.Context, projectID string) (*aitaskv1.GetCurrentHandoffResponse, error) {
	res, err := callRPC(ctx, c, func(ctx context.Context) (*connect.Response[aitaskv1.GetCurrentHandoffResponse], error) {
		req := connect.NewRequest(&aitaskv1.GetCurrentHandoffRequest{ProjectId: strings.TrimSpace(projectID)})
		c.applyToken(req.Header())
		return c.contextRPC.GetCurrentHandoff(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func (c *Client) GetREST(ctx context.Context, requestPath string, query map[string]string) (map[string]any, error) {
	var out map[string]any
	if err := c.requestREST(ctx, http.MethodGet, requestPath, query, nil, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func (c *Client) PostREST(ctx context.Context, requestPath string, body any) (map[string]any, error) {
	var out map[string]any
	if err := c.requestREST(ctx, http.MethodPost, requestPath, nil, body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func (c *Client) PatchREST(ctx context.Context, requestPath string, body any) (map[string]any, error) {
	var out map[string]any
	if err := c.requestREST(ctx, http.MethodPatch, requestPath, nil, body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func (c *Client) PutREST(ctx context.Context, requestPath string, body any) (map[string]any, error) {
	var out map[string]any
	if err := c.requestREST(ctx, http.MethodPut, requestPath, nil, body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func (c *Client) requestREST(ctx context.Context, method string, requestPath string, query map[string]string, body any, out any) error {
	return c.withRetry(ctx, func(ctx context.Context) error {
		reqURL, err := url.Parse(c.serverURL)
		if err != nil {
			return err
		}
		reqURL.Path = path.Join(reqURL.Path, requestPath)
		values := reqURL.Query()
		for k, v := range query {
			value := strings.TrimSpace(v)
			if value == "" {
				continue
			}
			values.Set(k, value)
		}
		reqURL.RawQuery = values.Encode()

		var payload io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				return err
			}
			payload = bytes.NewReader(raw)
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), payload)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		c.applyToken(req.Header)

		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 400 {
			apiErr := &APIError{Code: "HTTP_" + strconv.Itoa(resp.StatusCode), Message: strings.TrimSpace(string(raw)), Retriable: resp.StatusCode >= 500}
			var envelope APIError
			if err := json.Unmarshal(raw, &envelope); err == nil {
				if strings.TrimSpace(envelope.Code) != "" {
					apiErr = &envelope
				}
			}
			if apiErr.Details == nil {
				apiErr.Details = map[string]any{}
			}
			return apiErr
		}

		if out == nil || len(raw) == 0 {
			return nil
		}
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response failed: %w", err)
		}
		return nil
	})
}

func (c *Client) applyToken(header http.Header) {
	if strings.TrimSpace(c.token) == "" {
		return
	}
	header.Set("Authorization", "Bearer "+c.token)
}

func callRPC[T any](ctx context.Context, c *Client, fn func(context.Context) (*connect.Response[T], error)) (*connect.Response[T], error) {
	var response *connect.Response[T]
	err := c.withRetry(ctx, func(ctx context.Context) error {
		res, err := fn(ctx)
		if err != nil {
			return mapConnectError(err)
		}
		response = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (c *Client) withRetry(ctx context.Context, fn func(context.Context) error) error {
	var last error
	for i := 0; i < 3; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i) * 150 * time.Millisecond):
			}
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		last = err
		if !isRetriable(err) {
			return err
		}
	}
	if last == nil {
		return errors.New("request failed")
	}
	return last
}

func isRetriable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retriable
	}
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		switch connectErr.Code() {
		case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeResourceExhausted, connect.CodeInternal:
			return true
		default:
			meta := connectErr.Meta().Get("x-aitask-retriable")
			return strings.EqualFold(strings.TrimSpace(meta), "true")
		}
	}
	return false
}

func mapConnectError(err error) error {
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return err
	}
	apiErr := &APIError{
		Code:      strings.TrimSpace(connectErr.Meta().Get("x-aitask-code")),
		Message:   strings.TrimSpace(connectErr.Message()),
		Retriable: strings.EqualFold(strings.TrimSpace(connectErr.Meta().Get("x-aitask-retriable")), "true"),
		Details:   map[string]any{},
	}
	if apiErr.Code == "" {
		apiErr.Code = strings.TrimSpace(fmt.Sprintf("%s", connectErr.Code()))
	}
	if !apiErr.Retriable {
		switch connectErr.Code() {
		case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeInternal, connect.CodeResourceExhausted:
			apiErr.Retriable = true
		}
	}
	return apiErr
}
