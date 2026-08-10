package calendar

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jittair/waypoint/backend/internal/store"
)

type Config struct{ ClientID, ClientSecret, RedirectURL, StateSecret, TokenKey string }
type Service struct {
	config Config
	store  *store.Store
	client *http.Client
}

func New(config Config, st *store.Store) *Service {
	if config.ClientID == "" || config.ClientSecret == "" || config.RedirectURL == "" || config.StateSecret == "" || config.TokenKey == "" {
		return nil
	}
	return &Service{config: config, store: st, client: &http.Client{Timeout: 30 * time.Second}}
}
func (s *Service) AuthorizeURL(userID string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("Google Calendar is not configured")
	}
	state := s.signState(userID, time.Now().Add(10*time.Minute))
	params := url.Values{"client_id": {s.config.ClientID}, "redirect_uri": {s.config.RedirectURL}, "response_type": {"code"}, "scope": {"openid email https://www.googleapis.com/auth/calendar"}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state}}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode(), nil
}
func (s *Service) Connect(ctx context.Context, code, state string) (string, error) {
	userID, err := s.verifyState(state)
	if err != nil {
		return "", err
	}
	form := url.Values{"code": {code}, "client_id": {s.config.ClientID}, "client_secret": {s.config.ClientSecret}, "redirect_uri": {s.config.RedirectURL}, "grant_type": {"authorization_code"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return "", fmt.Errorf("Google token exchange: %s", response.Status)
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", err
	}
	if token.RefreshToken == "" {
		return "", fmt.Errorf("Google did not return a refresh token")
	}
	email, err := s.googleEmail(ctx, token.AccessToken)
	if err != nil {
		return "", err
	}
	calendarID, err := s.createCalendar(ctx, token.AccessToken)
	if err != nil {
		return "", err
	}
	encrypted, err := s.encrypt([]byte(token.RefreshToken))
	if err != nil {
		return "", err
	}
	_, err = s.store.UpsertCalendarConnection(ctx, userID, email, calendarID, encrypted)
	return userID, err
}
func (s *Service) SyncUser(ctx context.Context, userID string) error {
	if s == nil {
		return fmt.Errorf("Google Calendar is not configured")
	}
	if err := s.store.QueueCalendarSync(ctx, userID); err != nil {
		return err
	}
	return s.RunOnce(ctx)
}
func (s *Service) RunOnce(ctx context.Context) error {
	if s == nil {
		return nil
	}
	connection, err := s.store.ClaimCalendarSync(ctx)
	if err == store.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if err = s.exportPlans(ctx, connection); err != nil {
		_ = s.store.FailCalendarSync(ctx, connection.ID, err)
		return err
	}
	return s.store.CompleteCalendarSync(ctx, connection.ID, connection.SyncToken)
}
func (s *Service) exportPlans(ctx context.Context, connection store.CalendarConnection) error {
	refresh, err := s.decrypt(connection.EncryptedRefreshToken)
	if err != nil {
		return err
	}
	access, err := s.refresh(ctx, string(refresh))
	if err != nil {
		return err
	}
	plans, err := s.store.CalendarPlans(ctx, connection.UserID)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		link, linkErr := s.store.CalendarLink(ctx, connection.ID, plan.ID)
		event := map[string]any{"summary": plan.Title, "location": plan.Location, "description": plan.Notes, "start": map[string]any{"dateTime": plan.StartsAt.Format(time.RFC3339), "timeZone": plan.TimeZone}, "end": map[string]any{"dateTime": endTime(plan).Format(time.RFC3339), "timeZone": plan.TimeZone}, "extendedProperties": map[string]any{"private": map[string]string{"waypoint_plan_id": plan.ID}}}
		method := http.MethodPost
		endpoint := "https://www.googleapis.com/calendar/v3/calendars/" + url.PathEscape(connection.CalendarID) + "/events"
		if linkErr == nil {
			method = http.MethodPut
			endpoint += "/" + url.PathEscape(link.GoogleEventID)
		}
		response, err := s.calendarRequest(ctx, access, method, endpoint, event)
		if err != nil {
			return err
		}
		var remote struct {
			ID      string    `json:"id"`
			ETag    string    `json:"etag"`
			Updated time.Time `json:"updated"`
		}
		if err := json.NewDecoder(response.Body).Decode(&remote); err != nil {
			response.Body.Close()
			return err
		}
		response.Body.Close()
		if err := s.store.UpsertCalendarLink(ctx, store.CalendarLink{ConnectionID: connection.ID, PlanID: plan.ID, GoogleEventID: remote.ID, GoogleETag: remote.ETag, RemoteUpdatedAt: &remote.Updated, LastSource: "waypoint"}); err != nil {
			return err
		}
	}
	return nil
}
func endTime(plan store.Plan) time.Time {
	if plan.EndsAt != nil {
		return *plan.EndsAt
	}
	return plan.StartsAt.Add(time.Hour)
}
func (s *Service) refresh(ctx context.Context, refresh string) (string, error) {
	form := url.Values{"client_id": {s.config.ClientID}, "client_secret": {s.config.ClientSecret}, "refresh_token": {refresh}, "grant_type": {"refresh_token"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return "", fmt.Errorf("Google token refresh: %s", response.Status)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", err
	}
	return token.AccessToken, nil
}
func (s *Service) googleEmail(ctx context.Context, access string) (string, error) {
	response, err := s.calendarRequest(ctx, access, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		return "", err
	}
	return info.Email, nil
}
func (s *Service) createCalendar(ctx context.Context, access string) (string, error) {
	response, err := s.calendarRequest(ctx, access, http.MethodPost, "https://www.googleapis.com/calendar/v3/calendars", map[string]string{"summary": "Waypoint"})
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ID, nil
}
func (s *Service) calendarRequest(ctx context.Context, access, method, endpoint string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+access)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode/100 != 2 {
		response.Body.Close()
		return nil, fmt.Errorf("Google Calendar %s: %s", method, response.Status)
	}
	return response, nil
}
func (s *Service) signState(userID string, expires time.Time) string {
	payload := userID + "." + fmt.Sprint(expires.Unix())
	mac := hmac.New(sha256.New, []byte(s.config.StateSecret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))))
}
func (s *Service) verifyState(state string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid OAuth state")
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().After(time.Unix(expires, 0)) {
		return "", fmt.Errorf("expired OAuth state")
	}
	mac := hmac.New(sha256.New, []byte(s.config.StateSecret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(mac.Sum(nil), signature) {
		return "", fmt.Errorf("invalid OAuth state")
	}
	return parts[0], nil
}
func (s *Service) encrypt(plain []byte) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s.config.TokenKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}
func (s *Service) decrypt(ciphertext []byte) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s.config.TokenKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted token")
	}
	return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
}
