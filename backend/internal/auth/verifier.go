package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

type Verifier struct {
	issuer string
	cache  *jwk.Cache
	jwks   string
}

func NewVerifier(ctx context.Context, supabaseURL string) (*Verifier, error) {
	base := strings.TrimRight(supabaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("SUPABASE_URL is required")
	}

	jwksURL := base + "/auth/v1/.well-known/jwks.json"
	cache := jwk.NewCache(ctx)
	cache.Register(jwksURL, jwk.WithMinRefreshInterval(time.Hour))
	if _, err := cache.Get(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("load Supabase JWKS: %w", err)
	}

	return &Verifier{issuer: base + "/auth/v1", cache: cache, jwks: jwksURL}, nil
}

func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	keySet, err := v.cache.Get(ctx, v.jwks)
	if err != nil {
		return nil, fmt.Errorf("refresh JWKS: %w", err)
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("JWT is missing kid")
		}
		key, found := keySet.LookupKeyID(kid)
		if !found {
			return nil, fmt.Errorf("unknown JWT signing key")
		}
		var rawKey any
		if err := key.Raw(&rawKey); err != nil {
			return nil, fmt.Errorf("read JWT signing key: %w", err)
		}
		return rawKey, nil
	}, jwt.WithIssuer(v.issuer), jwt.WithAudience("authenticated"), jwt.WithValidMethods([]string{"RS256", "ES256", "EdDSA"}))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("access token has no subject")
	}
	return claims, nil
}
