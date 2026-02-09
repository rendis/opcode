package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func httpAction() *HTTPRequestAction {
	return NewHTTPRequestAction(HTTPConfig{})
}

func execHTTP(t *testing.T, action Action, params map[string]any) (map[string]any, error) {
	t.Helper()
	out, err := action.Execute(context.Background(), ActionInput{Params: params})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Data, &result))
	return result, nil
}

func TestHTTPRequest_GET_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom", "test-value")
		json.NewEncoder(w).Encode(map[string]any{"greeting": "hello", "count": 42})
	}))
	defer srv.Close()

	result, err := execHTTP(t, httpAction(), map[string]any{"url": srv.URL})
	require.NoError(t, err)

	assert.Equal(t, float64(200), result["status_code"])
	assert.Contains(t, result["content_type"], "application/json")
	assert.GreaterOrEqual(t, result["duration_ms"], float64(0))

	body, ok := result["body"].(map[string]any)
	require.True(t, ok, "body should be parsed map")
	assert.Equal(t, "hello", body["greeting"])

	hdrs, ok := result["headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test-value", hdrs["X-Custom"])
}

func TestHTTPRequest_POST_JSONBody(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.Header.Get("Content-Type"), "application/json")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"body":   map[string]any{"name": "test", "value": 123},
	})
	require.NoError(t, err)
	assert.Equal(t, "test", received["name"])
	assert.Equal(t, float64(123), received["value"])
}

func TestHTTPRequest_POST_FormBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
		r.ParseForm()
		assert.Equal(t, "bar", r.FormValue("foo"))
		assert.Equal(t, "42", r.FormValue("num"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url":           srv.URL,
		"method":        "POST",
		"body_encoding": "form",
		"body":          map[string]any{"foo": "bar", "num": 42},
	})
	require.NoError(t, err)
}

func TestHTTPRequest_POST_TextBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.Header.Get("Content-Type"), "text/plain")
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, "hello world", string(body))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url":           srv.URL,
		"method":        "POST",
		"body_encoding": "text",
		"body":          "hello world",
	})
	require.NoError(t, err)
}

func TestHTTPRequest_AllMethods(t *testing.T) {
	methods := []string{"PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, method, r.Method)
				w.WriteHeader(200)
			}))
			defer srv.Close()

			result, err := execHTTP(t, httpAction(), map[string]any{
				"url":    srv.URL,
				"method": method,
			})
			require.NoError(t, err)
			assert.Equal(t, float64(200), result["status_code"])
		})
	}
}

func TestHTTPRequest_CustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "my-agent", r.Header.Get("X-Agent"))
		assert.Equal(t, "custom-val", r.Header.Get("X-Custom"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url": srv.URL,
		"headers": map[string]any{
			"X-Agent":  "my-agent",
			"X-Custom": "custom-val",
		},
	})
	require.NoError(t, err)
}

func TestHTTPRequest_Auth_Bearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer my-secret-token", r.Header.Get("Authorization"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url": srv.URL,
		"auth": map[string]any{
			"type":  "bearer",
			"token": "my-secret-token",
		},
	})
	require.NoError(t, err)
}

func TestHTTPRequest_Auth_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "admin", user)
		assert.Equal(t, "s3cret", pass)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url": srv.URL,
		"auth": map[string]any{
			"type":     "basic",
			"username": "admin",
			"password": "s3cret",
		},
	})
	require.NoError(t, err)
}

func TestHTTPRequest_Auth_APIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "key-12345", r.Header.Get("X-API-Key"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url": srv.URL,
		"auth": map[string]any{
			"type":         "api_key",
			"header_name":  "X-API-Key",
			"header_value": "key-12345",
		},
	})
	require.NoError(t, err)
}

func TestHTTPRequest_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until client gives up
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url":     srv.URL,
		"timeout": "100ms",
	})
	require.Error(t, err)

	var opcodeErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcodeErr))
	assert.Equal(t, schema.ErrCodeExecution, opcodeErr.Code)
}

func TestHTTPRequest_NoRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/other")
		w.WriteHeader(302)
	}))
	defer srv.Close()

	result, err := execHTTP(t, httpAction(), map[string]any{
		"url":              srv.URL,
		"follow_redirects": false,
	})
	require.NoError(t, err)
	assert.Equal(t, float64(302), result["status_code"])
}

