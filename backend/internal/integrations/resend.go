package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ResendEmail struct {
	ID, Sender, Subject, HTML, Text, RawURL string
	ReceivedAt                              *time.Time
	Attachments                             []ResendAttachment
}
type ResendAttachment struct {
	ID, Filename, ContentType, DownloadURL string
	SizeBytes                              int64
}
type ResendClient struct {
	apiKey string
	client *http.Client
}

func NewResendClient(apiKey string) *ResendClient {
	if apiKey == "" {
		return nil
	}
	return &ResendClient{apiKey: apiKey, client: &http.Client{Timeout: 30 * time.Second}}
}

func (c *ResendClient) GetReceivedEmail(ctx context.Context, id string) (ResendEmail, error) {
	if c == nil {
		return ResendEmail{}, fmt.Errorf("Resend is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.resend.com/emails/receiving/"+id, nil)
	if err != nil {
		return ResendEmail{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	response, err := c.client.Do(request)
	if err != nil {
		return ResendEmail{}, err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return ResendEmail{}, fmt.Errorf("retrieve received email: %s", response.Status)
	}
	var payload struct {
		ID        string    `json:"id"`
		From      string    `json:"from"`
		Subject   string    `json:"subject"`
		HTML      string    `json:"html"`
		Text      string    `json:"text"`
		CreatedAt time.Time `json:"created_at"`
		Raw       struct {
			DownloadURL string `json:"download_url"`
		} `json:"raw"`
		Attachments []struct {
			ID          string `json:"id"`
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
			SizeBytes   int64  `json:"size"`
		} `json:"attachments"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ResendEmail{}, err
	}
	attachments, err := c.listAttachments(ctx, id)
	if err != nil {
		return ResendEmail{}, err
	}
	return ResendEmail{ID: payload.ID, Sender: payload.From, Subject: payload.Subject, HTML: payload.HTML, Text: payload.Text, RawURL: payload.Raw.DownloadURL, ReceivedAt: &payload.CreatedAt, Attachments: attachments}, nil
}
func (c *ResendClient) listAttachments(ctx context.Context, emailID string) ([]ResendAttachment, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.resend.com/emails/receiving/"+emailID+"/attachments", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("retrieve attachments: %s", response.Status)
	}
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			Filename    string `json:"filename"`
			ContentType string `json:"content_type"`
			DownloadURL string `json:"download_url"`
			SizeBytes   int64  `json:"size"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	items := make([]ResendAttachment, 0, len(payload.Data))
	for _, item := range payload.Data {
		items = append(items, ResendAttachment{ID: item.ID, Filename: item.Filename, ContentType: item.ContentType, DownloadURL: item.DownloadURL, SizeBytes: item.SizeBytes})
	}
	return items, nil
}
func (c *ResendClient) Download(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("download source: %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 10*1024*1024+1))
}
func StripHTML(source string) string {
	text := source
	for {
		start := strings.Index(text, "<")
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], ">")
		if end < 0 {
			break
		}
		text = text[:start] + " " + text[start+end+1:]
	}
	return strings.Join(strings.Fields(text), " ")
}
