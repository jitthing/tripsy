package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jittair/waypoint/backend/internal/integrations"
	"github.com/jittair/waypoint/backend/internal/store"
)

type Extractor interface {
	Extract(context.Context, string) ([]store.ReservationDraft, error)
}
type Processor struct {
	Store     *store.Store
	Resend    *integrations.ResendClient
	Storage   *integrations.Storage
	Extractor Extractor
}

func (p *Processor) RunOnce(ctx context.Context) error {
	item, err := p.Store.ClaimQueuedImport(ctx)
	if err == store.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if err := p.process(ctx, item); err != nil {
		_ = p.Store.FailImport(ctx, item.ID, err)
		return err
	}
	return nil
}
func (p *Processor) process(ctx context.Context, item store.ReservationImport) error {
	if p.Resend == nil || p.Storage == nil {
		return fmt.Errorf("reservation import is not configured")
	}
	email, err := p.Resend.GetReceivedEmail(ctx, item.ExternalEmailID)
	if err != nil {
		return err
	}
	prefix := "inbox/" + item.OwnerID
	if item.TripID != nil {
		prefix = *item.TripID
	}
	rawPath := ""
	if email.RawURL != "" {
		raw, downloadErr := p.Resend.Download(ctx, email.RawURL)
		if downloadErr != nil {
			return downloadErr
		}
		rawPath = prefix + "/" + item.ID + "/source.eml"
		if err := p.Storage.Put(ctx, "trip-imports", rawPath, "message/rfc822", raw); err != nil {
			return err
		}
	}
	text := email.Text
	if text == "" {
		text = integrations.StripHTML(email.HTML)
	}
	attachments := make([]store.ImportAttachment, 0, len(email.Attachments))
	for _, attachment := range email.Attachments {
		if attachment.SizeBytes > 10*1024*1024 {
			continue
		}
		bytes, downloadErr := p.Resend.Download(ctx, attachment.DownloadURL)
		if downloadErr != nil {
			return downloadErr
		}
		if len(bytes) > 10*1024*1024 {
			return fmt.Errorf("attachment %s exceeds 10 MB", attachment.Filename)
		}
		path := prefix + "/" + item.ID + "/attachments/" + sanitizeFilename(attachment.Filename)
		if err := p.Storage.Put(ctx, "trip-imports", path, attachment.ContentType, bytes); err != nil {
			return err
		}
		attachments = append(attachments, store.ImportAttachment{Filename: attachment.Filename, ContentType: attachment.ContentType, SizeBytes: int64(len(bytes)), StoragePath: path})
		if attachment.ContentType == "application/pdf" {
			text += "\n" + extractPDFText(bytes)
		}
	}
	textPath := prefix + "/" + item.ID + "/extracted.txt"
	if err := p.Storage.Put(ctx, "trip-imports", textPath, "text/plain; charset=utf-8", []byte(text)); err != nil {
		return err
	}
	drafts := fallbackDraft(item.Subject, text)
	usedLLM := false
	if p.Extractor != nil {
		if extracted, extractErr := p.Extractor.Extract(ctx, text); extractErr == nil && len(extracted) > 0 {
			drafts = extracted
			usedLLM = true
		}
	}
	for index := range drafts {
		if drafts[index].TimeZone == "" {
			drafts[index].TimeZone = "UTC"
		}
		if drafts[index].Title == "" {
			drafts[index].Title = item.Subject
		}
		if err := store.ValidateKind(drafts[index].Kind); err != nil {
			drafts[index].Kind = "other"
		}
	}
	return p.Store.CompleteImport(ctx, item.ID, rawPath, textPath, usedLLM, drafts, attachments)
}
func fallbackDraft(subject, text string) []store.ReservationDraft {
	kind := "other"
	lower := strings.ToLower(subject + " " + text)
	switch {
	case strings.Contains(lower, "hotel") || strings.Contains(lower, "stay"):
		kind = "stay"
	case strings.Contains(lower, "flight") || strings.Contains(lower, "boarding"):
		kind = "flight"
	case strings.Contains(lower, "train") || strings.Contains(lower, "rail"):
		kind = "transport"
	case strings.Contains(lower, "ticket") || strings.Contains(lower, "concert") || strings.Contains(lower, "museum"):
		kind = "activity"
	}
	return []store.ReservationDraft{{Kind: kind, Title: subject, Notes: "Review the forwarded reservation before adding it to the itinerary.", Confidence: .2, TimeZone: "UTC"}}
}
func extractPDFText(data []byte) string {
	path, err := exec.LookPath("pdftotext")
	if err != nil {
		return ""
	}
	temporary, err := os.CreateTemp("", "waypoint-import-*.pdf")
	if err != nil {
		return ""
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(data); err != nil {
		return ""
	}
	temporary.Close()
	result, err := exec.Command(path, "-layout", temporary.Name(), "-").Output()
	if err != nil {
		return ""
	}
	return string(result)
}
func sanitizeFilename(value string) string {
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
			return r
		}
		return '-'
	}, value)
	if value == "" {
		return "attachment"
	}
	return value
}

