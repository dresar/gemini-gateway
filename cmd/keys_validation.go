package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type KeyValidationIssue struct {
	Index  int    `json:"index"`
	Masked string `json:"masked"`
	Label  string `json:"label,omitempty"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

type KeyAuditDuplicateGroup struct {
	Provider     string   `json:"provider"`
	Masked       string   `json:"masked"`
	Count        int      `json:"count"`
	KeepID       string   `json:"keep_id"`
	KeepLabel    string   `json:"keep_label"`
	DuplicateIDs []string `json:"duplicate_ids"`
}

type KeyAuditCrossConflict struct {
	Masked    string   `json:"masked"`
	Providers []string `json:"providers"`
	Labels    []string `json:"labels"`
}

type KeyAuditInvalidItem struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Masked   string `json:"masked"`
	Code     string `json:"code"`
	Reason   string `json:"reason"`
}

type KeyAuditReport struct {
	OK              bool                     `json:"ok"`
	TotalKeys       int                      `json:"total_keys"`
	HealthyCount    int                      `json:"healthy_count"`
	ExactDuplicates int                      `json:"exact_duplicates"`
	CrossDuplicates int                      `json:"cross_duplicates"`
	InvalidCount    int                      `json:"invalid_count"`
	WarningCount    int                      `json:"warning_count"`
	DuplicateGroups []KeyAuditDuplicateGroup `json:"duplicate_groups"`
	CrossConflicts  []KeyAuditCrossConflict  `json:"cross_conflicts"`
	InvalidItems    []KeyAuditInvalidItem    `json:"invalid_items"`
	WarningItems    []KeyAuditInvalidItem    `json:"warning_items"`
}

type KeyImportResult struct {
	Added           int                  `json:"added"`
	Duplicates      int                  `json:"duplicates"`
	PoolDuplicates  int                  `json:"pool_duplicates"`
	BatchDuplicates int                  `json:"batch_duplicates"`
	Invalid         int                  `json:"invalid"`
	CrossDuplicates int                  `json:"cross_duplicates"`
	Total           int                  `json:"total_provider_keys"`
	InvalidItems    []KeyValidationIssue `json:"invalid_items,omitempty"`
	Warnings        []string             `json:"warnings,omitempty"`
}

type KeyValidatePreviewItem struct {
	Index      int    `json:"index"`
	Masked     string `json:"masked"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Code       string `json:"code,omitempty"`
	Reason     string `json:"reason,omitempty"`
	WarnCode   string `json:"warn_code,omitempty"`
	WarnReason string `json:"warn_reason,omitempty"`
}

func (p *KeyPool) GetMetaValue(k string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.getMeta(k)
	if !ok {
		return ""
	}
	return v
}

func (p *KeyPool) SetMetaValue(k, v string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.setMeta(k, v)
}

func normalizeRawKey(raw string) string {
	r := strings.NewReplacer(
		"\u200b", "",
		"\u200c", "",
		"\u200d", "",
		"\ufeff", "",
		"\u00a0", " ",
	).Replace(raw)
	clean := strings.TrimSpace(r)
	clean = strings.Trim(clean, "\"'`,;")
	clean = strings.TrimSpace(clean)
	if len(clean) > 7 && strings.EqualFold(clean[:7], "bearer ") {
		clean = strings.TrimSpace(clean[7:])
	}
	return clean
}

func isPlaceholderKey(clean string) bool {
	lower := strings.ToLower(clean)
	patterns := []string{
		"your_api_key",
		"your-api-key",
		"masukkan_",
		"api_key_here",
		"replace_me",
		"changeme",
		"placeholder",
		"example_key",
		"sample_key",
		"sk-xxx",
		"sk-your",
		"aiza_your",
		"<api_key>",
		"<your_",
	}
	for _, pat := range patterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	exact := map[string]bool{
		"undefined": true,
		"null":      true,
		"none":      true,
		"bearer":    true,
		"apikey":    true,
		"api_key":   true,
		"test":      true,
		"demo":      true,
	}
	if exact[lower] {
		return true
	}
	stripped := strings.TrimPrefix(lower, "sk-")
	stripped = strings.TrimPrefix(stripped, "aiza")
	if len(stripped) >= 6 {
		uniq := make(map[rune]bool, 8)
		for _, ch := range stripped {
			if ch != '-' && ch != '_' {
				uniq[ch] = true
			}
		}
		if len(uniq) <= 2 {
			return true
		}
	}
	return false
}