func TestHTTPRequest_MaxRedirects(t *testing.T) {
	redirectCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		w.Header().Set("Location", fmt.Sprintf("/redirect-%d", redirectCount))
		w.WriteHeader(302)
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url":           srv.URL,
		"max_redirects": 3,
	})
	require.Error(t, err)

	var opcodeErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcodeErr))
	assert.Equal(t, schema.ErrCodeExecution, opcodeErr.Code)
}

func TestHTTPRequest_ResponseSizeLimit(t *testing.T) {
	bigBody := strings.Repeat("X", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	action := NewHTTPRequestAction(HTTPConfig{MaxResponseBody: 100})
	result, err := execHTTP(t, action, map[string]any{"url": srv.URL})
	require.NoError(t, err)

	body, ok := result["body"].(string)
	require.True(t, ok)
	assert.Len(t, body, 100)
}

func TestHTTPRequest_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<h1>Hello</h1>"))
	}))
	defer srv.Close()

	result, err := execHTTP(t, httpAction(), map[string]any{"url": srv.URL})
	require.NoError(t, err)

	body, ok := result["body"].(string)
	require.True(t, ok)
	assert.Equal(t, "<h1>Hello</h1>", body)
	assert.Contains(t, result["content_type"], "text/html")
}

func TestHTTPRequest_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	result, err := execHTTP(t, httpAction(), map[string]any{"url": srv.URL})
	require.NoError(t, err)

	assert.Equal(t, float64(204), result["status_code"])
	assert.Nil(t, result["body"])
}

func TestHTTPRequest_Validate_MissingURL(t *testing.T) {
	action := httpAction()
	err := action.Validate(map[string]any{})
	require.Error(t, err)

	var opcodeErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcodeErr))
	assert.Equal(t, schema.ErrCodeValidation, opcodeErr.Code)
	assert.Contains(t, opcodeErr.Message, "url")
}

func TestHTTPRequest_Validate_InvalidURL(t *testing.T) {
	action := httpAction()
	err := action.Validate(map[string]any{"url": "not-a-url"})
	require.Error(t, err)

	var opcodeErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcodeErr))
	assert.Equal(t, schema.ErrCodeValidation, opcodeErr.Code)
}

func TestHTTPRequest_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until client disconnects
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := httpAction().Execute(ctx, ActionInput{Params: map[string]any{
		"url":     srv.URL,
		"timeout": "10s",
	}})
	require.Error(t, err)

	var opcodeErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcodeErr))
	assert.Equal(t, schema.ErrCodeExecution, opcodeErr.Code)
}

func TestHTTPGet_Convenience(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	action := NewHTTPGetAction(HTTPConfig{})
	result, err := execHTTP(t, action, map[string]any{
		"url":    srv.URL,
		"method": "POST", // should be overridden to GET
	})
	require.NoError(t, err)
	assert.Equal(t, float64(200), result["status_code"])
}

func TestHTTPPost_Convenience(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	action := NewHTTPPostAction(HTTPConfig{})
	result, err := execHTTP(t, action, map[string]any{
		"url":    srv.URL,
		"method": "GET", // should be overridden to POST
	})
	require.NoError(t, err)
	assert.Equal(t, float64(200), result["status_code"])
}

func TestHTTPRequest_FailOnErrorStatus_4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url":                  srv.URL,
		"fail_on_error_status": true,
	})
	require.Error(t, err)

	var opcodeErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcodeErr))
	assert.Equal(t, schema.ErrCodeNonRetryable, opcodeErr.Code)
	assert.False(t, opcodeErr.IsRetryable())
	assert.Contains(t, opcodeErr.Message, "404")
}

func TestHTTPRequest_FailOnErrorStatus_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	_, err := execHTTP(t, httpAction(), map[string]any{
		"url":                  srv.URL,
		"fail_on_error_status": true,
	})
	require.Error(t, err)

	var opcodeErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcodeErr))
	assert.Equal(t, schema.ErrCodeExecution, opcodeErr.Code)
	assert.True(t, opcodeErr.IsRetryable())
	assert.Contains(t, opcodeErr.Message, "500")
}

func TestHTTPRequest_NoFailOnErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer srv.Close()

	result, err := execHTTP(t, httpAction(), map[string]any{
		"url": srv.URL,
		// fail_on_error_status defaults to false
	})
	require.NoError(t, err)
	assert.Equal(t, float64(500), result["status_code"])

	body, ok := result["body"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "server error", body["error"])
}
