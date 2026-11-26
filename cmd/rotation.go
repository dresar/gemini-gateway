package main

import (
	"encoding/json"
	"fmt"
	"log"
	mathrand "math/rand"
	"sort"
	"strings"
	"time"
)

func (p *KeyPool) ProviderViews() []ProviderView {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now().Unix()
	out := make([]ProviderView, 0, len(p.providers))
	for _, pr := range p.providers {
		pv := ProviderView{
			Provider: *pr,
			ReqCount: p.providerReqs[pr.ID],
		}
		for _, k := range p.keys {
			if k.Provider != pr.ID {
				continue
			}
			pv.TotalKeys++
			if k.Stat.Health != "dead" {
				pv.HealthyKeys++
				if k.Stat.Enabled && k.Stat.CooldownUntil <= now {
					pv.EligibleKeys++
				}
			}
		}
		out = append(out, pv)
	}
	return out
}

func (p *KeyPool) GetProvider(id string) (Provider, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pr, ok := p.providerByID[id]
	if !ok {
		return Provider{}, false
	}
	return *pr, true
}

func (p *KeyPool) CreateProvider(pr Provider) (Provider, error) {
	if err := normalizeProvider(&pr); err != nil {
		return Provider{}, err
	}
	pr.CreatedAt = time.Now().Unix()
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.providerByID[pr.ID]; exists {
		pr.ID = pr.ID + "-" + randomHex(2)
	}
	enInt := 0
	if pr.Enabled {
		enInt = 1
	}
	if _, err := p.db.Exec(
		`INSERT INTO providers(id,name,category,api_type,auth_mode,base_url,default_model,icon,enabled,weight,priority,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		pr.ID, pr.Name, pr.Category, pr.APIType, pr.AuthMode, pr.BaseURL, pr.DefaultModel, pr.Icon, enInt, pr.Weight, pr.Priority, pr.CreatedAt,
	); err != nil {
		return Provider{}, err
	}
	cp := pr
	p.providers = append(p.providers, &cp)
	sort.SliceStable(p.providers, func(i, j int) bool {
		if p.providers[i].Priority == p.providers[j].Priority {
			return p.providers[i].CreatedAt < p.providers[j].CreatedAt
		}
		return p.providers[i].Priority < p.providers[j].Priority
	})
	p.providerByID[cp.ID] = &cp
	return cp, nil
}

func (p *KeyPool) UpdateProvider(id string, next Provider) (Provider, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cur, ok := p.providerByID[id]
	if !ok {
		return Provider{}, false, nil
	}
	next.ID = id
	next.CreatedAt = cur.CreatedAt
	if err := normalizeProvider(&next); err != nil {
		return Provider{}, true, err
	}
	enInt := 0
	if next.Enabled {
		enInt = 1
	}
	if _, err := p.db.Exec(
		`UPDATE providers SET name=?,category=?,api_type=?,auth_mode=?,base_url=?,default_model=?,icon=?,enabled=?,weight=?,priority=? WHERE id=?`,
		next.Name, next.Category, next.APIType, next.AuthMode, next.BaseURL, next.DefaultModel, next.Icon, enInt, next.Weight, next.Priority, id,
	); err != nil {
		return Provider{}, true, err
	}
	*cur = next
	sort.SliceStable(p.providers, func(i, j int) bool {
		if p.providers[i].Priority == p.providers[j].Priority {
			return p.providers[i].CreatedAt < p.providers[j].CreatedAt
		}
		return p.providers[i].Priority < p.providers[j].Priority
	})
	return *cur, true, nil
}

func (p *KeyPool) DeleteProvider(id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.providerByID[id]; !ok {
		return false, nil
	}
	tx, err := p.db.Begin()
	if err != nil {
		return true, err
	}
	_, _ = tx.Exec(`DELETE FROM providers WHERE id=?`, id)
	_, _ = tx.Exec(`DELETE FROM provider_keys WHERE provider=?`, id)
	if err := tx.Commit(); err != nil {
		return true, err
	}
	delete(p.providerByID, id)
	delete(p.swrrCurrent, id)
	var nextProvs []*Provider
	for _, pr := range p.providers {
		if pr.ID != id {
			nextProvs = append(nextProvs, pr)
		}
	}
	p.providers = nextProvs
	var nextKeys []*KeyEntry
	for _, k := range p.keys {
		if k.Provider == id {
			delete(p.byID, k.ID)
			continue
		}
		nextKeys = append(nextKeys, k)
	}
	p.keys = nextKeys
	return true, nil
}

func (p *KeyPool) Settings() RouterSettings {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.settings
}

func (p *KeyPool) eligibleCountLocked(provider string, now int64) int {
	n := 0
	for _, k := range p.keys {
		if k.Provider == provider && k.Stat.Enabled && k.Stat.Health != "dead" && k.Stat.CooldownUntil <= now {
			n++
		}
	}
	return n
}

func (p *KeyPool) ProviderOrder(requestedModel, scope string) []*Provider {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.settings

	catFilter := ""
	if strings.HasPrefix(scope, scopeCategoryPrefix) {
		catFilter = normalizeCategory(strings.TrimPrefix(scope, scopeCategoryPrefix))
	} else if scope != "" && scope != scopeGlobal {
		pr, ok := p.providerByID[scope]
		if !ok || !pr.Enabled {
			return nil
		}
		cp := *pr
		return []*Provider{&cp}
	}

	now := time.Now().Unix()
	var enabled []*Provider
	var ready []*Provider
	for _, pr := range p.providers {
		if !pr.Enabled {
			continue
		}
		if catFilter != "" && normalizeCategory(pr.Category) != catFilter {
			continue
		}
		cp := *pr
		enabled = append(enabled, &cp)
		if p.eligibleCountLocked(pr.ID, now) > 0 {
			ready = append(ready, &cp)
		}
	}
	if len(enabled) == 0 {
		return nil
	}

	candidates := ready
	if len(candidates) == 0 {
		candidates = enabled
	}

	lower := strings.ToLower(strings.TrimSpace(requestedModel))
	isAuto := lower == "" || lower == "auto" || lower == "router"

	appendFallbacks := func(first *Provider) []*Provider {
		out := []*Provider{first}
		if !s.CrossFallback {
			return out
		}
		for _, pr := range candidates {
			if pr.ID != first.ID {
				out = append(out, pr)
			}
		}
		for _, pr := range enabled {
			seen := false
			for _, ex := range out {
				if ex.ID == pr.ID {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, pr)
			}
		}
		return out
	}

	if s.ModelRoutingPolicy == "auto_or_override" && !isAuto {
		var matched *Provider
		for _, pr := range candidates {
			if lower == pr.ID || strings.HasPrefix(lower, pr.ID+"/") || strings.HasPrefix(lower, pr.ID+"-") || strings.EqualFold(lower, pr.DefaultModel) {
				matched = pr
				break
			}
		}
		if matched == nil && s.RotationMode == "model_prefix" {
			for _, pr := range enabled {
				if lower == pr.ID || strings.HasPrefix(lower, pr.ID+"/") || strings.HasPrefix(lower, pr.ID+"-") || strings.EqualFold(lower, pr.DefaultModel) {
					matched = pr
					break
				}
			}
		}
		if matched != nil {
			return appendFallbacks(matched)
		}
	}

	if s.RotationMode == "priority" {
		sort.SliceStable(candidates, func(i, j int) bool {
			return candidates[i].Priority < candidates[j].Priority
		})
		return appendFallbacks(candidates[0])
	}

	totalWeight := 0
	var best *Provider
	bestScore := -1 << 30
	for _, pr := range candidates {
		w := pr.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
		cur := p.swrrCurrent[pr.ID] + w
		p.swrrCurrent[pr.ID] = cur
		if best == nil || cur > bestScore {
			best = pr
			bestScore = cur
		}
	}
	if best != nil {
		p.swrrCurrent[best.ID] -= totalWeight
		return appendFallbacks(best)
	}
	return enabled
}

func (p *KeyPool) SelectKey(provider string, tried map[string]bool) *KeyEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().Unix()
	mode := p.settings.KeySelection

	var tier0 []*KeyEntry
	var tier1 []*KeyEntry
	for _, k := range p.keys {
		if k.Provider != provider || !k.Stat.Enabled || k.Stat.Health == "dead" || k.Stat.CooldownUntil > now || tried[k.ID] {
			continue
		}
		if k.Stat.ConsecFails > 0 || k.Stat.LastErrorCode != "" {
			tier1 = append(tier1, k)
		} else {
			tier0 = append(tier0, k)
		}
	}

	pool := tier0
	if len(pool) == 0 {
		pool = tier1
		if len(pool) > 1 {
			sort.SliceStable(pool, func(i, j int) bool {
				if pool[i].Stat.ConsecFails != pool[j].Stat.ConsecFails {
					return pool[i].Stat.ConsecFails < pool[j].Stat.ConsecFails
				}
				return pool[i].LastPickedNano < pool[j].LastPickedNano
			})
		}
	}
	if len(pool) == 0 {
		return nil
	}

	var chosen *KeyEntry
	switch mode {
	case "random":
		if len(tier0) > 0 {
			chosen = tier0[mathrand.Intn(len(tier0))]
		} else {
			bestFails := pool[0].Stat.ConsecFails
			end := 1
			for end < len(pool) && pool[end].Stat.ConsecFails == bestFails {
				end++
			}
			chosen = pool[mathrand.Intn(end)]
		}
	case "round_robin":
		sort.SliceStable(pool, func(i, j int) bool {
			if pool[i].Stat.ConsecFails != pool[j].Stat.ConsecFails {
				return pool[i].Stat.ConsecFails < pool[j].Stat.ConsecFails
			}
			return pool[i].ProviderIndex < pool[j].ProviderIndex
		})
		start := p.rrCursor[provider] % len(pool)
		chosen = pool[start]
		p.rrCursor[provider] = (start + 1) % len(pool)
	case "smart":
		if len(tier0) > 0 {
			sort.SliceStable(pool, func(i, j int) bool {
				ti := pool[i].LastPickedNano
				tj := pool[j].LastPickedNano
				diff := ti - tj
				if diff < 0 {
					diff = -diff
				}
				if diff > int64(2*time.Second) {
					return ti < tj
				}
				li := pool[i].Stat.LastLatencyMs
				lj := pool[j].Stat.LastLatencyMs
				if li > 0 && lj > 0 && li != lj {
					return li < lj
				}
				return ti < tj
			})
			topN := 2
			if len(pool) < topN {
				topN = len(pool)
			}
			chosen = pool[mathrand.Intn(topN)]
		} else {
			chosen = pool[0]
		}
	default:
		var top [4]*KeyEntry
		topLen := 0
		for _, cand := range pool {
			if len(tier0) == 0 && cand.Stat.ConsecFails > pool[0].Stat.ConsecFails {
				continue
			}
			if topLen < 4 {
				top[topLen] = cand
				pos := topLen
				topLen++
				for pos > 0 && top[pos].LastPickedNano < top[pos-1].LastPickedNano {
					top[pos], top[pos-1] = top[pos-1], top[pos]
					pos--
				}
			} else if cand.LastPickedNano < top[3].LastPickedNano {
				top[3] = cand
				pos := 3
				for pos > 0 && top[pos].LastPickedNano < top[pos-1].LastPickedNano {
					top[pos], top[pos-1] = top[pos-1], top[pos]
					pos--
				}
			}
		}
		chosen = top[mathrand.Intn(topLen)]
	}

	chosen.LastPickedNano = time.Now().UnixNano()
	return chosen
}

func (p *KeyPool) IncProviderReq(providerID string) {
	p.mu.Lock()
	p.providerReqs[providerID]++
	p.mu.Unlock()
}

func (p *KeyPool) Mark200(id string, latencyMs, routeUs int64, tokens int) {
	p.mu.Lock()
	if k, ok := p.byID[id]; ok {
		k.Stat.OkCount++
		k.Stat.LastUsedUnix = time.Now().Unix()
		k.Stat.CooldownUntil = 0
		k.Stat.ConsecFails = 0
		k.Stat.Health = "healthy"
		k.Stat.LastLatencyMs = latencyMs
		k.Stat.TotalTokens += int64(tokens)
		k.Stat.LastErrorCode = ""
		k.Stat.LastError = ""
	}
	p.latencySum += latencyMs
	p.latencyCount++
	p.routeUsSum += routeUs
	p.routeUsCount++
	p.mu.Unlock()
	p.enqueueWrite(writeEvent{providerKeyID: id})
}

func (p *KeyPool) MarkFail(id string, status int, retryAfter int64, errMsg string) (deadNow bool, errCode string, fullMsg string) {
	p.mu.Lock()
	k, ok := p.byID[id]
	if !ok {
		p.mu.Unlock()
		return false, "upstream_error", sanitizeError(errMsg)
	}
	cleanRaw := sanitizeError(errMsg, k.Key)
	code, label, detail := classifyUpstreamError(status, cleanRaw)
	if detail == "" {
		detail = label
	}
	k.Stat.LastUsedUnix = time.Now().Unix()
	k.Stat.ConsecFails++
	k.Stat.LastErrorCode = code

	switch code {
	case "rate_limit":
		k.Stat.Fail429++
	case "invalid_key", "credit_exhausted":
		k.Stat.FailAuth++
	default:
		k.Stat.FailOther++
	}

	if k.Stat.ConsecFails >= maxKeyConsecFails {
		k.Stat.Health = "dead"
		k.Stat.CooldownUntil = 0
		deadNow = true
		k.Stat.LastError = sanitizeError(fmt.Sprintf("Mati (%d/%d) · %s (%d): %s", k.Stat.ConsecFails, maxKeyConsecFails, label, status, detail), k.Key)
	} else {
		var cooldownSec int64 = 15
		if code == "rate_limit" {
			shift := k.Stat.Fail429 - 1
			if shift < 0 {
				shift = 0
			}
			if shift > 6 {
				shift = 6
			}
			base := int64(p.settings.CooldownBaseSec)
			if base <= 0 {
				base = 30
			}
			cooldownSec = base << uint(shift)
			if retryAfter > cooldownSec {
				cooldownSec = retryAfter
			}
			if cooldownSec > 3600 {
				cooldownSec = 3600
			}
		} else if code == "invalid_key" || code == "credit_exhausted" {
			cooldownSec = 20
		}
		k.Stat.CooldownUntil = time.Now().Unix() + cooldownSec
		k.Stat.LastError = sanitizeError(fmt.Sprintf("Gagal %d/%d · %s (%d): %s", k.Stat.ConsecFails, maxKeyConsecFails, label, status, detail), k.Key)
	}
	fullMsg = k.Stat.LastError
	prov := k.Provider
	idx := k.ProviderIndex
	consec := k.Stat.ConsecFails
	p.mu.Unlock()
	p.enqueueWrite(writeEvent{providerKeyID: id})
	log.Printf("[%s key #%d] fail=%d/%d code=%s status=%d dead=%v", prov, idx, consec, maxKeyConsecFails, code, status, deadNow)
	return deadNow, code, fullMsg
}

func (p *KeyPool) RecordLog(entry RequestLog) {
	p.mu.Lock()
	p.nextLogID++
	entry.ID = p.nextLogID
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().Unix()
	}
	p.logs[p.logHead] = entry
	p.logHead = (p.logHead + 1) % maxLogEntries
	if p.logCount < maxLogEntries {
		p.logCount++
	}
	p.mu.Unlock()
	cp := entry
	p.enqueueWrite(writeEvent{logEntry: &cp})
}

func (p *KeyPool) RecentLogs() []RequestLog {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]RequestLog, 0, p.logCount)
	for i := 0; i < p.logCount; i++ {
		idx := (p.logHead - 1 - i + maxLogEntries) % maxLogEntries
		out = append(out, p.logs[idx])
	}
	return out
}

func keyViewFromEntry(k *KeyEntry, now int64) KeyView {
	status := "healthy"
	var rem int64
	if !k.Stat.Enabled {
		status = "paused"
	} else if k.Stat.Health == "dead" {
		status = "dead"
	} else if k.Stat.CooldownUntil > now {
		status = "cooldown"
		rem = k.Stat.CooldownUntil - now
	}
	return KeyView{
		ID:                   k.ID,
		Index:                k.ProviderIndex,
		Provider:             k.Provider,
		Label:                k.Stat.Label,
		Masked:               k.Masked,
		Enabled:              k.Stat.Enabled,
		Status:               status,
		Health:               k.Stat.Health,
		CooldownUntil:        k.Stat.CooldownUntil,
		CooldownRemainingSec: rem,
		LastUsedUnix:         k.Stat.LastUsedUnix,
		OkCount:              k.Stat.OkCount,
		Fail429:              k.Stat.Fail429,
		FailAuth:             k.Stat.FailAuth,
		FailOther:            k.Stat.FailOther,
		ConsecFails:          k.Stat.ConsecFails,
		TotalTokens:          k.Stat.TotalTokens,
		LastLatencyMs:        k.Stat.LastLatencyMs,
		LastErrorCode:        k.Stat.LastErrorCode,
		LastError:            k.Stat.LastError,
	}
}

func keyOrderRank(k *KeyEntry, now int64) int {
	if !k.Stat.Enabled {
		return 3
	}
	if k.Stat.Health == "dead" {
		return 4
	}
	if k.Stat.CooldownUntil > now {
		return 2
	}
	if k.Stat.ConsecFails > 0 || k.Stat.LastErrorCode != "" {
		return 1
	}
	return 0
}

func (p *KeyPool) KeyViews() []KeyView {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now().Unix()
	ordered := make([]*KeyEntry, len(p.keys))
	copy(ordered, p.keys)
	sort.SliceStable(ordered, func(i, j int) bool {
		ri := keyOrderRank(ordered[i], now)
		rj := keyOrderRank(ordered[j], now)
		if ri != rj {
			return ri < rj
		}
		if ordered[i].Stat.ConsecFails != ordered[j].Stat.ConsecFails {
			return ordered[i].Stat.ConsecFails < ordered[j].Stat.ConsecFails
		}
		return ordered[i].ProviderIndex < ordered[j].ProviderIndex
	})
	out := make([]KeyView, 0, len(ordered))
	for _, k := range ordered {
		out = append(out, keyViewFromEntry(k, now))
	}
	return out
}

func (p *KeyPool) GetKeySnapshot(id string) (KeyEntry, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	k, ok := p.byID[id]
	if !ok {
		return KeyEntry{}, false
	}
	return *k, true
}

func (p *KeyPool) HealthSummary() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now().Unix()
	total, healthy, eligible := 0, 0, 0
	for _, k := range p.keys {
		total++
		if k.Stat.Health != "dead" {
			healthy++
			if k.Stat.Enabled && k.Stat.CooldownUntil <= now {
				eligible++
			}
		}
	}
	return map[string]any{
		"total":       total,
		"healthy":     healthy,
		"eligible":    eligible,
		"providers":   len(p.providers),
		"client_keys": len(p.clients),
	}
}

func (p *KeyPool) AvgStats() (avgLatencyMs, avgRouteUs int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.latencyCount > 0 {
		avgLatencyMs = p.latencySum / p.latencyCount
	}
	if p.routeUsCount > 0 {
		avgRouteUs = p.routeUsSum / p.routeUsCount
	}
	return
}

func (p *KeyPool) UpdateSettings(next RouterSettings) (RouterSettings, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	normalizeSettings(&next)
	b, _ := json.Marshal(next)
	if err := p.setMeta("router_settings", string(b)); err != nil {
		return p.settings, err
	}
	p.settings = next
	return p.settings, nil
}