func validateKeyCandidate(prov Provider, raw string) (clean, errCode, errReason, warnCode, warnReason string) {
	clean = normalizeRawKey(raw)
	if clean == "" {
		return "", "empty", "Key kosong", "", ""
	}
	if strings.ContainsAny(clean, " \t\r\n") {
		return clean, "whitespace_inside", "Mengandung spasi atau tab di tengah key", "", ""
	}
	for _, ch := range clean {
		if ch < 0x21 || ch > 0x7e {
			return clean, "invalid_chars", "Mengandung karakter kontrol atau non-ASCII", "", ""
		}
	}
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.Contains(lower, "://") {
		return clean, "url_not_key", "Terlihat seperti URL endpoint, bukan API key", "", ""
	}
	if strings.ContainsAny(clean, "{}[]") || strings.Contains(clean, "\":") {
		return clean, "json_fragment", "Potongan format JSON/objek, bukan API key mentah", "", ""
	}
	if isPlaceholderKey(clean) {
		return clean, "placeholder_key", "Placeholder / key contoh terdeteksi", "", ""
	}
	if len(clean) < 8 {
		return clean, "too_short", "Panjang key terlalu pendek (< 8 karakter)", "", ""
	}
	if len(clean) > 512 {
		return clean, "too_long", "Panjang key terlalu panjang (> 512 karakter)", "", ""
	}
	if prov.APIType == apiTypeGemini {
		if strings.HasPrefix(clean, "sk-") {
			return clean, "provider_mismatch", "Format sk- milik OpenAI/Agnes, bukan key Gemini", "", ""
		}
		if strings.HasPrefix(clean, "AIza") && len(clean) < 14 {
			return clean, "truncated_key", "Key Gemini AIza terpotong (< 14 karakter)", "", ""
		}
		if strings.HasPrefix(clean, "AIza") && len(clean) < 35 {
			warnCode = "short_aiza"
			warnReason = "Panjang key AIza umumnya 39 karakter (terlihat lebih pendek)"
		}
	} else {
		if prov.ID == "agnes" && strings.HasPrefix(clean, "sk-") && len(clean) < 20 {
			return clean, "truncated_key", "Key Agnes sk- terpotong (< 20 karakter)", "", ""
		}
		if strings.HasPrefix(clean, "AIza") && !strings.Contains(strings.ToLower(prov.BaseURL), "googleapis.com") {
			warnCode = "provider_mismatch_warn"
			warnReason = "Key diawali AIza (umumnya Google Gemini) di endpoint OpenAI-compat"
		} else if prov.ID == "groq" && !strings.HasPrefix(clean, "gsk_") {
			warnCode = "prefix_unusual"
			warnReason = "Key Groq umumnya diawali gsk_"
		} else if prov.ID == "openrouter" && !strings.HasPrefix(clean, "sk-or-") {
			warnCode = "prefix_unusual"
			warnReason = "Key OpenRouter umumnya diawali sk-or-"
		}
	}
	return clean, "", "", warnCode, warnReason
}

