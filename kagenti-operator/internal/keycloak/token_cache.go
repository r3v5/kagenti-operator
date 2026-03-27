/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

package keycloak

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// tokenCacheSkew is how long before OAuth expiry we refresh the cached admin token.
const tokenCacheSkew = 60 * time.Second

// CachedAdminTokenProvider caches Keycloak admin password-grant tokens keyed by base URL and
// credentials so frequent reconciles do not issue a new token request every time.
type CachedAdminTokenProvider struct {
	mu      sync.Mutex
	entries map[string]cachedAdminTokenEntry
}

type cachedAdminTokenEntry struct {
	token     string
	expiresAt time.Time
}

func adminTokenCacheKey(baseURL, username, password string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	sum := sha256.Sum256([]byte(base + "\x00" + username + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

// Token returns a valid admin access token, reusing the cache when the token is not near expiry.
func (p *CachedAdminTokenProvider) Token(ctx context.Context, a *Admin, adminUser, adminPass string) (string, error) {
	key := adminTokenCacheKey(a.BaseURL, adminUser, adminPass)
	now := time.Now()

	p.mu.Lock()
	if p.entries != nil {
		for k, e := range p.entries {
			if !now.Before(e.expiresAt) {
				delete(p.entries, k)
			}
		}
		if e, ok := p.entries[key]; ok && now.Before(e.expiresAt.Add(-tokenCacheSkew)) {
			tok := e.token
			p.mu.Unlock()
			return tok, nil
		}
	}
	p.mu.Unlock()

	token, expiresAt, err := a.adminToken(ctx, adminUser, adminPass)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = make(map[string]cachedAdminTokenEntry)
	}
	p.entries[key] = cachedAdminTokenEntry{token: token, expiresAt: expiresAt}
	return token, nil
}
