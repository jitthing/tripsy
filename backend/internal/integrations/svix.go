package integrations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func VerifySvix(secret, id, timestamp, signature string, payload []byte) error {
	if secret == "" || id == "" || timestamp == "" || signature == "" {
		return fmt.Errorf("missing webhook signature headers")
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid webhook timestamp")
	}
	if delta := time.Since(time.Unix(unix, 0)); delta > 5*time.Minute || delta < -5*time.Minute {
		return fmt.Errorf("expired webhook timestamp")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		return fmt.Errorf("decode webhook secret: %w", err)
	}
	mac := hmac.New(sha256.New, decoded)
	_, _ = mac.Write([]byte(id + "." + timestamp + "." + string(payload)))
	expected := mac.Sum(nil)
	for _, candidate := range strings.Fields(signature) {
		parts := strings.SplitN(candidate, ",", 2)
		if len(parts) != 2 || parts[0] != "v1" {
			continue
		}
		actual, decodeErr := base64.StdEncoding.DecodeString(parts[1])
		if decodeErr == nil && hmac.Equal(expected, actual) {
			return nil
		}
	}
	return fmt.Errorf("invalid webhook signature")
}