func (p *KeyPool) AddKeyItemsValidated(provider string, items []KeyItemInput, labelPrefix string) (KeyImportResult, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	var res KeyImportResult
	if provider == "" {
		return res, errors.New("provider required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	provPtr, ok := p.providerByID[provider]
	if !ok {
		return res, fmt.Errorf("provider %s not found", provider)
	}
	prov := *provPtr
	prefix := cleanKeyLabelPrefix(labelPrefix, provider)

	inPool := map[string]bool{}
	otherPool := map[string]string{}
	usedLabels := map[string]bool{}
	provCount := 0
	for _, k := range p.keys {
		norm := normalizeRawKey(k.Key)
		if k.Provider == provider {
			inPool[k.Key] = true
			if norm != "" {
				inPool[norm] = true
			}
			usedLabels[strings.ToLower(k.Stat.Label)] = true
			provCount++
		} else if norm != "" {
			if _, exists := otherPool[norm]; !exists {
				otherPool[norm] = fmt.Sprintf("%s (%s)", k.Provider, k.Stat.Label)
			}
		}
	}

	seq := nextKeySeqStart(p.keys, provider, prefix)
	now := time.Now().Unix()
	tx, err := p.db.Begin()
	if err != nil {
		return res, err
	}

	batchSeen := map[string]bool{}
	var addedEntries []*KeyEntry
	var skippedCommentOrEmpty int
	idx := 0

	for _, item := range items {
		rawTrim := strings.TrimSpace(item.Key)
		if rawTrim == "" || strings.HasPrefix(rawTrim, "#") || strings.HasPrefix(rawTrim, "//") {
			skippedCommentOrEmpty++
			continue
		}
		idx++
		clean, errCode, errReason, warnCode, warnReason := validateKeyCandidate(prov, rawTrim)
		if errCode != "" {
			res.Invalid++
			if len(res.InvalidItems) < 25 {
				res.InvalidItems = append(res.InvalidItems, KeyValidationIssue{
					Index:  idx,
					Masked: maskKey(rawTrim),
					Label:  strings.TrimSpace(item.Label),
					Code:   errCode,
					Reason: errReason,
				})
			}
			continue
		}
		if batchSeen[clean] {
			res.BatchDuplicates++
			res.Duplicates++
			continue
		}
		if inPool[clean] {
			res.PoolDuplicates++
			res.Duplicates++
			continue
		}
		batchSeen[clean] = true

		lbl := strings.TrimSpace(item.Label)
		if lbl == "" {
			for {
				lbl = fmt.Sprintf("%s%02d", prefix, seq)
				seq++
				if !usedLabels[strings.ToLower(lbl)] {
					break
				}
			}
		}
		usedLabels[strings.ToLower(lbl)] = true

		if conflictProv, exists := otherPool[clean]; exists {
			res.CrossDuplicates++
			if len(res.Warnings) < 10 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s (%s) juga aktif di %s", lbl, maskKey(clean), conflictProv))
			}
		} else if warnCode != "" && len(res.Warnings) < 10 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", lbl, warnReason))
		}

		id := makeKeyID(provider, clean)
		masked := maskKey(clean)
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO provider_keys(id,provider,api_key,masked,label,enabled,health,cooldown_until,last_used_unix,ok_count,fail_429,fail_auth,fail_other,consec_fails,total_tokens,last_latency_ms,last_error_code,last_error,created_at) VALUES(?,?,?,?,?,1,'healthy',0,0,0,0,0,0,0,0,0,'','',?)`,
			id, provider, clean, masked, lbl, now,
		); err != nil {
			_ = tx.Rollback()
			res.Total = provCount - res.Added
			return res, err
		}
		entry := &KeyEntry{
			ID:            id,
			Provider:      provider,
			ProviderIndex: provCount,
			Key:           clean,
			Masked:        masked,
			CreatedAt:     now,
			Stat: KeyStat{
				Label:   lbl,
				Enabled: true,
				Health:  "healthy",
			},
		}
		addedEntries = append(addedEntries, entry)
		provCount++
		res.Added++
	}

	if err := tx.Commit(); err != nil {
		res.Added = 0
		res.Total = provCount - len(addedEntries)
		return res, err
	}
	for _, e := range addedEntries {
		p.keys = append(p.keys, e)
		p.byID[e.ID] = e
	}
	res.Total = provCount
	_ = skippedCommentOrEmpty
	return res, nil
}

func keyKeepPriority(k *KeyEntry) int64 {
	var score int64
	if k.Stat.Enabled {
		score += 10000000
	}
	if k.Stat.Health != "dead" {
		score += 5000000
	}
	score -= int64(k.Stat.ConsecFails) * 100000
	score += int64(k.Stat.OkCount) * 1000
	score -= int64(k.Stat.FailAuth) * 50000
	if k.Stat.LastLatencyMs > 0 {
		latPenalty := k.Stat.LastLatencyMs
		if latPenalty > 5000 {
			latPenalty = 5000
		}
		score -= latPenalty
	}
	if k.Key == normalizeRawKey(k.Key) {
		score += 500
	}
	return score
}

func (p *KeyPool) ValidateKeyItems(provider string, items []KeyItemInput, labelPrefix string) (map[string]any, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	p.mu.RLock()
	defer p.mu.RUnlock()

	provPtr, ok := p.providerByID[provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", provider)
	}
	prov := *provPtr
	prefix := cleanKeyLabelPrefix(labelPrefix, provider)
	inPool := map[string]bool{}
	otherPool := map[string]string{}
	for _, k := range p.keys {
		norm := normalizeRawKey(k.Key)
		if norm == "" {
			continue
		}
		if k.Provider == provider {
			inPool[norm] = true
		} else if otherPool[norm] == "" {
			otherPool[norm] = fmt.Sprintf("%s (%s)", k.Provider, k.Stat.Label)
		}
	}
	seq := nextKeySeqStart(p.keys, provider, prefix)
	batchSeen := map[string]bool{}
	results := make([]KeyValidatePreviewItem, 0, len(items))
	var validCount, poolDupCount, batchDupCount, invalidCount, crossCount int

	for _, item := range items {
		rawTrim := strings.TrimSpace(item.Key)
		if rawTrim == "" || strings.HasPrefix(rawTrim, "#") || strings.HasPrefix(rawTrim, "//") {
			continue
		}
		idx := len(results) + 1
		lbl := strings.TrimSpace(item.Label)
		clean, errCode, errReason, warnCode, warnReason := validateKeyCandidate(prov, rawTrim)
		if errCode != "" {
			invalidCount++
			results = append(results, KeyValidatePreviewItem{
				Index:  idx,
				Masked: maskKey(rawTrim),
				Label:  lbl,
				Status: "invalid",
				Code:   errCode,
				Reason: errReason,
			})
			continue
		}
		if inPool[clean] {
			poolDupCount++
			results = append(results, KeyValidatePreviewItem{
				Index:  idx,
				Masked: maskKey(clean),
				Label:  lbl,
				Status: "pool_duplicate",
				Code:   "pool_duplicate",
				Reason: "Sudah terdaftar di pool provider ini",
			})
			continue
		}
		if batchSeen[clean] {
			batchDupCount++
			results = append(results, KeyValidatePreviewItem{
				Index:  idx,
				Masked: maskKey(clean),
				Label:  lbl,
				Status: "batch_duplicate",
				Code:   "batch_duplicate",
				Reason: "Duplikat di daftar input ini",
			})
			continue
		}
		batchSeen[clean] = true
		if lbl == "" {
			lbl = fmt.Sprintf("%s%02d", prefix, seq)
			seq++
		}
		validCount++
		entry := KeyValidatePreviewItem{
			Index:      idx,
			Masked:     maskKey(clean),
			Label:      lbl,
			Status:     "valid",
			WarnCode:   warnCode,
			WarnReason: warnReason,
		}
		if conflict, exists := otherPool[clean]; exists {
			crossCount++
			entry.WarnCode = "cross_duplicate"
			entry.WarnReason = "Juga ada di " + conflict
		}
		results = append(results, entry)
	}
	return map[string]any{
		"ok":               true,
		"total_parsed":     len(results),
		"valid":            validCount,
		"pool_duplicates":  poolDupCount,
		"batch_duplicates": batchDupCount,
		"invalid":          invalidCount,
		"cross_duplicates": crossCount,
		"items":            results,
	}, nil
}

func (p *KeyPool) AuditKeys(providerFilter string) KeyAuditReport {
	p.mu.RLock()
	defer p.mu.RUnlock()

	filter := strings.TrimSpace(providerFilter)
	matchProv := func(pid string) bool {
		return filter == "" || filter == "all" || pid == filter
	}

	report := KeyAuditReport{
		OK:              true,
		DuplicateGroups: []KeyAuditDuplicateGroup{},
		CrossConflicts:  []KeyAuditCrossConflict{},
		InvalidItems:    []KeyAuditInvalidItem{},
		WarningItems:    []KeyAuditInvalidItem{},
	}

	byProvNorm := map[string][]*KeyEntry{}
	byCrossNorm := map[string][]*KeyEntry{}

	for _, k := range p.keys {
		norm := normalizeRawKey(k.Key)
		if norm != "" {
			byCrossNorm[norm] = append(byCrossNorm[norm], k)
		}
		if !matchProv(k.Provider) {
			continue
		}
		report.TotalKeys++
		if k.Stat.Enabled && k.Stat.Health != "dead" {
			report.HealthyCount++
		}
		if norm != "" {
			gk := k.Provider + "\x00" + norm
			byProvNorm[gk] = append(byProvNorm[gk], k)
		}
		prov := Provider{ID: k.Provider, APIType: apiTypeOpenAI}
		if pPtr, ok := p.providerByID[k.Provider]; ok {
			prov = *pPtr
		}
		_, errCode, errReason, warnCode, warnReason := validateKeyCandidate(prov, k.Key)
		if errCode != "" {
			report.InvalidCount++
			report.InvalidItems = append(report.InvalidItems, KeyAuditInvalidItem{
				ID:       k.ID,
				Provider: k.Provider,
				Label:    k.Stat.Label,
				Masked:   k.Masked,
				Code:     errCode,
				Reason:   errReason,
			})
		} else if warnCode != "" {
			report.WarningCount++
			report.WarningItems = append(report.WarningItems, KeyAuditInvalidItem{
				ID:       k.ID,
				Provider: k.Provider,
				Label:    k.Stat.Label,
				Masked:   k.Masked,
				Code:     warnCode,
				Reason:   warnReason,
			})
		}
	}

	groupKeys := make([]string, 0, len(byProvNorm))
	for gk, list := range byProvNorm {
		if len(list) > 1 {
			groupKeys = append(groupKeys, gk)
		}
	}
	sort.Strings(groupKeys)
	for _, gk := range groupKeys {
		list := append([]*KeyEntry(nil), byProvNorm[gk]...)
		sort.SliceStable(list, func(i, j int) bool {
			return keyKeepPriority(list[i]) > keyKeepPriority(list[j])
		})
		keep := list[0]
		dupIDs := make([]string, 0, len(list)-1)
		for _, dup := range list[1:] {
			dupIDs = append(dupIDs, dup.ID)
			report.ExactDuplicates++
		}
		report.DuplicateGroups = append(report.DuplicateGroups, KeyAuditDuplicateGroup{
			Provider:     keep.Provider,
			Masked:       keep.Masked,
			Count:        len(list),
			KeepID:       keep.ID,
			KeepLabel:    keep.Stat.Label,
			DuplicateIDs: dupIDs,
		})
	}

	for _, list := range byCrossNorm {
		provSet := map[string]bool{}
		var provs, labels []string
		matchedFilter := false
		for _, k := range list {
			if matchProv(k.Provider) {
				matchedFilter = true
			}
			if !provSet[k.Provider] {
				provSet[k.Provider] = true
				provs = append(provs, k.Provider)
			}
			labels = append(labels, fmt.Sprintf("%s:%s", k.Provider, k.Stat.Label))
		}
		if matchedFilter && len(provSet) > 1 {
			report.CrossDuplicates++
			report.CrossConflicts = append(report.CrossConflicts, KeyAuditCrossConflict{
				Masked:    list[0].Masked,
				Providers: provs,
				Labels:    labels,
			})
		}
	}

	return report
}

func (p *KeyPool) dedupeKeysLocked(providerFilter string) (int, error) {
	filter := strings.TrimSpace(providerFilter)
	matchProv := func(pid string) bool {
		return filter == "" || filter == "all" || pid == filter
	}
	byGroup := map[string][]*KeyEntry{}
	for _, k := range p.keys {
		if !matchProv(k.Provider) {
			continue
		}
		norm := normalizeRawKey(k.Key)
		if norm == "" {
			continue
		}
		gk := k.Provider + "\x00" + norm
		byGroup[gk] = append(byGroup[gk], k)
	}
	toDelete := map[string]bool{}
	for _, list := range byGroup {
		if len(list) <= 1 {
			continue
		}
		sorted := append([]*KeyEntry(nil), list...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return keyKeepPriority(sorted[i]) > keyKeepPriority(sorted[j])
		})
		for _, dup := range sorted[1:] {
			toDelete[dup.ID] = true
		}
	}
	if len(toDelete) == 0 {
		return 0, nil
	}
	tx, err := p.db.Begin()
	if err != nil {
		return 0, err
	}
	affected := 0
	kept := make([]*KeyEntry, 0, len(p.keys))
	for _, k := range p.keys {
		if toDelete[k.ID] {
			delete(p.byID, k.ID)
			_, _ = tx.Exec(`DELETE FROM provider_keys WHERE id=?`, k.ID)
			affected++
			continue
		}
		kept = append(kept, k)
	}
	p.keys = kept
	p.reindexProvidersLocked()
	return affected, tx.Commit()
}

func (p *KeyPool) purgeInvalidKeysLocked(providerFilter string) (int, error) {
	filter := strings.TrimSpace(providerFilter)
	matchProv := func(pid string) bool {
		return filter == "" || filter == "all" || pid == filter
	}
	tx, err := p.db.Begin()
	if err != nil {
		return 0, err
	}
	affected := 0
	kept := make([]*KeyEntry, 0, len(p.keys))
	for _, k := range p.keys {
		if matchProv(k.Provider) {
			prov := Provider{ID: k.Provider, APIType: apiTypeOpenAI}
			if pPtr, ok := p.providerByID[k.Provider]; ok {
				prov = *pPtr
			}
			_, errCode, _, _, _ := validateKeyCandidate(prov, k.Key)
			if errCode != "" {
				delete(p.byID, k.ID)
				_, _ = tx.Exec(`DELETE FROM provider_keys WHERE id=?`, k.ID)
				affected++
				continue
			}
		}
		kept = append(kept, k)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return 0, nil
	}
	p.keys = kept
	p.reindexProvidersLocked()
	return affected, tx.Commit()
}

func (g *Gateway) handleKeysAudit(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	switch r.Method {
	case http.MethodGet:
		prov := strings.TrimSpace(r.URL.Query().Get("provider"))
		writeJSON(w, http.StatusOK, g.pool.AuditKeys(prov))
	case http.MethodPost:
		var body struct {
			Action      string         `json:"action"`
			Provider    string         `json:"provider"`
			Keys        []string       `json:"keys"`
			RawText     string         `json:"raw_text"`
			LabelPrefix string         `json:"label_prefix"`
			Items       []KeyItemInput `json:"items"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		if strings.TrimSpace(body.Action) == "validate" {
			items := parseKeyItemLines(body.RawText, body.Keys, body.Items)
			res, err := g.pool.ValidateKeyItems(body.Provider, items, body.LabelPrefix)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
				return
			}
			writeJSON(w, http.StatusOK, res)
			return
		}
		action := strings.TrimSpace(body.Action)
		if action == "" {
			action = "auto_clean"
		}
		affected, err := g.pool.BulkAction(action, body.Provider)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"affected": affected,
			"audit":    g.pool.AuditKeys(body.Provider),
		})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}