const openRouterEndpoint = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouterExtractor extracts reservation drafts using the OpenRouter API
// (OpenAI-compatible chat completions).
type OpenRouterExtractor struct {
	APIKey string
	Model  string
	Client *http.Client
}

func NewOpenRouterExtractor(apiKey, model string) (*OpenRouterExtractor, error) {
	if apiKey == "" || model == "" {
		return nil, fmt.Errorf("OpenRouter extraction is not configured")
	}
	return &OpenRouterExtractor{
		APIKey: apiKey,
		Model:  model,
		Client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (e *OpenRouterExtractor) Extract(ctx context.Context, text string) ([]store.ReservationDraft, error) {
	if e.APIKey == "" || e.Model == "" {
		return nil, fmt.Errorf("OpenRouter extraction is not configured")
	}
	payload := openRouterRequest{
		Model: e.Model,
		Messages: []openRouterMessage{
			{Role: "user", Content: "Extract reservation details from the following text. Return every reservation found. Use RFC 3339 timestamps when known; otherwise use null. Use an empty string when a text field is unknown.\n\n" + text},
		},
		ResponseFormat: &openRouterFormat{
			Type: "json_schema",
			JSONSchema: &openRouterJSONSchema{
				Name:   "reservation_drafts",
				Schema: reservationDraftsSchema,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter request failed with status %d: %s", resp.StatusCode, string(raw))
	}
	var completion openRouterResponse
	if err := json.Unmarshal(raw, &completion); err != nil {
		return nil, err
	}
	if completion.Error != nil {
		return nil, fmt.Errorf("openrouter error: %s", completion.Error.Message)
	}
	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("openrouter returned no choices")
	}
	var result reservationDraftsResult
	if err := json.Unmarshal([]byte(completion.Choices[0].Message.Content), &result); err != nil {
		return nil, err
	}
	drafts := make([]store.ReservationDraft, 0, len(result.Drafts))
	for _, draft := range result.Drafts {
		drafts = append(drafts, store.ReservationDraft{Kind: draft.Kind, Title: draft.Title, Supplier: draft.Supplier, ConfirmationCode: draft.ConfirmationCode, StartsAt: draft.StartsAt, EndsAt: draft.EndsAt, TimeZone: draft.TimeZone, Location: draft.Location, Notes: draft.Notes, Confidence: draft.Confidence})
	}
	return drafts, nil
}

type openRouterRequest struct {
	Model          string              `json:"model"`
	Messages       []openRouterMessage `json:"messages"`
	ResponseFormat *openRouterFormat   `json:"response_format,omitempty"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterFormat struct {
	Type       string               `json:"type"`
	JSONSchema *openRouterJSONSchema `json:"json_schema,omitempty"`
}

type openRouterJSONSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type reservationDraftsResult struct {
	Drafts []struct {
		Kind, Title, Supplier, ConfirmationCode, TimeZone, Location, Notes string
		StartsAt, EndsAt                                                   *time.Time
		Confidence                                                         float64
	} `json:"drafts"`
}

var reservationDraftsSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"drafts": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":             map[string]any{"type": "string", "enum": []string{"stay", "flight", "transport", "activity", "other"}},
					"title":            map[string]any{"type": "string"},
					"supplier":         map[string]any{"type": "string"},
					"confirmationCode": map[string]any{"type": "string"},
					"startsAt":         map[string]any{"anyOf": []any{map[string]any{"type": "string", "format": "date-time"}, map[string]any{"type": "null"}}},
					"endsAt":           map[string]any{"anyOf": []any{map[string]any{"type": "string", "format": "date-time"}, map[string]any{"type": "null"}}},
					"timeZone":         map[string]any{"type": "string"},
					"location":         map[string]any{"type": "string"},
					"notes":            map[string]any{"type": "string"},
					"confidence":       map[string]any{"type": "number", "minimum": 0, "maximum": 1},
				},
				"required": []string{"kind", "title", "supplier", "confirmationCode", "startsAt", "endsAt", "timeZone", "location", "notes", "confidence"},
			},
		},
	},
	"required": []string{"drafts"},
}
