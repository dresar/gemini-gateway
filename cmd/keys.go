package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func cleanKeyLabelPrefix(prefix, provider string) string {
	src := strings.TrimSpace(prefix)
	if src == "" {
		src = provider
	}
	src = strings.ToLower(src)
	var b strings.Builder
	for _, r := range src {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "key"
	}
	return out
}

func parseKeyItemLines(rawText string, rawKeys []string, items []KeyItemInput) []KeyItemInput {
	var out []KeyItemInput
	push := func(k, lbl string) {
		k = strings.TrimSpace(k)
		k = strings.Trim(k, `"'`+"`"+`,;`)
		if k == "" || strings.HasPrefix(k, "#") || strings.HasPrefix(k, "//") || k == "KEY" {
			return
		}
		out = append(out, KeyItemInput{Key: k, Label: strings.TrimSpace(lbl)})
	}

	for _, it := range items {
		push(it.Key, it.Label)
	}

	parseBlob := func(blob string) {
		trimmed := strings.TrimSpace(blob)
		if trimmed == "" {
			return
		}
		if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
			var strArr []string
			if json.Unmarshal([]byte(trimmed), &strArr) == nil && len(strArr) > 0 {
				for _, s := range strArr {
					push(s, "")
				}
				return
			}
			var objArr []KeyItemInput
			if json.Unmarshal([]byte(trimmed), &objArr) == nil && len(objArr) > 0 {
				for _, o := range objArr {
					push(o.Key, o.Label)
				}
				return
			}
		}
		for _, rawLine := range strings.Split(trimmed, "\n") {
			line := strings.TrimSpace(rawLine)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				var lineArr []string
				if json.Unmarshal([]byte(line), &lineArr) == nil && len(lineArr) > 0 {
					for _, s := range lineArr {
						push(s, "")
					}
					continue
				}
				var lineObjArr []KeyItemInput
				if json.Unmarshal([]byte(line), &lineObjArr) == nil && len(lineObjArr) > 0 {
					for _, o := range lineObjArr {
						push(o.Key, o.Label)
					}
					continue
				}
			}
			if strings.Contains(line, "=") && !strings.HasPrefix(strings.ToLower(line), "http") {
				eqIdx := strings.Index(line, "=")
				envVal := strings.TrimSpace(line[eqIdx+1:])
				if envVal != "" {
					push(envVal, "")
					continue
				}
			}
			if strings.Contains(line, ";") || strings.Count(line, ",") > 1 {
				fields := strings.FieldsFunc(line, func(r rune) bool {
					return r == ',' || r == ';'
				})
				for _, f := range fields {
					push(f, "")
				}
				continue
			}
			if strings.Contains(line, ",") {
				parts := strings.SplitN(line, ",", 2)
				p0 := strings.TrimSpace(parts[0])
				p1 := strings.TrimSpace(parts[1])
				if p0 != "" && p1 != "" {
					p0IsKey := len(p0) >= 16 || strings.HasPrefix(p0, "AIza") || strings.HasPrefix(p0, "sk-")
					p1IsKey := len(p1) >= 16 || strings.HasPrefix(p1, "AIza") || strings.HasPrefix(p1, "sk-")
					if !p0IsKey && p1IsKey {
						push(p1, p0)
					} else if p0IsKey && !p1IsKey {
						push(p0, p1)
					} else {
						push(p0, "")
						push(p1, "")
					}
					continue
				}
			}
			spaceTokens := strings.Fields(line)
			if len(spaceTokens) == 2 && strings.EqualFold(spaceTokens[0], "bearer") {
				push(spaceTokens[1], "")
				continue
			}
			if len(spaceTokens) > 1 {
				allKeys := true
				for _, tok := range spaceTokens {
					if len(tok) < 12 && !strings.HasPrefix(tok, "AIza") && !strings.HasPrefix(tok, "sk-") {
						allKeys = false
						break
					}
				}
				if allKeys {
					for _, tok := range spaceTokens {
						push(tok, "")
					}
					continue
				}
			}
			push(line, "")
		}
	}

	if strings.TrimSpace(rawText) != "" {
		parseBlob(rawText)
	}
	for _, rk := range rawKeys {
		parseBlob(rk)
	}
	return out
}

func nextKeySeqStart(keys []*KeyEntry, provider, prefix string) int {
	maxNum := 0
	lowerPref := strings.ToLower(prefix)
	for _, k := range keys {
		if k.Provider != provider {
			continue
		}
		lbl := strings.ToLower(strings.TrimSpace(k.Stat.Label))
		if !strings.HasPrefix(lbl, lowerPref) {
			continue
		}
		rest := strings.TrimLeft(lbl[len(lowerPref):], "#_- ")
		if rest != "" {
			if n, err := strconv.Atoi(rest); err == nil && n > maxNum {
				maxNum = n
			}
		}
	}
	return maxNum + 1
}

