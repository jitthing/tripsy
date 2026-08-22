package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func routedPath(t *testing.T, prefix, request string) (int, string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	})
	mux.HandleFunc("/v1/trips", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	})
	recorder := httptest.NewRecorder()
	stripBasePath(prefix, mux).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, request, nil))
	return recorder.Code, recorder.Body.String()
}

func TestStripBasePathRoutesPrefixedRequests(t *testing.T) {
	// The bug this fixes: the proxy forwards /api/v1/trips unchanged and chi 404s.
	code, path := routedPath(t, "/api", "/api/v1/trips")
	if code != http.StatusOK {
		t.Fatalf("expected the prefixed path to route, got %d", code)
	}
	if path != "/v1/trips" {
		t.Errorf("handler should see the unprefixed path, got %q", path)
	}
}

func TestStripBasePathStillServesUnprefixedRequests(t *testing.T) {
	// Render's health check hits /health directly, with no proxy in front.
	code, path := routedPath(t, "/api", "/health")
	if code != http.StatusOK {
		t.Fatalf("a bare path must keep working, got %d", code)
	}
	if path != "/health" {
		t.Errorf("expected /health, got %q", path)
	}
}

func TestStripBasePathIgnoresPartialPrefixMatches(t *testing.T) {
	// "/apiary" starts with "/api" as a string but is not under the mount point.
	if code, _ := routedPath(t, "/api", "/apiary/v1/trips"); code != http.StatusNotFound {
		t.Errorf("a partial prefix match must not be stripped, got %d", code)
	}
}

func TestStripBasePathIsAPassThroughWhenUnset(t *testing.T) {
	code, path := routedPath(t, "", "/v1/trips")
	if code != http.StatusOK || path != "/v1/trips" {
		t.Errorf("unset prefix must not alter routing, got %d %q", code, path)
	}
	if code, _ := routedPath(t, "", "/api/v1/trips"); code != http.StatusNotFound {
		t.Errorf("unset prefix must not strip anything, got %d", code)
	}
}

func TestStripBasePathToleratesTrailingSlashInConfig(t *testing.T) {
	if code, path := routedPath(t, "/api/", "/api/v1/trips"); code != http.StatusOK || path != "/v1/trips" {
		t.Errorf(`BasePath "/api/" should behave like "/api", got %d %q`, code, path)
	}
}

func TestStripBasePathMapsTheMountRootToSlash(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.URL.Path)) })
	recorder := httptest.NewRecorder()
	stripBasePath("/api", mux).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api", nil))
	if recorder.Body.String() != "/" {
		t.Errorf("the mount root should become /, got %q", recorder.Body.String())
	}
}
