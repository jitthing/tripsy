package importer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestExtractor(t *testing.T, handler http.HandlerFunc) (*OpenRouterExtractor, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &OpenRouterExtractor{APIKey: "test-key", Model: "test/model", Client: server.Client(), Endpoint: server.URL}, server
}

func completion(content string) string {
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
	return string(body)
}

func TestExtractParsesDrafts(t *testing.T) {
	var sent openRouterRequest
	extractor, _ := newTestExtractor(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.Write([]byte(completion(`{"drafts":[{"kind":"flight","title":"BA487 London → Barcelona","supplier":"British Airways","confirmationCode":"XK92QP","startsAt":"2026-09-10T08:30:00+01:00","endsAt":"2026-09-10T11:45:00+02:00","timeZone":"Europe/London","location":"Gatwick North","notes":"","confidence":0.92}]}`)))
	})

	drafts, err := extractor.Extract(context.Background(), "BA487 departs 08:30")
	if err != nil {
		t.Fatalf("Extract returned an error: %v", err)
	}
	if len(drafts) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(drafts))
	}
	if drafts[0].ConfirmationCode != "XK92QP" || drafts[0].Kind != "flight" {
		t.Errorf("draft fields not mapped: %+v", drafts[0])
	}
	// The offset must survive: a local 08:30 departure is 07:30Z, not 08:30Z.
	if got := drafts[0].StartsAt.UTC(); !got.Equal(time.Date(2026, 9, 10, 7, 30, 0, 0, time.UTC)) {
		t.Errorf("startsAt lost its offset: got %s", got)
	}
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" {
		t.Fatalf("expected a system prompt followed by the email, got %+v", sent.Messages)
	}
	if !strings.Contains(sent.Messages[1].Content, "BA487 departs 08:30") {
		t.Error("email text was not sent to the model")
	}
}

func TestExtractSurfacesAPIErrors(t *testing.T) {
	extractor, _ := newTestExtractor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	})

	if _, err := extractor.Extract(context.Background(), "anything"); err == nil {
		t.Fatal("expected an error for a 401 response")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should name the status code, got %v", err)
	}
}

func TestExtractRejectsUnparseableContent(t *testing.T) {
	extractor, _ := newTestExtractor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completion("I'm sorry, I can't help with that.")))
	})

	if _, err := extractor.Extract(context.Background(), "anything"); err == nil {
		t.Fatal("expected an error when the model returns prose instead of JSON")
	}
}

func TestExtractReturnsNoDraftsForAnEmailWithNoReservation(t *testing.T) {
	extractor, _ := newTestExtractor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(completion(`{"drafts":[]}`)))
	})

	drafts, err := extractor.Extract(context.Background(), "50% off flights this weekend")
	if err != nil {
		t.Fatalf("an advert is not an error: %v", err)
	}
	if len(drafts) != 0 {
		t.Errorf("expected no drafts, got %d", len(drafts))
	}
}

func TestFallbackDraftClassifiesByKeyword(t *testing.T) {
	for _, tc := range []struct{ name, subject, body, want string }{
		{"hotel", "Your Hotel Bairro Alto booking", "", "stay"},
		{"flight", "Your flight to Lisbon", "", "flight"},
		{"train", "Your train ticket", "", "transport"},
		{"museum", "Your museum entry", "", "activity"},
		{"unknown", "Thanks for your order", "", "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fallbackDraft(tc.subject, tc.body)[0].Kind; got != tc.want {
				t.Errorf("fallbackDraft(%q) = %q, want %q", tc.subject, got, tc.want)
			}
		})
	}
}

func TestFallbackDraftIsLowConfidence(t *testing.T) {
	draft := fallbackDraft("Your flight to Lisbon", "")[0]
	if draft.Confidence > 0.3 {
		t.Errorf("a keyword guess must not claim high confidence, got %v", draft.Confidence)
	}
}

func TestTruncateKeepsShortValuesIntact(t *testing.T) {
	if got := truncate("  short  ", 100); got != "short" {
		t.Errorf("truncate should trim and pass through, got %q", got)
	}
	if got := truncate(strings.Repeat("a", 50), 10); len([]rune(got)) != 11 {
		t.Errorf("truncate should cap length and mark the cut, got %q", got)
	}
}
