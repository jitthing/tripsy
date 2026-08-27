package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	Logger    *slog.Logger
}

func (p *Processor) log() *slog.Logger {
	if p.Logger == nil {
		return slog.Default()
	}
	return p.Logger
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func (p *Processor) RunOnce(ctx context.Context) error {
	if err := p.Store.RequeueStaleImports(ctx, 10*time.Minute, 3); err != nil {
		return err
	}
	_, err := p.ProcessNext(ctx)
	return err
}

// ReclaimStale returns imports abandoned by a worker that died mid-flight. Split
// out of RunOnce so a batch runner reclaims once rather than before every item.
func (p *Processor) ReclaimStale(ctx context.Context) error {
	return p.Store.RequeueStaleImports(ctx, 10*time.Minute, 3)
}

// ProcessNext handles at most one queued import. The bool reports whether an
// import was claimed, so a caller draining the queue knows when it is empty.
func (p *Processor) ProcessNext(ctx context.Context) (bool, error) {
	return p.processClaimed(ctx, func() (store.ReservationImport, error) { return p.Store.ClaimQueuedImport(ctx) })
}

// ProcessNextForOwner is ProcessNext restricted to one owner's queue.
func (p *Processor) ProcessNextForOwner(ctx context.Context, ownerID string) (bool, error) {
	return p.processClaimed(ctx, func() (store.ReservationImport, error) {
		return p.Store.ClaimQueuedImportForOwner(ctx, ownerID)
	})
}

func (p *Processor) processClaimed(ctx context.Context, claim func() (store.ReservationImport, error)) (bool, error) {
	item, err := claim()
	if err == store.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := p.process(ctx, item); err != nil {
		_ = p.Store.FailImport(ctx, item.ID, err)
		return true, err
	}
	return true, nil
}
func (p *Processor) process(ctx context.Context, item store.ReservationImport) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic processing import: %v", r)
		}
	}()
	if p.Resend == nil || p.Storage == nil {
		return fmt.Errorf("reservation import is not configured")
	}
	_ = p.Store.StageImport(ctx, item.ID, "downloading")
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
	_ = p.Store.StageImport(ctx, item.ID, "extracting")
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
	// A fallback is not the same as an empty email. Record why the extractor did not
	// produce drafts so a broken key or a rate limit is visible instead of silent.
	drafts := fallbackDraft(item.Subject, text)
	usedLLM := false
	extractionError := ""
	_ = p.Store.StageImport(ctx, item.ID, "analyzing")
	switch {
	case p.Extractor == nil:
		extractionError = "AI extraction is not configured on the server."
	case strings.TrimSpace(text) == "":
		extractionError = "No readable text was found in the email or its attachments."
	default:
		startedAt := time.Now()
		extracted, extractErr := p.Extractor.Extract(ctx, text)
		switch {
		case extractErr != nil:
			// The elapsed time separates a client timeout from the host killing the
			// process: a clean timeout lands on the configured limit, a kill does not.
			elapsed := time.Since(startedAt)
			extractionError = truncate(extractErr.Error(), 400)
			p.log().Error("reservation extraction failed", "importID", item.ID,
				"elapsedSeconds", elapsed.Round(time.Millisecond).Seconds(), "textBytes", len(text), "error", extractErr)
		case len(extracted) == 0:
			extractionError = "The extractor found no reservations in this email."
			p.log().Warn("reservation extraction returned no drafts", "importID", item.ID)
		default:
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
	if err := p.Store.CompleteImport(ctx, item.ID, rawPath, textPath, usedLLM, extractionError, drafts, attachments); err != nil {
		return err
	}
	// Keep suspected duplicates visible so the user can compare them before
	// deciding whether the message is a change, a duplicate, or a new booking.
	return p.Store.MarkPotentialDuplicate(ctx, item.ID, item.OwnerID, drafts)
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

const extractionSystemPrompt = `You extract travel reservations from forwarded confirmation emails. You return only structured data; a person reviews every draft before it reaches an itinerary.

SPLITTING
Emit one draft per travelled segment, not one per email.
- A return flight booked together is TWO drafts (outbound, inbound).
- A multi-leg journey with a connection is one draft per leg.
- A hotel stay is ONE draft covering check-in to check-out, however many nights.
- Ignore anything that is not a reservation: adverts, loyalty balances, newsletters, receipts for something already past, "manage your booking" prompts. If the email contains no reservation, return an empty drafts array.

TIMES — the most common source of error
- startsAt and endsAt are absolute instants in RFC 3339 with an explicit UTC offset.
- Confirmation emails almost always quote LOCAL time at the place concerned. Convert it using that place's offset on that date, accounting for daylight saving. A 08:30 departure from London on 10 September is "2026-09-10T08:30:00+01:00", NOT "...Z".
- Flights: startsAt is departure from the origin, endsAt is arrival at the destination — each in its OWN local offset. Do not assume a flight lands on its departure date.
- Hotels: startsAt is check-in, endsAt is check-out, both local to the property.
- timeZone is the IANA name for where the reservation STARTS, e.g. "Europe/Lisbon". Leave it "" if you cannot determine it — never guess a plausible-sounding zone.
- If you cannot establish the offset with confidence, still give your best local reading and lower the confidence rather than silently emitting Z.
- Numeric dates are ambiguous: 03/04/2026 is 3 April for a European sender and 4 March for a US one. Use the airline, currency, language, and address to decide. If it stays ambiguous, lower the confidence and say which reading you took in notes.
- A date with no year means the next such date after the email was sent.

FIELDS
- kind: "flight" for air travel; "transport" for train, bus, ferry, and transfers; "stay" for hotels and rentals; "activity" for tours, tickets, restaurants, events; "other" when nothing fits.
- title: what a traveller would recognise on a timeline, e.g. "BA487 London → Barcelona" or "Hotel Bairro Alto, 3 nights". Not the email's subject line.
- supplier: the operating company — the airline, hotel, or rail operator.
- confirmationCode: the booking reference the traveller would quote at the desk (PNR, booking number). Not a ticket number, invoice number, or loyalty ID. Empty string if absent.
- location: where the traveller physically goes at the start — airport and terminal, or the property address.
- notes: only what affects the traveller — baggage allowance, gate-closing time, cancellation deadline, seat, an ambiguity you resolved. Never restate the other fields. Empty string if nothing useful.

CONFIDENCE — this number is shown to the user, so it must mean something
- 0.9–1.0: date, time, offset, and confirmation code all stated explicitly.
- 0.7–0.9: core details explicit, but you inferred the offset or the year.
- 0.4–0.7: you resolved a real ambiguity, such as a numeric date or a missing arrival time.
- 0.1–0.4: the text is fragmentary and much of this is inference.
Report what the evidence supports. Do not default to a high number.`

// OpenRouterExtractor extracts reservation drafts using the OpenRouter API
// (OpenAI-compatible chat completions).
type OpenRouterExtractor struct {
	APIKey string
	Model  string
	Client *http.Client
	// Endpoint overrides the OpenRouter URL; empty means the real service.
	Endpoint string
}

func (e *OpenRouterExtractor) endpoint() string {
	if e.Endpoint == "" {
		return openRouterEndpoint
	}
	return e.Endpoint
}

// NewOpenRouterExtractor builds the extractor. Keep timeout below the host's
// own request or execution limit: a client timeout records why extraction failed,
// whereas the host killing the process strands the import until its lease expires.
func NewOpenRouterExtractor(apiKey, model string, timeout time.Duration) (*OpenRouterExtractor, error) {
	if apiKey == "" || model == "" {
		return nil, fmt.Errorf("OpenRouter extraction is not configured")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenRouterExtractor{
		APIKey: apiKey,
		Model:  model,
		Client: &http.Client{Timeout: timeout},
	}, nil
}

func (e *OpenRouterExtractor) Extract(ctx context.Context, text string) ([]store.ReservationDraft, error) {
	if e.APIKey == "" || e.Model == "" {
		return nil, fmt.Errorf("OpenRouter extraction is not configured")
	}
	payload := openRouterRequest{
		Model: e.Model,
		Messages: []openRouterMessage{
			{Role: "system", Content: extractionSystemPrompt},
			{Role: "user", Content: "Extract every reservation from this forwarded email.\n\n<email>\n" + truncate(text, 24000) + "\n</email>"},
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint(), bytes.NewReader(body))
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
		// Name the model: an abort is usually the provider rejecting the request
		// (often structured outputs), which is model-specific.
		return nil, fmt.Errorf("openrouter error from %s: %s", e.Model, completion.Error.Message)
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
