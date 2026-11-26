package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func (p *KeyPool) VerifyAdminPassword(candidate string) bool {
	p.mu.RLock()
	salt := p.adminSalt
	expected := p.adminHash
	p.mu.RUnlock()
	if salt == "" || expected == "" {
		return false
	}
	got := hashAdminPassword(candidate, salt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func (p *KeyPool) UpdateAdminPassword(currentPass, newPass string) error {
	if !p.VerifyAdminPassword(currentPass) {
		return errors.New("sandi lama salah")
	}
	if len(newPass) < 4 {
		return errors.New("sandi baru minimal 4 karakter")
	}
	salt := randomHex(16)
	hash := hashAdminPassword(newPass, salt)
	secHex := randomHex(32)
	secBytes, _ := hex.DecodeString(secHex)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.setMeta("admin_salt", salt); err != nil {
		return err
	}
	if err := p.setMeta("admin_hash", hash); err != nil {
		return err
	}
	if err := p.setMeta("session_secret", secHex); err != nil {
		return err
	}
	p.adminSalt = salt
	p.adminHash = hash
	p.sessionSecret = secBytes
	return nil
}

func (p *KeyPool) issueSessionJWTAt(nowUnix int64) (string, int64) {
	p.mu.RLock()
	secret := append([]byte(nil), p.sessionSecret...)
	p.mu.RUnlock()
	exp := nowUnix + sessionTTLSeconds
	hdrBytes, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payBytes, _ := json.Marshal(map[string]any{
		"sub": "admin",
		"iat": nowUnix,
		"exp": exp,
		"jti": randomHex(8),
	})
	unsigned := base64.RawURLEncoding.EncodeToString(hdrBytes) + "." + base64.RawURLEncoding.EncodeToString(payBytes)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + sig, exp
}

func (p *KeyPool) IssueSessionJWT() (string, int64) {
	return p.issueSessionJWTAt(time.Now().Unix())
}

func (p *KeyPool) IssueSessionToken() string {
	tok, _ := p.IssueSessionJWT()
	return tok
}

func (p *KeyPool) ParseSessionJWT(tok string) (int64, bool) {
	parts := strings.Split(strings.TrimSpace(tok), ".")
	if len(parts) != 3 {
		return 0, false
	}
	p.mu.RLock()
	secret := append([]byte(nil), p.sessionSecret...)
	p.mu.RUnlock()
	if len(secret) == 0 {
		return 0, false
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	expectedSig := mac.Sum(nil)
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(gotSig, expectedSig) {
		return 0, false
	}
	hdrRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, false
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(hdrRaw, &hdr) != nil || hdr.Alg != "HS256" {
		return 0, false
	}
	payRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, false
	}
	var claims struct {
		Sub string `json:"sub"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
	}
	if json.Unmarshal(payRaw, &claims) != nil || claims.Exp <= 0 {
		return 0, false
	}
	if time.Now().Unix() >= claims.Exp {
		return 0, false
	}
	return claims.Exp, true
}

func (p *KeyPool) VerifySessionToken(tok string) bool {
	_, ok := p.ParseSessionJWT(tok)
	return ok
}

func (p *KeyPool) AuthenticateClientToken(rawToken string) (ClientKey, bool) {
	if rawToken == "" {
		return ClientKey{}, false
	}
	h := sha256Hex(rawToken)
	p.mu.RLock()
	ck, ok := p.clientByHash[h]
	if ok && ck.Enabled {
		cp := *ck
		p.mu.RUnlock()
		return cp, true
	}
	p.mu.RUnlock()

	if p.cfg.LegacyGatewayKey != "" && len(rawToken) == len(p.cfg.LegacyGatewayKey) {
		if subtle.ConstantTimeCompare([]byte(rawToken), []byte(p.cfg.LegacyGatewayKey)) == 1 {
			return ClientKey{
				ID:      "",
				Name:    "env-key",
				Scope:   scopeGlobal,
				Enabled: true,
			}, true
		}
	}
	return ClientKey{}, false
}

func (p *KeyPool) RecordClientUsage(clientID string, tokens int) {
	if clientID == "" {
		return
	}
	p.mu.Lock()
	if ck, ok := p.clientByID[clientID]; ok {
		ck.UsageCount++
		ck.TotalTokens += int64(tokens)
		ck.LastUsedUnix = time.Now().Unix()
	}
	p.mu.Unlock()
	p.enqueueWrite(writeEvent{clientKeyID: clientID})
}

func (p *KeyPool) ClientKeyViews() []ClientKey {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]ClientKey, 0, len(p.clients))
	for _, c := range p.clients {
		out = append(out, *c)
	}
	return out
}

func (p *KeyPool) validateScopeLocked(rawScope string) (string, string, error) {
	sc := strings.ToLower(strings.TrimSpace(rawScope))
	if sc == "" || sc == scopeGlobal {
		return scopeGlobal, "Global Key", nil
	}
	if strings.HasPrefix(sc, scopeCategoryPrefix) {
		cat := normalizeCategory(strings.TrimPrefix(sc, scopeCategoryPrefix))
		return scopeCategoryPrefix + cat, "Grup " + cat + " Key", nil
	}
	pr, ok := p.providerByID[sc]
	if !ok {
		return "", "", fmt.Errorf("provider atau kategori %q tidak ditemukan", sc)
	}
	return sc, pr.Name + " Key", nil
}

func (p *KeyPool) CreateClientKey(name, scope, customKey string) (ClientKey, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	normScope, defaultName, err := p.validateScopeLocked(scope)
	if err != nil {
		return ClientKey{}, "", err
	}
	scope = normScope
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultName
	}
	secret := strings.TrimSpace(customKey)
	if secret != "" {
		if len(secret) < 8 {
			return ClientKey{}, "", fmt.Errorf("custom key minimal 8 karakter")
		}
	} else {
		tag := "glb"
		if strings.HasPrefix(scope, scopeCategoryPrefix) {
			tag = "cat"
		} else if scope != scopeGlobal {
			tag = scope
			if len(tag) > 3 {
				tag = tag[:3]
			}
		}
		secret = "kr_" + tag + "_" + randomHex(16)
	}
	keyHash := sha256Hex(secret)
	id := randomHex(8)
	now := time.Now().Unix()
	ck := &ClientKey{
		ID:        id,
		Name:      name,
		Scope:     scope,
		KeyHash:   keyHash,
		Masked:    maskKey(secret),
		Enabled:   true,
		CreatedAt: now,
	}

	if _, exists := p.clientByHash[keyHash]; exists {
		return ClientKey{}, "", fmt.Errorf("api key sudah terdaftar")
	}
	if _, err := p.db.Exec(
		`INSERT INTO client_keys(id,name,scope,key_hash,masked,enabled,usage_count,total_tokens,last_used_unix,created_at) VALUES(?,?,?,?,?,1,0,0,0,?)`,
		ck.ID, ck.Name, ck.Scope, ck.KeyHash, ck.Masked, ck.CreatedAt,
	); err != nil {
		return ClientKey{}, "", err
	}
	p.clients = append([]*ClientKey{ck}, p.clients...)
	p.clientByID[ck.ID] = ck
	p.clientByHash[ck.KeyHash] = ck
	return *ck, secret, nil
}

func (p *KeyPool) PatchClientKey(id string, enabled *bool, name *string, scope *string) (ClientKey, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ck, ok := p.clientByID[id]
	if !ok {
		return ClientKey{}, false, nil
	}
	nextEnabled := ck.Enabled
	if enabled != nil {
		nextEnabled = *enabled
	}
	nextName := ck.Name
	if name != nil && strings.TrimSpace(*name) != "" {
		nextName = strings.TrimSpace(*name)
	}
	nextScope := ck.Scope
	if scope != nil {
		sc, _, err := p.validateScopeLocked(*scope)
		if err != nil {
			return ClientKey{}, true, err
		}
		nextScope = sc
	}
	enInt := 0
	if nextEnabled {
		enInt = 1
	}
	if _, err := p.db.Exec(`UPDATE client_keys SET name=?, scope=?, enabled=? WHERE id=?`, nextName, nextScope, enInt, id); err != nil {
		return ClientKey{}, true, err
	}
	ck.Enabled = nextEnabled
	ck.Name = nextName
	ck.Scope = nextScope
	return *ck, true, nil
}

func (p *KeyPool) DeleteClientKey(id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ck, ok := p.clientByID[id]
	if !ok {
		return false, nil
	}
	if _, err := p.db.Exec(`DELETE FROM client_keys WHERE id=?`, id); err != nil {
		return true, err
	}
	delete(p.clientByID, id)
	delete(p.clientByHash, ck.KeyHash)
	next := make([]*ClientKey, 0, len(p.clients)-1)
	for _, item := range p.clients {
		if item.ID != id {
			next = append(next, item)
		}
	}
	p.clients = next
	return true, nil
}

func extractClientToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if auth != "" {
		return auth
	}
	if xk := strings.TrimSpace(r.Header.Get("x-api-key")); xk != "" {
		return xk
	}
	if gk := strings.TrimSpace(r.Header.Get("x-goog-api-key")); gk != "" {
		return gk
	}
	return strings.TrimSpace(r.URL.Query().Get("key"))
}

func (g *Gateway) sessionExpiry(r *http.Request) (int64, bool) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if exp, ok := g.pool.ParseSessionJWT(c.Value); ok {
			return exp, true
		}
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		tok := strings.TrimSpace(auth[7:])
		if tok != "" {
			if exp, ok := g.pool.ParseSessionJWT(tok); ok {
				return exp, true
			}
		}
	}
	return 0, false
}

func (g *Gateway) adminAuthorized(r *http.Request) bool {
	_, ok := g.sessionExpiry(r)
	return ok
}

func (g *Gateway) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	isHTTPS := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS,
	})
}

func clientIP(r *http.Request) string {
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
		if idx := strings.IndexByte(fwd, ','); idx > 0 {
			return strings.TrimSpace(fwd[:idx])
		}
		return fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (g *Gateway) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	exp, ok := g.sessionExpiry(r)
	var rem int64
	if ok {
		rem = exp - time.Now().Unix()
		if rem < 0 {
			rem = 0
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": ok,
		"expires_at":    exp,
		"expires_in":    rem,
	})
}

func (g *Gateway) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	ip := clientIP(r)
	now := time.Now().Unix()

	g.loginMu.Lock()
	st, ok := g.loginFails[ip]
	if ok && st.blockedUntil > now {
		rem := st.blockedUntil - now
		g.loginMu.Unlock()
		writeJSONError(w, http.StatusTooManyRequests, fmt.Sprintf("coba lagi dalam %d detik", rem), "rate_limit_error")
		return
	}
	g.loginMu.Unlock()

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	if !g.pool.VerifyAdminPassword(body.Password) {
		g.loginMu.Lock()
		if g.loginFails == nil {
			g.loginFails = make(map[string]*authFailureState)
		}
		cur := g.loginFails[ip]
		if cur == nil {
			cur = &authFailureState{}
			g.loginFails[ip] = cur
		}
		cur.count++
		if cur.count >= 6 {
			cur.blockedUntil = now + 60
			cur.count = 0
		}
		g.loginMu.Unlock()
		writeJSONError(w, http.StatusUnauthorized, "sandi salah", "auth_error")
		return
	}

	g.loginMu.Lock()
	delete(g.loginFails, ip)
	g.loginMu.Unlock()

	tok, exp := g.pool.IssueSessionJWT()
	g.setSessionCookie(w, r, tok, sessionTTLSeconds)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"authenticated": true,
		"token":         tok,
		"expires_at":    exp,
		"expires_in":    sessionTTLSeconds,
	})
}

func (g *Gateway) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	g.setSessionCookie(w, r, "", -1)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (g *Gateway) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	if err := g.pool.UpdateAdminPassword(body.CurrentPassword, body.NewPassword); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	tok, exp := g.pool.IssueSessionJWT()
	g.setSessionCookie(w, r, tok, sessionTTLSeconds)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"token":      tok,
		"expires_at": exp,
		"expires_in": sessionTTLSeconds,
	})
}
