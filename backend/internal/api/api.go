package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jittair/waypoint/backend/internal/auth"
	"github.com/jittair/waypoint/backend/internal/calendar"
	"github.com/jittair/waypoint/backend/internal/importer"
	"github.com/jittair/waypoint/backend/internal/integrations"
	"github.com/jittair/waypoint/backend/internal/store"
)

type Config struct {
	AllowedOrigins      []string
	InboundDomain       string
	InboundAddress      string
	InboundOwnerID      string
	ResendWebhookSecret string
	ImportProcessor     *importer.Processor
	Calendar            *calendar.Service
	AppURL              string
}
type API struct {
	store               *store.Store
	verifier            *auth.Verifier
	logger              *slog.Logger
	origins             map[string]bool
	inboundDomain       string
	inboundAddress      string
	inboundOwnerID      string
	resendWebhookSecret string
	calendar            *calendar.Service
	appURL              string
}
type contextKey string

const userIDKey contextKey = "user-id"

func New(st *store.Store, verifier *auth.Verifier, logger *slog.Logger, cfg Config) http.Handler {
	origins := map[string]bool{}
	for _, origin := range cfg.AllowedOrigins {
		origins[strings.TrimSpace(origin)] = true
	}
	a := &API{store: st, verifier: verifier, logger: logger, origins: origins, inboundDomain: cfg.InboundDomain, inboundAddress: strings.ToLower(strings.TrimSpace(cfg.InboundAddress)), inboundOwnerID: cfg.InboundOwnerID, resendWebhookSecret: cfg.ResendWebhookSecret, calendar: cfg.Calendar, appURL: cfg.AppURL}
	r := chi.NewRouter()
	r.Use(a.recoverer, a.cors, a.requestLog)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Post("/webhooks/resend/email-received", a.resendWebhook)
	r.Get("/calendar/callback", a.calendarCallback)
	r.Route("/v1", func(r chi.Router) {
		r.Use(a.authenticate)
		r.Get("/me", a.me)
		r.Get("/trips", a.listTrips)
		r.Get("/inbox", a.listInbox)
		r.Post("/trips", a.createTrip)
		r.Route("/trips/{tripID}", func(r chi.Router) {
			r.Get("/", a.tripDetail)
			r.Patch("/", a.updateTrip)
			r.Delete("/", a.deleteTrip)
			r.Post("/members", a.addMember)
			r.Delete("/members/{userID}", a.deleteMember)
			r.Post("/plans", a.createPlan)
			r.Patch("/plans/{planID}", a.updatePlan)
			r.Delete("/plans/{planID}", a.deletePlan)
			r.Post("/checklist", a.createChecklist)
			r.Patch("/checklist/{itemID}", a.updateChecklist)
			r.Delete("/checklist/{itemID}", a.deleteChecklist)
			r.Post("/documents", a.createDocument)
			r.Delete("/documents/{documentID}", a.deleteDocument)
			r.Post("/route-options", a.createRouteOption)
			r.Patch("/route-options/{optionID}", a.updateRouteOption)
			r.Delete("/route-options/{optionID}", a.deleteRouteOption)
			r.Get("/imports", a.listImports)
			r.Post("/import-address", a.importAddress)
		})
		r.Get("/imports/{importID}", a.importDetail)
		r.Post("/imports/{importID}/drafts/{draftID}/approve", a.approveDraft)
		r.Post("/imports/{importID}/drafts/{draftID}/discard", a.discardDraft)
		r.Post("/imports/{importID}/assign", a.assignImport)
		r.Get("/calendar/status", a.calendarStatus)
		r.Post("/calendar/connect", a.calendarConnect)
		r.Post("/calendar/sync", a.calendarSync)
		r.Delete("/calendar", a.calendarDisconnect)
	})
	return r
}