func (p *KeyPool) AddKeys(provider string, rawKeys []string, labelPrefix string) (added, duplicates, total int, err error) {
	items := parseKeyItemLines("", rawKeys, nil)
	return p.AddKeyItems(provider, items, labelPrefix)
}

func (p *KeyPool) AddKeyItems(provider string, items []KeyItemInput, labelPrefix string) (added, duplicates, total int, err error) {
	res, err := p.AddKeyItemsValidated(provider, items, labelPrefix)
	return res.Added, res.Duplicates, res.Total, err
}

func (p *KeyPool) PatchKey(id string, enabled *bool, label *string, action string) (KeyView, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k, ok := p.byID[id]
	if !ok {
		return KeyView{}, false, nil
	}
	nextStat := k.Stat
	if enabled != nil {
		nextStat.Enabled = *enabled
	}
	if label != nil && strings.TrimSpace(*label) != "" {
		nextStat.Label = strings.TrimSpace(*label)
	}
	if action == "revive" {
		nextStat.Health = "healthy"
		nextStat.CooldownUntil = 0
		nextStat.Fail429 = 0
		nextStat.FailAuth = 0
		nextStat.FailOther = 0
		nextStat.ConsecFails = 0
		nextStat.LastErrorCode = ""
		nextStat.LastError = ""
		nextStat.Enabled = true
	}
	enInt := 0
	if nextStat.Enabled {
		enInt = 1
	}
	if _, err := p.db.Exec(
		`UPDATE provider_keys SET label=?,enabled=?,health=?,cooldown_until=?,fail_429=?,fail_auth=?,fail_other=?,consec_fails=?,last_error_code=?,last_error=? WHERE id=?`,
		nextStat.Label, enInt, nextStat.Health, nextStat.CooldownUntil, nextStat.Fail429, nextStat.FailAuth, nextStat.FailOther, nextStat.ConsecFails, nextStat.LastErrorCode, nextStat.LastError, id,
	); err != nil {
		return KeyView{}, true, err
	}
	k.Stat = nextStat
	return keyViewFromEntry(k, time.Now().Unix()), true, nil
}

func (p *KeyPool) DeleteKey(id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.byID[id]; !ok {
		return false, nil
	}
	if _, err := p.db.Exec(`DELETE FROM provider_keys WHERE id=?`, id); err != nil {
		return true, err
	}
	delete(p.byID, id)
	next := make([]*KeyEntry, 0, len(p.keys)-1)
	for _, item := range p.keys {
		if item.ID != id {
			next = append(next, item)
		}
	}
	p.keys = next
	p.reindexProvidersLocked()
	return true, nil
}

func (p *KeyPool) BulkAction(action, providerFilter string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	matchProv := func(prov string) bool {
		return providerFilter == "" || providerFilter == "all" || prov == providerFilter
	}
	affected := 0
	switch action {
	case "revive_all":
		tx, err := p.db.Begin()
		if err != nil {
			return 0, err
		}
		for _, k := range p.keys {
			if matchProv(k.Provider) {
				if k.Stat.Health == "dead" || k.Stat.CooldownUntil > 0 || !k.Stat.Enabled || k.Stat.ConsecFails > 0 || k.Stat.LastError != "" {
					affected++
				}
				k.Stat.Health = "healthy"
				k.Stat.CooldownUntil = 0
				k.Stat.Fail429 = 0
				k.Stat.FailAuth = 0
				k.Stat.FailOther = 0
				k.Stat.ConsecFails = 0
				k.Stat.LastErrorCode = ""
				k.Stat.LastError = ""
				k.Stat.Enabled = true
				_, _ = tx.Exec(`UPDATE provider_keys SET enabled=1,health='healthy',cooldown_until=0,fail_429=0,fail_auth=0,fail_other=0,consec_fails=0,last_error_code='',last_error='' WHERE id=?`, k.ID)
			}
		}
		return affected, tx.Commit()
	case "purge_dead":
		tx, err := p.db.Begin()
		if err != nil {
			return 0, err
		}
		kept := make([]*KeyEntry, 0, len(p.keys))
		for _, k := range p.keys {
			if matchProv(k.Provider) && k.Stat.Health == "dead" {
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
	case "dedupe_all":
		return p.dedupeKeysLocked(providerFilter)
	case "purge_invalid":
		return p.purgeInvalidKeysLocked(providerFilter)
	case "auto_clean":
		deduped, err := p.dedupeKeysLocked(providerFilter)
		if err != nil {
			return deduped, err
		}
		purged, err := p.purgeInvalidKeysLocked(providerFilter)
		return deduped + purged, err
	default:
		return 0, fmt.Errorf("unknown bulk action: %s", action)
	}
}
