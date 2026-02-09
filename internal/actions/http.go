package actions

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rendis/opcode/pkg/schema"
)

// HTTPConfig configures the HTTP actions.
type HTTPConfig struct {
	MaxResponseBody int64
	DefaultTimeout  time.Duration
}

const (
	defaultMaxResponseBody = 10 * 1024 * 1024 // 10MB
	defaultHTTPTimeout     = 30 * time.Second
)

// Param helpers used by all action files.

func stringParam(m map[string]any, key, defaultVal string) string {
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	s, ok := v.(string)
	if !ok {
		return defaultVal
	}
	return s
}

func boolParam(m map[string]any, key string, defaultVal bool) bool {
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}

func intParam(m map[string]any, key string, defaultVal int) int {
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return defaultVal
		}
		return int(i)
	default:
		return defaultVal
	}
}

func floatParam(m map[string]any, key string, defaultVal float64) float64 {
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return defaultVal
		}
		return f
	default:
		return defaultVal
	}
}

// --- JSON Schemas ---

const httpRequestInputSchema = `{
  "type": "object",
  "properties": {
    "method": {"type": "string", "default": "GET"},
    "url": {"type": "string"},
    "headers": {"type": "object", "additionalProperties": {"type": "string"}},
    "body": {},
    "body_encoding": {"type": "string", "enum": ["json","form","text","raw"], "default": "json"},
    "auth": {
      "type": "object",
      "properties": {
        "type": {"type": "string", "enum": ["bearer","basic","api_key"]},
        "token": {"type": "string"},
        "username": {"type": "string"},
        "password": {"type": "string"},
        "header_name": {"type": "string"},
        "header_value": {"type": "string"}
      }
    },
    "timeout": {"type": "string"},
    "follow_redirects": {"type": "boolean", "default": true},
    "max_redirects": {"type": "integer", "default": 10},
    "tls_skip_verify": {"type": "boolean", "default": false},
    "fail_on_error_status": {"type": "boolean", "default": false}
  },
  "required": ["url"]
}`

const httpRequestOutputSchema = `{
  "type": "object",
  "properties": {
    "status_code": {"type": "integer"},
    "status": {"type": "string"},
    "headers": {"type": "object", "additionalProperties": {"type": "string"}},
    "body": {},
    "content_type": {"type": "string"},
    "duration_ms": {"type": "integer"}
  }
}`

const httpGetInputSchema = `{
  "type": "object",
  "properties": {
    "url": {"type": "string"},
    "headers": {"type": "object", "additionalProperties": {"type": "string"}},
    "auth": {"type": "object"},
    "timeout": {"type": "string"},
    "follow_redirects": {"type": "boolean", "default": true},
    "max_redirects": {"type": "integer", "default": 10},
    "tls_skip_verify": {"type": "boolean", "default": false},
    "fail_on_error_status": {"type": "boolean", "default": false}
  },
  "required": ["url"]
}`

const httpPostInputSchema = `{
  "type": "object",
  "properties": {
    "url": {"type": "string"},
    "headers": {"type": "object", "additionalProperties": {"type": "string"}},
    "body": {},
    "body_encoding": {"type": "string", "enum": ["json","form","text","raw"], "default": "json"},
    "auth": {"type": "object"},
    "timeout": {"type": "string"},
    "follow_redirects": {"type": "boolean", "default": true},
    "max_redirects": {"type": "integer", "default": 10},
    "tls_skip_verify": {"type": "boolean", "default": false},
    "fail_on_error_status": {"type": "boolean", "default": false}
  },
  "required": ["url"]
}`

// --- HTTPRequestAction ---

// HTTPRequestAction implements the "http.request" action.
type HTTPRequestAction struct {
	config HTTPConfig
}

// NewHTTPRequestAction creates a new http.request action.
func NewHTTPRequestAction(cfg HTTPConfig) *HTTPRequestAction {
	if cfg.MaxResponseBody <= 0 {
		cfg.MaxResponseBody = defaultMaxResponseBody
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = defaultHTTPTimeout
	}
	return &HTTPRequestAction{config: cfg}
}

func (a *HTTPRequestAction) Name() string { return "http.request" }

func (a *HTTPRequestAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Execute an HTTP request with full control over method, headers, body, auth, and redirects.",
		InputSchema:  json.RawMessage(httpRequestInputSchema),
		OutputSchema: json.RawMessage(httpRequestOutputSchema),
	}
}

func (a *HTTPRequestAction) Validate(input map[string]any) error {
	rawURL := stringParam(input, "url", "")
	if rawURL == "" {
		return schema.NewError(schema.ErrCodeValidation, "http.request: missing required param 'url'")
	}
	u, err := url.ParseRequestURI(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return schema.NewError(schema.ErrCodeValidation, fmt.Sprintf("http.request: invalid url %q", rawURL))
	}
	return nil
}

