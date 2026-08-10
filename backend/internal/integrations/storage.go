package integrations

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Storage struct {
	baseURL, serviceRole string
	client               *http.Client
}

func NewStorage(supabaseURL, serviceRole string) *Storage {
	if supabaseURL == "" || serviceRole == "" {
		return nil
	}
	return &Storage{baseURL: strings.TrimRight(supabaseURL, "/") + "/storage/v1/object", serviceRole: serviceRole, client: &http.Client{}}
}

func (s *Storage) Put(ctx context.Context, bucket, path, contentType string, body []byte) error {
	if s == nil {
		return fmt.Errorf("Supabase Storage is not configured")
	}
	endpoint := s.baseURL + "/" + bucket + "/" + escapePath(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.serviceRole)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "false")
	response, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("store object: %s: %s", response.Status, raw)
	}
	return nil
}

func escapePath(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
