package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func workerRequest(t *testing.T, secret, header, query string) *httptest.ResponseRecorder {
	t.Helper()
	// No store, importer, or calendar: the pass must still authenticate and report
	// an empty run rather than panicking on absent dependencies.
	a := &API{logger: testLogger(), cronSecret: secret, cronBudget: time.Second}
	target := "/internal/cron/worker"
	if query != "" {
		target += "?token=" + query
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if header != "" {
		request.Header.Set("Authorization", "Bearer "+header)
	}
	recorder := httptest.NewRecorder()
	a.runWorkerPass(recorder, request)
	return recorder
}

func TestWorkerPassRejectsAMissingToken(t *testing.T) {
	if code := workerRequest(t, "s3cret", "", "").Code; code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a token, got %d", code)
	}
}

func TestWorkerPassRejectsAWrongToken(t *testing.T) {
	if code := workerRequest(t, "s3cret", "wrong", "").Code; code != http.StatusUnauthorized {
		t.Errorf("expected 401 for a wrong token, got %d", code)
	}
}

func TestWorkerPassRefusesToRunWithNoSecretConfigured(t *testing.T) {
	// An empty secret must never mean "everyone is authorised".
	if code := workerRequest(t, "", "", "").Code; code != http.StatusUnauthorized {
		t.Errorf("an unconfigured secret must not authorise anyone, got %d", code)
	}
	if code := workerRequest(t, "", "anything", "").Code; code != http.StatusUnauthorized {
		t.Errorf("an unconfigured secret must not authorise anyone, got %d", code)
	}
}

func TestWorkerPassAcceptsTheBearerToken(t *testing.T) {
	if code := workerRequest(t, "s3cret", "s3cret", "").Code; code != http.StatusOK {
		t.Errorf("expected 200 for the configured token, got %d", code)
	}
}

func TestWorkerPassAcceptsAQueryToken(t *testing.T) {
	// Schedulers that cannot set headers fall back to ?token=.
	if code := workerRequest(t, "s3cret", "", "s3cret").Code; code != http.StatusOK {
		t.Errorf("expected 200 for a query token, got %d", code)
	}
}

func TestWorkerPassIsNotRoutedWithoutASecret(t *testing.T) {
	recorder := httptest.NewRecorder()
	New(nil, nil, testLogger(), Config{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/internal/cron/worker", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("the endpoint must not exist when no secret is configured, got %d", recorder.Code)
	}
}

func TestWorkerPassIsRoutedWhenConfigured(t *testing.T) {
	recorder := httptest.NewRecorder()
	New(nil, nil, testLogger(), Config{CronSecret: "s3cret"}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/internal/cron/worker", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("expected the route to exist and reject the caller, got %d", recorder.Code)
	}
}
