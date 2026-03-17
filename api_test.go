package nodeloom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApiClient_Request(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer sdk_testkey" {
			t.Errorf("expected Bearer sdk_testkey, got %s", auth)
		}

		if r.URL.Path == "/api/workflows" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]string{{"id": "wf-1"}})
			return
		}

		if r.URL.Path == "/api/workflows/wf-1/execute" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"executionId": "ex-1"})
			return
		}

		if r.URL.Path == "/api/forbidden" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"Access denied"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newApiClient("sdk_testkey", server.URL)

	t.Run("successful GET request", func(t *testing.T) {
		body, err := client.Request("GET", "/api/workflows?teamId=t1", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("expected non-empty response")
		}
	})

	t.Run("successful POST request", func(t *testing.T) {
		body, err := client.Request("POST", "/api/workflows/wf-1/execute", map[string]any{"input": "test"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var result map[string]string
		json.Unmarshal(body, &result)
		if result["executionId"] != "ex-1" {
			t.Errorf("expected executionId=ex-1, got %s", result["executionId"])
		}
	})

	t.Run("error response returns ApiError", func(t *testing.T) {
		_, err := client.Request("GET", "/api/forbidden", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		apiErr, ok := err.(*ApiError)
		if !ok {
			t.Fatalf("expected *ApiError, got %T", err)
		}
		if apiErr.StatusCode != 403 {
			t.Errorf("expected status 403, got %d", apiErr.StatusCode)
		}
	})

	t.Run("404 returns ApiError", func(t *testing.T) {
		_, err := client.Request("GET", "/api/unknown", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		apiErr, ok := err.(*ApiError)
		if !ok {
			t.Fatalf("expected *ApiError, got %T", err)
		}
		if apiErr.StatusCode != 404 {
			t.Errorf("expected status 404, got %d", apiErr.StatusCode)
		}
	})
}

func TestApiClient_RequestJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "wf-1", "name": "test"})
	}))
	defer server.Close()

	client := newApiClient("sdk_test", server.URL)

	var result map[string]string
	err := client.RequestJSON("GET", "/api/workflows/wf-1", nil, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["id"] != "wf-1" {
		t.Errorf("expected id=wf-1, got %s", result["id"])
	}
}

func TestApiClient_ConvenienceMethods(t *testing.T) {
	var lastPath string
	var lastMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.RequestURI()
		lastMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := newApiClient("sdk_test", server.URL)

	t.Run("ListWorkflows", func(t *testing.T) {
		client.ListWorkflows("team-1")
		if lastPath != "/api/workflows?teamId=team-1" {
			t.Errorf("unexpected path: %s", lastPath)
		}
		if lastMethod != "GET" {
			t.Errorf("expected GET, got %s", lastMethod)
		}
	})

	t.Run("GetWorkflow", func(t *testing.T) {
		client.GetWorkflow("wf-1")
		if lastPath != "/api/workflows/wf-1" {
			t.Errorf("unexpected path: %s", lastPath)
		}
	})

	t.Run("ExecuteWorkflow", func(t *testing.T) {
		client.ExecuteWorkflow("wf-1", map[string]any{"q": "test"})
		if lastPath != "/api/workflows/wf-1/execute" {
			t.Errorf("unexpected path: %s", lastPath)
		}
		if lastMethod != "POST" {
			t.Errorf("expected POST, got %s", lastMethod)
		}
	})

	t.Run("GetExecution", func(t *testing.T) {
		client.GetExecution("ex-1")
		if lastPath != "/api/executions/ex-1" {
			t.Errorf("unexpected path: %s", lastPath)
		}
	})

	t.Run("ListCredentials", func(t *testing.T) {
		client.ListCredentials("team-1")
		if lastPath != "/api/credentials?teamId=team-1" {
			t.Errorf("unexpected path: %s", lastPath)
		}
	})
}

func TestClient_Api(t *testing.T) {
	client := New("sdk_test")
	defer client.Close()

	api := client.Api()
	if api == nil {
		t.Fatal("expected non-nil ApiClient")
	}

	// Should be cached
	api2 := client.Api()
	if api != api2 {
		t.Error("expected cached ApiClient instance")
	}
}