func (a *API) calendarConnect(w http.ResponseWriter, r *http.Request) {
	if a.calendar == nil {
		fail(w, http.StatusServiceUnavailable, "Google Calendar is not configured")
		return
	}
	url, err := a.calendar.AuthorizeURL(userID(r))
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"url": url})
}
func (a *API) calendarCallback(w http.ResponseWriter, r *http.Request) {
	if a.calendar == nil {
		fail(w, http.StatusServiceUnavailable, "Google Calendar is not configured")
		return
	}
	if _, err := a.calendar.Connect(r.Context(), r.URL.Query().Get("code"), r.URL.Query().Get("state")); err != nil {
		a.logger.Error("Google Calendar connection failed", "error", err)
		http.Redirect(w, r, a.appURL+"?calendar=error", http.StatusFound)
		return
	}
	http.Redirect(w, r, a.appURL+"?calendar=connected", http.StatusFound)
}
func (a *API) calendarStatus(w http.ResponseWriter, r *http.Request) {
	connection, err := a.store.CalendarConnection(r.Context(), userID(r))
	if errors.Is(err, store.ErrNotFound) {
		respond(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"connected": true, "email": connection.GoogleEmail, "calendarId": connection.CalendarID, "status": connection.Status, "lastError": connection.LastError, "lastSyncedAt": connection.LastSyncedAt})
}
func (a *API) calendarSync(w http.ResponseWriter, r *http.Request) {
	if a.calendar == nil {
		fail(w, http.StatusServiceUnavailable, "Google Calendar is not configured")
		return
	}
	if err := a.calendar.SyncUser(r.Context(), userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusAccepted, map[string]bool{"queued": true})
}
func (a *API) calendarDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DisconnectCalendar(r.Context(), userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) resendWebhook(w http.ResponseWriter, r *http.Request) {
	if a.resendWebhookSecret == "" {
		fail(w, http.StatusServiceUnavailable, "reservation imports are not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid webhook payload")
		return
	}
	if err := integrations.VerifySvix(a.resendWebhookSecret, r.Header.Get("svix-id"), r.Header.Get("svix-timestamp"), r.Header.Get("svix-signature"), raw); err != nil {
		fail(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			EmailID   string    `json:"email_id"`
			From      string    `json:"from"`
			Subject   string    `json:"subject"`
			To        []string  `json:"to"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		fail(w, http.StatusBadRequest, "invalid webhook event")
		return
	}
	if event.Type != "email.received" || event.Data.EmailID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var address store.ImportAddress
	var ownerID string
	found := false
	for _, recipient := range event.Data.To {
		if a.inboundAddress != "" && strings.ToLower(strings.TrimSpace(recipient)) == a.inboundAddress {
			ownerID = a.inboundOwnerID
			found = ownerID != ""
			break
		}
		token := importToken(recipient)
		if token == "" {
			continue
		}
		candidate, lookupErr := a.store.ImportAddressForToken(r.Context(), token)
		if lookupErr == nil {
			address = candidate
			ownerID = ""
			found = true
			break
		}
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var tripID *string
	if address.TripID != "" {
		tripID = &address.TripID
	}
	_, _, err = a.store.CreateInboundImport(r.Context(), tripID, ownerID, event.Data.EmailID, r.Header.Get("svix-id"), event.Data.From, event.Data.Subject, &event.Data.CreatedAt)
	if err != nil {
		a.logger.Error("persist inbound import", "error", err)
		fail(w, http.StatusInternalServerError, "could not receive import")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
func importToken(address string) string {
	local, _, ok := strings.Cut(strings.ToLower(strings.TrimSpace(address)), "@")
	if !ok {
		return ""
	}
	return strings.TrimPrefix(local, "imports-")
}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			fail(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := a.verifier.Verify(r.Context(), token)
		if err != nil {
			fail(w, http.StatusUnauthorized, "invalid access token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey, claims.Subject)))
	})
}
func userID(r *http.Request) string { id, _ := r.Context().Value(userIDKey).(string); return id }

func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && a.origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				a.logger.Error("panic recovered")
				fail(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (a *API) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		a.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{"id": userID(r)})
}
func (a *API) listTrips(w http.ResponseWriter, r *http.Request) {
	trips, err := a.store.ListTrips(r.Context(), userID(r))
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, trips)
}

func (a *API) listInbox(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListInbox(r.Context(), userID(r))
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, items)
}

func (a *API) assignImport(w http.ResponseWriter, r *http.Request) {
	importID, ok := tripParam(w, r, "importID")
	if !ok {
		return
	}
	var input struct {
		TripID string `json:"tripId"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.TripID == "" {
		fail(w, http.StatusBadRequest, "tripId is required")
		return
	}
	if err := a.store.AssignImport(r.Context(), importID, input.TripID, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "assigned"})
}

type tripRequest struct {
	Title       string    `json:"title"`
	Destination string    `json:"destination"`
	StartDate   time.Time `json:"startDate"`
	EndDate     time.Time `json:"endDate"`
	CoverColor  string    `json:"coverColor"`
}

func (a *API) createTrip(w http.ResponseWriter, r *http.Request) {
	var input tripRequest
	if !readJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Destination) == "" || input.StartDate.IsZero() || input.EndDate.IsZero() || input.EndDate.Before(input.StartDate) {
		fail(w, http.StatusBadRequest, "title, destination, and valid dates are required")
		return
	}
	trip, err := a.store.CreateTrip(r.Context(), userID(r), store.Trip{Title: input.Title, Destination: input.Destination, StartDate: input.StartDate, EndDate: input.EndDate, CoverColor: defaultColor(input.CoverColor)})
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusCreated, trip)
}
func (a *API) tripDetail(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	trip, plans, checklist, docs, routes, members, err := a.store.TripDetail(r.Context(), tripID, userID(r))
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"trip": trip, "plans": plans, "checklist": checklist, "documents": docs, "routeOptions": routes, "members": members})
}
func (a *API) updateTrip(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	var input tripRequest
	if !readJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Destination) == "" || input.StartDate.IsZero() || input.EndDate.IsZero() || input.EndDate.Before(input.StartDate) {
		fail(w, http.StatusBadRequest, "title, destination, and valid dates are required")
		return
	}
	trip, err := a.store.UpdateTrip(r.Context(), tripID, userID(r), store.Trip{Title: input.Title, Destination: input.Destination, StartDate: input.StartDate, EndDate: input.EndDate, CoverColor: defaultColor(input.CoverColor)})
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, trip)
}
func (a *API) deleteTrip(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	if err := a.store.DeleteTrip(r.Context(), tripID, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addMember(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !strings.Contains(input.Email, "@") {
		fail(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	member, err := a.store.AddMemberByEmail(r.Context(), tripID, userID(r), input.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusConflict, "user has not signed in yet or is already a member")
			return
		}
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusCreated, member)
}
func (a *API) deleteMember(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	target, ok := tripParam(w, r, "userID")
	if !ok {
		return
	}
	if err := a.store.DeleteMember(r.Context(), tripID, target, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type planRequest struct {
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	StartsAt         time.Time  `json:"startsAt"`
	EndsAt           *time.Time `json:"endsAt"`
	Location         string     `json:"location"`
	ConfirmationCode string     `json:"confirmationCode"`
	Notes            string     `json:"notes"`
	TimeZone         string     `json:"timeZone"`
}

func planFrom(input planRequest) (store.Plan, error) {
	if err := store.ValidateKind(input.Kind); err != nil {
		return store.Plan{}, err
	}
	if strings.TrimSpace(input.Title) == "" || input.StartsAt.IsZero() {
		return store.Plan{}, errors.New("title and start time are required")
	}
	if input.EndsAt != nil && input.EndsAt.Before(input.StartsAt) {
		return store.Plan{}, errors.New("end time must be after start time")
	}
	return store.Plan{Kind: input.Kind, Title: input.Title, StartsAt: input.StartsAt, EndsAt: input.EndsAt, Location: input.Location, ConfirmationCode: input.ConfirmationCode, Notes: input.Notes, TimeZone: input.TimeZone}, nil
}
func (a *API) createPlan(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	var input planRequest
	if !readJSON(w, r, &input) {
		return
	}
	plan, err := planFrom(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	plan.TripID = tripID
	plan, err = a.store.CreatePlan(r.Context(), userID(r), plan)
	if err != nil {
		a.handleError(w, err)
		return
	}
	_ = a.store.QueueCalendarSync(r.Context(), userID(r))
	respond(w, http.StatusCreated, plan)
}
func (a *API) updatePlan(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	planID, ok := tripParam(w, r, "planID")
	if !ok {
		return
	}
	var input planRequest
	if !readJSON(w, r, &input) {
		return
	}
	plan, err := planFrom(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	plan, err = a.store.UpdatePlan(r.Context(), tripID, planID, userID(r), plan)
	if err != nil {
		a.handleError(w, err)
		return
	}
	_ = a.store.QueueCalendarSync(r.Context(), userID(r))
	respond(w, http.StatusOK, plan)
}
func (a *API) deletePlan(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	planID, ok := tripParam(w, r, "planID")
	if !ok {
		return
	}
	if err := a.store.DeletePlan(r.Context(), tripID, planID, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	_ = a.store.QueueCalendarSync(r.Context(), userID(r))
	w.WriteHeader(http.StatusNoContent)
}

type checklistRequest struct {
	Title      string `json:"title"`
	IsComplete bool   `json:"isComplete"`
	SortOrder  int    `json:"sortOrder"`
}

func checklistFrom(input checklistRequest) (store.ChecklistItem, error) {
	if strings.TrimSpace(input.Title) == "" {
		return store.ChecklistItem{}, errors.New("title is required")
	}
	return store.ChecklistItem{Title: input.Title, IsComplete: input.IsComplete, SortOrder: input.SortOrder}, nil
}
func (a *API) createChecklist(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	var input checklistRequest
	if !readJSON(w, r, &input) {
		return
	}
	item, err := checklistFrom(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	item.TripID = tripID
	item, err = a.store.CreateChecklist(r.Context(), userID(r), item)
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusCreated, item)
}
func (a *API) updateChecklist(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	itemID, ok := tripParam(w, r, "itemID")
	if !ok {
		return
	}
	var input checklistRequest
	if !readJSON(w, r, &input) {
		return
	}
	item, err := checklistFrom(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err = a.store.UpdateChecklist(r.Context(), tripID, itemID, userID(r), item)
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, item)
}
func (a *API) deleteChecklist(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	itemID, ok := tripParam(w, r, "itemID")
	if !ok {
		return
	}
	if err := a.store.DeleteChecklist(r.Context(), tripID, itemID, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type documentRequest struct {
	Name        string `json:"name"`
	StoragePath string `json:"storagePath"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
}

func (a *API) createDocument(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	var input documentRequest
	if !readJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" || !strings.HasPrefix(input.StoragePath, tripID+"/") || input.SizeBytes < 0 || input.SizeBytes > 10485760 {
		fail(w, http.StatusBadRequest, "invalid document metadata")
		return
	}
	switch input.ContentType {
	case "application/pdf", "image/jpeg", "image/png", "image/webp":
	default:
		fail(w, http.StatusBadRequest, "unsupported document type")
		return
	}
	doc, err := a.store.CreateDocument(r.Context(), userID(r), store.Document{TripID: tripID, Name: input.Name, StoragePath: input.StoragePath, ContentType: input.ContentType, SizeBytes: input.SizeBytes})
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusCreated, doc)
}
func (a *API) deleteDocument(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	docID, ok := tripParam(w, r, "documentID")
	if !ok {
		return
	}
	if err := a.store.DeleteDocument(r.Context(), tripID, docID, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) importAddress(w http.ResponseWriter, r *http.Request) {
	if a.inboundDomain == "" {
		fail(w, http.StatusServiceUnavailable, "reservation imports are not configured")
		return
	}
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	token, err := newImportToken()
	if err != nil {
		a.handleError(w, err)
		return
	}
	address, err := a.store.EnsureImportAddress(r.Context(), tripID, userID(r), token)
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"address": "imports-" + address.Token + "@" + a.inboundDomain})
}
func (a *API) listImports(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	items, err := a.store.ListImports(r.Context(), tripID, userID(r))
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, items)
}
func (a *API) importDetail(w http.ResponseWriter, r *http.Request) {
	importID, ok := tripParam(w, r, "importID")
	if !ok {
		return
	}
	item, drafts, attachments, err := a.store.ImportDetail(r.Context(), importID, userID(r))
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"import": item, "drafts": drafts, "attachments": attachments})
}
func (a *API) approveDraft(w http.ResponseWriter, r *http.Request) {
	draftID, ok := tripParam(w, r, "draftID")
	if !ok {
		return
	}
	var input planRequest
	if !readJSON(w, r, &input) {
		return
	}
	plan, err := planFrom(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	plan.TimeZone = "UTC"
	plan, err = a.store.ApproveDraft(r.Context(), draftID, userID(r), plan)
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusCreated, plan)
}
func (a *API) discardDraft(w http.ResponseWriter, r *http.Request) {
	draftID, ok := tripParam(w, r, "draftID")
	if !ok {
		return
	}
	if err := a.store.DiscardDraft(r.Context(), draftID, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func newImportToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type routeOptionRequest struct {
	Title           string     `json:"title"`
	RouteType       string     `json:"routeType"`
	Origin          string     `json:"origin"`
	Destination     string     `json:"destination"`
	DepartsAt       *time.Time `json:"departsAt"`
	ArrivesAt       *time.Time `json:"arrivesAt"`
	DurationMinutes *int       `json:"durationMinutes"`
	Transfers       int        `json:"transfers"`
	PriceAmount     *float64   `json:"priceAmount"`
	Currency        *string    `json:"currency"`
	BookingURL      string     `json:"bookingUrl"`
	Notes           string     `json:"notes"`
	Status          string     `json:"status"`
}

func routeOptionFrom(input routeOptionRequest) (store.RouteOption, error) {
	if strings.TrimSpace(input.Title) == "" {
		return store.RouteOption{}, errors.New("title is required")
	}
	if err := store.ValidateRouteType(input.RouteType); err != nil {
		return store.RouteOption{}, err
	}
	if err := store.ValidateRouteStatus(input.Status); err != nil {
		return store.RouteOption{}, err
	}
	if input.Transfers < 0 || input.Transfers > 20 {
		return store.RouteOption{}, errors.New("transfers must be between 0 and 20")
	}
	if input.DurationMinutes != nil && *input.DurationMinutes < 0 {
		return store.RouteOption{}, errors.New("duration cannot be negative")
	}
	if input.DepartsAt != nil && input.ArrivesAt != nil && input.ArrivesAt.Before(*input.DepartsAt) {
		return store.RouteOption{}, errors.New("arrival must be after departure")
	}
	if input.PriceAmount != nil && *input.PriceAmount < 0 {
		return store.RouteOption{}, errors.New("price cannot be negative")
	}
	if input.PriceAmount == nil && input.Currency != nil {
		return store.RouteOption{}, errors.New("currency requires a price")
	}
	if input.PriceAmount != nil && (input.Currency == nil || len(*input.Currency) != 3) {
		return store.RouteOption{}, errors.New("a three-letter currency is required with a price")
	}
	if input.BookingURL != "" && !strings.HasPrefix(input.BookingURL, "https://") {
		return store.RouteOption{}, errors.New("booking link must use https")
	}
	if input.Currency != nil {
		value := strings.ToUpper(*input.Currency)
		input.Currency = &value
	}
	return store.RouteOption{Title: input.Title, RouteType: input.RouteType, Origin: input.Origin, Destination: input.Destination, DepartsAt: input.DepartsAt, ArrivesAt: input.ArrivesAt, DurationMinutes: input.DurationMinutes, Transfers: input.Transfers, PriceAmount: input.PriceAmount, Currency: input.Currency, BookingURL: input.BookingURL, Notes: input.Notes, Status: input.Status}, nil
}

func (a *API) createRouteOption(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	var input routeOptionRequest
	if !readJSON(w, r, &input) {
		return
	}
	option, err := routeOptionFrom(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	option.TripID = tripID
	option, err = a.store.CreateRouteOption(r.Context(), userID(r), option)
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusCreated, option)
}

func (a *API) updateRouteOption(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	optionID, ok := tripParam(w, r, "optionID")
	if !ok {
		return
	}
	var input routeOptionRequest
	if !readJSON(w, r, &input) {
		return
	}
	option, err := routeOptionFrom(input)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	option, err = a.store.UpdateRouteOption(r.Context(), tripID, optionID, userID(r), option)
	if err != nil {
		a.handleError(w, err)
		return
	}
	respond(w, http.StatusOK, option)
}

func (a *API) deleteRouteOption(w http.ResponseWriter, r *http.Request) {
	tripID, ok := tripParam(w, r, "tripID")
	if !ok {
		return
	}
	optionID, ok := tripParam(w, r, "optionID")
	if !ok {
		return
	}
	if err := a.store.DeleteRouteOption(r.Context(), tripID, optionID, userID(r)); err != nil {
		a.handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrForbidden):
		fail(w, http.StatusForbidden, "forbidden")
	default:
		a.logger.Error("request failed", "error", err)
		fail(w, http.StatusInternalServerError, "internal server error")
	}
}
func tripParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if _, err := uuid.Parse(id); err != nil {
		fail(w, http.StatusBadRequest, "invalid "+name)
		return "", false
	}
	return id, true
}
func defaultColor(color string) string {
	if color == "" {
		return "#1d4c46"
	}
	return color
}
func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