func (a *HTTPRequestAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	method := strings.ToUpper(stringParam(params, "method", "GET"))
	rawURL := stringParam(params, "url", "")
	bodyEncoding := stringParam(params, "body_encoding", "json")
	followRedirects := boolParam(params, "follow_redirects", true)
	maxRedirects := intParam(params, "max_redirects", 10)
	tlsSkipVerify := boolParam(params, "tls_skip_verify", false)
	failOnErrorStatus := boolParam(params, "fail_on_error_status", false)

	// Parse timeout
	timeout := a.config.DefaultTimeout
	if ts := stringParam(params, "timeout", ""); ts != "" {
		if d, err := time.ParseDuration(ts); err == nil {
			timeout = d
		}
	}

	// Build request body
	var bodyReader io.Reader
	var contentType string
	if rawBody, ok := params["body"]; ok && rawBody != nil {
		switch bodyEncoding {
		case "form":
			formData, ok := rawBody.(map[string]any)
			if ok {
				vals := url.Values{}
				for k, v := range formData {
					vals.Set(k, fmt.Sprintf("%v", v))
				}
				bodyReader = strings.NewReader(vals.Encode())
				contentType = "application/x-www-form-urlencoded"
			}
		case "text":
			bodyReader = strings.NewReader(fmt.Sprintf("%v", rawBody))
			contentType = "text/plain"
		case "raw":
			bodyReader = strings.NewReader(fmt.Sprintf("%v", rawBody))
		default: // json
			b, err := json.Marshal(rawBody)
			if err != nil {
				return nil, schema.NewErrorf(schema.ErrCodeExecution, "http.request: failed to marshal body as JSON").WithCause(err)
			}
			bodyReader = strings.NewReader(string(b))
			contentType = "application/json"
		}
	}

	// Create request with timeout context
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, bodyReader)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "http.request: failed to create request").WithCause(err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Set custom headers
	if hdrs, ok := params["headers"]; ok {
		if hm, ok := hdrs.(map[string]any); ok {
			for k, v := range hm {
				req.Header.Set(k, fmt.Sprintf("%v", v))
			}
		}
	}

	// Apply auth
	if authRaw, ok := params["auth"]; ok {
		if auth, ok := authRaw.(map[string]any); ok {
			authType := stringParam(auth, "type", "")
			switch authType {
			case "bearer":
				token := stringParam(auth, "token", "")
				req.Header.Set("Authorization", "Bearer "+token)
			case "basic":
				username := stringParam(auth, "username", "")
				password := stringParam(auth, "password", "")
				req.SetBasicAuth(username, password)
			case "api_key":
				headerName := stringParam(auth, "header_name", "")
				headerValue := stringParam(auth, "header_value", "")
				if headerName != "" {
					req.Header.Set(headerName, headerValue)
				}
			}
		}
	}

	// Build client — always create a new client to avoid mutating shared state.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if tlsSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	client := &http.Client{Transport: transport}

	if !followRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else if maxRedirects > 0 {
		limit := maxRedirects
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= limit {
				return fmt.Errorf("stopped after %d redirects", limit)
			}
			return nil
		}
	}

	// Execute
	start := time.Now()
	resp, err := client.Do(req)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "http.request: request failed: %v", err).WithCause(err)
	}
	defer resp.Body.Close()

	// Read response body with size limit
	limitedReader := io.LimitReader(resp.Body, a.config.MaxResponseBody)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "http.request: failed to read response body").WithCause(err)
	}

	// Parse body
	respContentType := resp.Header.Get("Content-Type")
	var parsedBody any
	if len(bodyBytes) == 0 {
		parsedBody = nil
	} else if strings.Contains(respContentType, "application/json") {
		var jsonBody any
		if err := json.Unmarshal(bodyBytes, &jsonBody); err == nil {
			parsedBody = jsonBody
		} else {
			parsedBody = string(bodyBytes)
		}
	} else {
		parsedBody = string(bodyBytes)
	}

	// Response headers as map[string]string
	respHeaders := make(map[string]string, len(resp.Header))
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	result := map[string]any{
		"status_code":  resp.StatusCode,
		"status":       resp.Status,
		"headers":      respHeaders,
		"body":         parsedBody,
		"content_type": respContentType,
		"duration_ms":  durationMs,
	}

	// fail_on_error_status handling
	if failOnErrorStatus && resp.StatusCode >= 400 {
		code := schema.ErrCodeNonRetryable
		if resp.StatusCode >= 500 {
			code = schema.ErrCodeExecution
		}
		return nil, schema.NewErrorf(code, "http.request: server returned %d", resp.StatusCode).
			WithDetails(result)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "http.request: failed to marshal output").WithCause(err)
	}
	return &ActionOutput{Data: json.RawMessage(data)}, nil
}

// --- HTTPGetAction ---

// HTTPGetAction implements the "http.get" convenience action.
type HTTPGetAction struct {
	inner *HTTPRequestAction
}

// NewHTTPGetAction creates a new http.get action.
func NewHTTPGetAction(cfg HTTPConfig) *HTTPGetAction {
	return &HTTPGetAction{inner: NewHTTPRequestAction(cfg)}
}

func (a *HTTPGetAction) Name() string { return "http.get" }

func (a *HTTPGetAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Convenience action for HTTP GET requests.",
		InputSchema:  json.RawMessage(httpGetInputSchema),
		OutputSchema: json.RawMessage(httpRequestOutputSchema),
	}
}

func (a *HTTPGetAction) Validate(input map[string]any) error {
	return a.inner.Validate(input)
}

func (a *HTTPGetAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	if input.Params == nil {
		input.Params = map[string]any{}
	}
	input.Params["method"] = "GET"
	return a.inner.Execute(ctx, input)
}

// --- HTTPPostAction ---

// HTTPPostAction implements the "http.post" convenience action.
type HTTPPostAction struct {
	inner *HTTPRequestAction
}

// NewHTTPPostAction creates a new http.post action.
func NewHTTPPostAction(cfg HTTPConfig) *HTTPPostAction {
	return &HTTPPostAction{inner: NewHTTPRequestAction(cfg)}
}

func (a *HTTPPostAction) Name() string { return "http.post" }

func (a *HTTPPostAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Convenience action for HTTP POST requests.",
		InputSchema:  json.RawMessage(httpPostInputSchema),
		OutputSchema: json.RawMessage(httpRequestOutputSchema),
	}
}

func (a *HTTPPostAction) Validate(input map[string]any) error {
	return a.inner.Validate(input)
}

func (a *HTTPPostAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	if input.Params == nil {
		input.Params = map[string]any{}
	}
	input.Params["method"] = "POST"
	return a.inner.Execute(ctx, input)
}
