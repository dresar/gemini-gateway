package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func openGatewayDB(dbPath string) (*sql.DB, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA cache_size=-8000;",
	}
	for _, q := range pragmas {
		if _, err := db.Exec(q); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	schema := `
CREATE TABLE IF NOT EXISTS meta_config (
	k TEXT PRIMARY KEY,
	v TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS providers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	category TEXT NOT NULL DEFAULT 'gratis',
	api_type TEXT NOT NULL,
	auth_mode TEXT NOT NULL,
	base_url TEXT NOT NULL,
	default_model TEXT NOT NULL,
	icon TEXT NOT NULL DEFAULT 'custom',
	enabled INTEGER NOT NULL DEFAULT 1,
	weight INTEGER NOT NULL DEFAULT 50,
	priority INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS provider_keys (
	id TEXT PRIMARY KEY,
	provider TEXT NOT NULL,
	api_key TEXT NOT NULL,
	masked TEXT NOT NULL,
	label TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	health TEXT NOT NULL DEFAULT 'healthy',
	cooldown_until INTEGER NOT NULL DEFAULT 0,
	last_used_unix INTEGER NOT NULL DEFAULT 0,
	ok_count INTEGER NOT NULL DEFAULT 0,
	fail_429 INTEGER NOT NULL DEFAULT 0,
	fail_auth INTEGER NOT NULL DEFAULT 0,
	fail_other INTEGER NOT NULL DEFAULT 0,
	consec_fails INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	last_latency_ms INTEGER NOT NULL DEFAULT 0,
	last_error_code TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	UNIQUE(provider, api_key)
);
CREATE TABLE IF NOT EXISTS client_keys (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	scope TEXT NOT NULL,
	key_hash TEXT NOT NULL UNIQUE,
	masked TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	usage_count INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	last_used_unix INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS request_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ts INTEGER NOT NULL,
	provider TEXT NOT NULL,
	provider_name TEXT NOT NULL DEFAULT '',
	key_index INTEGER NOT NULL,
	key_masked TEXT NOT NULL,
	client_name TEXT NOT NULL DEFAULT '',
	scope TEXT NOT NULL DEFAULT '',
	requested_model TEXT NOT NULL,
	upstream_model TEXT NOT NULL,
	status INTEGER NOT NULL,
	route_us INTEGER NOT NULL DEFAULT 0,
	latency_ms INTEGER NOT NULL DEFAULT 0,
	tokens INTEGER NOT NULL DEFAULT 0,
	stream INTEGER NOT NULL DEFAULT 0,
	fallback INTEGER NOT NULL DEFAULT 0,
	attempts INTEGER NOT NULL DEFAULT 1,
	error_code TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT ''
);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, _ = db.Exec(`ALTER TABLE request_logs ADD COLUMN provider_name TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE request_logs ADD COLUMN error_code TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE providers ADD COLUMN category TEXT NOT NULL DEFAULT 'gratis'`)
	_, _ = db.Exec(`ALTER TABLE provider_keys ADD COLUMN consec_fails INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE provider_keys ADD COLUMN last_error_code TEXT NOT NULL DEFAULT ''`)
	if dbPath != ":memory:" {
		_ = os.Chmod(dbPath, 0o600)
	}
	return db, nil
}

func (p *KeyPool) getMeta(k string) (string, bool) {
	var v string
	err := p.db.QueryRow("SELECT v FROM meta_config WHERE k = ?", k).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

func (p *KeyPool) setMeta(k, v string) error {
	_, err := p.db.Exec("INSERT INTO meta_config(k, v) VALUES(?, ?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", k, v)
	return err
}

func NewPool(cfg Config) (*KeyPool, error) {
	db, err := openGatewayDB(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	p := &KeyPool{
		db:           db,
		cfg:          cfg,
		settings:     defaultSettings(),
		providerByID: make(map[string]*Provider),
		providerReqs: make(map[string]int64),
		byID:         make(map[string]*KeyEntry),
		clientByID:   make(map[string]*ClientKey),
		clientByHash: make(map[string]*ClientKey),
		swrrCurrent:  make(map[string]int),
		rrCursor:     make(map[string]int),
		writeCh:      make(chan writeEvent, writeBatchBufferSize),
	}

	if err := p.initAuthAndSettings(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := p.seedDefaultProvidersAndMigrate(); err != nil {
		log.Printf("[db] seed/migrate warning: %v", err)
	}
	if err := p.loadAllFromDB(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return p, nil
}

func (p *KeyPool) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

func (p *KeyPool) initAuthAndSettings() error {
	secHex, ok := p.getMeta("session_secret")
	if !ok || len(secHex) < 32 {
		secHex = randomHex(32)
		if err := p.setMeta("session_secret", secHex); err != nil {
			return err
		}
	}
	secBytes, _ := hex.DecodeString(secHex)
	p.sessionSecret = secBytes

	salt, okSalt := p.getMeta("admin_salt")
	hash, okHash := p.getMeta("admin_hash")
	if !okSalt || !okHash || salt == "" || hash == "" {
		initialPass := defaultAdminPass
		if strings.TrimSpace(p.cfg.AdminPasswordEnv) != "" {
			initialPass = p.cfg.AdminPasswordEnv
		}
		salt = randomHex(16)
		hash = hashAdminPassword(initialPass, salt)
		if err := p.setMeta("admin_salt", salt); err != nil {
			return err
		}
		if err := p.setMeta("admin_hash", hash); err != nil {
			return err
		}
	}
	p.adminSalt = salt
	p.adminHash = hash

	if rawCfg, ok := p.getMeta("router_settings"); ok && rawCfg != "" {
		var s RouterSettings
		if json.Unmarshal([]byte(rawCfg), &s) == nil {
			normalizeSettings(&s)
			p.settings = s
		}
	} else {
		b, _ := json.Marshal(p.settings)
		_ = p.setMeta("router_settings", string(b))
	}

	p.loadBackupConfigLocked()
	return nil
}

func (p *KeyPool) loadBackupConfigLocked() {
	bc := BackupConfig{
		IntervalDays: p.cfg.NeonIntervalDays,
	}
	if bc.IntervalDays < 1 {
		bc.IntervalDays = 7
	}
	if rawURL, ok := p.getMeta("neon_db_url"); ok && strings.TrimSpace(rawURL) != "" {
		bc.NeonURL = strings.TrimSpace(rawURL)
	} else if p.cfg.NeonDBURLEnv != "" {
		bc.NeonURL = p.cfg.NeonDBURLEnv
		_ = p.setMeta("neon_db_url", bc.NeonURL)
	}
	if v, ok := p.getMeta("neon_auto_backup"); ok {
		bc.AutoBackup = v == "1" || v == "true"
	} else if bc.NeonURL != "" {
		bc.AutoBackup = true
		_ = p.setMeta("neon_auto_backup", "true")
	}
	if v, ok := p.getMeta("neon_interval_days"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 90 {
			bc.IntervalDays = n
		}
	}
	if v, ok := p.getMeta("neon_last_backup_unix"); ok {
		bc.LastBackupUnix, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := p.getMeta("neon_last_backup_status"); ok {
		bc.LastStatus = v
	}
	if v, ok := p.getMeta("neon_last_backup_error"); ok {
		bc.LastError = v
	}
	if v, ok := p.getMeta("neon_last_backup_summary"); ok {
		bc.LastSummary = v
	}
	bc.HasNeonURL = bc.NeonURL != ""
	bc.NeonURLMasked = maskPostgresURL(bc.NeonURL)
	if bc.AutoBackup && bc.HasNeonURL {
		if bc.LastBackupUnix > 0 {
			bc.NextBackupUnix = bc.LastBackupUnix + int64(bc.IntervalDays)*86400
		} else {
			bc.NextBackupUnix = time.Now().Unix() + 30
		}
	}
	p.backup = bc
}

func (p *KeyPool) seedDefaultProvidersAndMigrate() error {
	var provCount int
	if err := p.db.QueryRow("SELECT COUNT(1) FROM providers").Scan(&provCount); err != nil {
		return err
	}
	if provCount == 0 {
		var legacyRouter LegacyRouterFile
		hasRouterJSON := false
		if raw, err := os.ReadFile(p.cfg.LegacyStateFile); err == nil && len(raw) > 0 {
			if json.Unmarshal(raw, &legacyRouter) == nil {
				hasRouterJSON = true
			}
		}
		now := time.Now().Unix()
		gemModel := p.cfg.GeminiModel
		agnModel := p.cfg.AgnesModel
		agnURL := p.cfg.AgnesBaseURL
		gemW, agnW := 50, 50
		gemEn, agnEn := 1, 1
		if hasRouterJSON {
			if legacyRouter.Settings.GeminiModel != "" {
				gemModel = legacyRouter.Settings.GeminiModel
			}
			if legacyRouter.Settings.AgnesModel != "" {
				agnModel = legacyRouter.Settings.AgnesModel
			}
			if legacyRouter.Settings.AgnesBaseURL != "" {
				agnURL = legacyRouter.Settings.AgnesBaseURL
			}
			if legacyRouter.Settings.GeminiWeight > 0 || legacyRouter.Settings.AgnesWeight > 0 {
				gemW = legacyRouter.Settings.GeminiWeight
				agnW = legacyRouter.Settings.AgnesWeight
			}
			if !legacyRouter.Settings.GeminiEnabled {
				gemEn = 0
			}
			if !legacyRouter.Settings.AgnesEnabled {
				agnEn = 0
			}
			s := RouterSettings{
				RotationMode:          legacyRouter.Settings.RotationMode,
				ModelRoutingPolicy:    legacyRouter.Settings.ModelRoutingPolicy,
				KeySelection:          legacyRouter.Settings.KeySelection,
				CrossFallback:         legacyRouter.Settings.CrossFallback,
				MaxRetriesPerProvider: legacyRouter.Settings.MaxRetriesPerProvider,
				CooldownBaseSec:       legacyRouter.Settings.CooldownBaseSec,
				DefaultMaxTokens:      legacyRouter.Settings.DefaultMaxTokens,
				RequestTimeoutSec:     legacyRouter.Settings.RequestTimeoutSec,
			}
			if s.RotationMode == "gemini_first" || s.RotationMode == "agnes_first" {
				s.RotationMode = "priority"
			}
			normalizeSettings(&s)
			p.settings = s
			b, _ := json.Marshal(s)
			_ = p.setMeta("router_settings", string(b))
		}
		gemPrio, agnPrio := 1, 2
		if hasRouterJSON && legacyRouter.Settings.RotationMode == "agnes_first" {
			agnPrio, gemPrio = 1, 2
		}
		_, _ = p.db.Exec(
			`INSERT OR IGNORE INTO providers(id,name,category,api_type,auth_mode,base_url,default_model,icon,enabled,weight,priority,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			"gemini", "Gemini", "gratis", apiTypeGemini, authModeQueryKey, p.cfg.GeminiBaseURL, gemModel, "gemini", gemEn, gemW, gemPrio, now,
		)
		_, _ = p.db.Exec(
			`INSERT OR IGNORE INTO providers(id,name,category,api_type,auth_mode,base_url,default_model,icon,enabled,weight,priority,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			"agnes", "Agnes", "gratis", apiTypeOpenAI, authModeBearer, agnURL, agnModel, "agnes", agnEn, agnW, agnPrio, now+1,
		)
	}

	var pkCount int
	if err := p.db.QueryRow("SELECT COUNT(1) FROM provider_keys").Scan(&pkCount); err != nil {
		return err
	}
	if pkCount == 0 {
		var legacyRouter LegacyRouterFile
		hasRouterJSON := false
		if raw, err := os.ReadFile(p.cfg.LegacyStateFile); err == nil && len(raw) > 0 {
			if json.Unmarshal(raw, &legacyRouter) == nil {
				hasRouterJSON = true
			}
		}
		var legacyPool LegacyPoolState
		if !hasRouterJSON && fileExists(p.cfg.LegacyPoolState) {
			if raw, err := os.ReadFile(p.cfg.LegacyPoolState); err == nil {
				_ = json.Unmarshal(raw, &legacyPool)
			}
		}
		importPool := func(provider, path string) {
			lines := readKeyFileLines(path)
			now := time.Now().Unix()
			for idx, rawKey := range lines {
				id := makeKeyID(provider, rawKey)
				stat := KeyStat{
					Label:   fmt.Sprintf("%s%02d", provider, idx+1),
					Enabled: true,
					Health:  "healthy",
				}
				if hasRouterJSON && legacyRouter.KeyStats != nil {
					if prev, ok := legacyRouter.KeyStats[id]; ok {
						stat = prev
					}
				} else if provider == "gemini" && legacyPool != nil {
					if lv, ok := legacyPool[strconv.Itoa(idx)]; ok {
						if lv.Health != "" {
							stat.Health = lv.Health
						}
						stat.CooldownUntil = lv.CooldownUntil
						stat.LastUsedUnix = lv.LastUsedUnix
						stat.OkCount = lv.OkCount
						stat.Fail429 = lv.Fail429
						stat.FailAuth = lv.Fail403
					}
				}
				if stat.Health == "" {
					stat.Health = "healthy"
				}
				if stat.Label == "" {
					stat.Label = fmt.Sprintf("%s%02d", provider, idx+1)
				}
				enInt := 0
				if stat.Enabled {
					enInt = 1
				}
				_, _ = p.db.Exec(
					`INSERT OR IGNORE INTO provider_keys(id,provider,api_key,masked,label,enabled,health,cooldown_until,last_used_unix,ok_count,fail_429,fail_auth,fail_other,consec_fails,total_tokens,last_latency_ms,last_error_code,last_error,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
					id, provider, rawKey, maskKey(rawKey), stat.Label, enInt, stat.Health, stat.CooldownUntil, stat.LastUsedUnix, stat.OkCount, stat.Fail429, stat.FailAuth, stat.FailOther, stat.ConsecFails, stat.TotalTokens, stat.LastLatencyMs, stat.LastErrorCode, stat.LastError, now,
				)
			}
		}
		importPool("gemini", p.cfg.LegacyGemKeys)
		importPool("agnes", p.cfg.LegacyAgnKeys)
	}
	return nil
}

func (p *KeyPool) loadAllFromDB() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.providers = nil
	p.providerByID = make(map[string]*Provider)
	pRows, err := p.db.Query(`SELECT id,name,category,api_type,auth_mode,base_url,default_model,icon,enabled,weight,priority,created_at FROM providers ORDER BY priority ASC, created_at ASC`)
	if err != nil {
		return err
	}
	defer pRows.Close()
	for pRows.Next() {
		var pr Provider
		var en int
		if err := pRows.Scan(&pr.ID, &pr.Name, &pr.Category, &pr.APIType, &pr.AuthMode, &pr.BaseURL, &pr.DefaultModel, &pr.Icon, &en, &pr.Weight, &pr.Priority, &pr.CreatedAt); err != nil {
			return err
		}
		pr.Category = normalizeCategory(pr.Category)
		pr.Enabled = en == 1
		p.providers = append(p.providers, &pr)
		p.providerByID[pr.ID] = &pr
	}

	p.keys = nil
	p.byID = make(map[string]*KeyEntry)
	rows, err := p.db.Query(`SELECT id,provider,api_key,masked,label,enabled,health,cooldown_until,last_used_unix,ok_count,fail_429,fail_auth,fail_other,consec_fails,total_tokens,last_latency_ms,last_error_code,last_error,created_at FROM provider_keys ORDER BY created_at ASC, rowid ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	provIdx := map[string]int{}
	for rows.Next() {
		var k KeyEntry
		var en int
		if err := rows.Scan(
			&k.ID, &k.Provider, &k.Key, &k.Masked, &k.Stat.Label, &en, &k.Stat.Health,
			&k.Stat.CooldownUntil, &k.Stat.LastUsedUnix, &k.Stat.OkCount, &k.Stat.Fail429,
			&k.Stat.FailAuth, &k.Stat.FailOther, &k.Stat.ConsecFails, &k.Stat.TotalTokens,
			&k.Stat.LastLatencyMs, &k.Stat.LastErrorCode, &k.Stat.LastError, &k.CreatedAt,
		); err != nil {
			return err
		}
		k.Stat.Enabled = en == 1
		k.ProviderIndex = provIdx[k.Provider]
		provIdx[k.Provider]++
		k.LastPickedNano = k.Stat.LastUsedUnix * 1e9
		p.keys = append(p.keys, &k)
		p.byID[k.ID] = &k
	}

	p.clients = nil
	p.clientByID = make(map[string]*ClientKey)
	p.clientByHash = make(map[string]*ClientKey)
	cRows, err := p.db.Query(`SELECT id,name,scope,key_hash,masked,enabled,usage_count,total_tokens,last_used_unix,created_at FROM client_keys ORDER BY created_at DESC, rowid DESC`)
	if err != nil {
		return err
	}
	defer cRows.Close()
	for cRows.Next() {
		var ck ClientKey
		var en int
		if err := cRows.Scan(&ck.ID, &ck.Name, &ck.Scope, &ck.KeyHash, &ck.Masked, &en, &ck.UsageCount, &ck.TotalTokens, &ck.LastUsedUnix, &ck.CreatedAt); err != nil {
			return err
		}
		ck.Enabled = en == 1
		p.clients = append(p.clients, &ck)
		p.clientByID[ck.ID] = &ck
		p.clientByHash[ck.KeyHash] = &ck
	}

	p.logHead = 0
	p.logCount = 0
	p.nextLogID = 0
	lRows, err := p.db.Query(`SELECT id,ts,provider,provider_name,key_index,key_masked,client_name,scope,requested_model,upstream_model,status,route_us,latency_ms,tokens,stream,fallback,attempts,error_code,error FROM request_logs ORDER BY id DESC LIMIT ?`, maxLogEntries)
	if err == nil {
		defer lRows.Close()
		var loaded []RequestLog
		for lRows.Next() {
			var lg RequestLog
			var st, fb int
			if err := lRows.Scan(&lg.ID, &lg.Timestamp, &lg.Provider, &lg.ProviderName, &lg.KeyIndex, &lg.KeyMasked, &lg.ClientName, &lg.Scope, &lg.RequestedModel, &lg.UpstreamModel, &lg.Status, &lg.RouteUs, &lg.LatencyMs, &lg.Tokens, &st, &fb, &lg.Attempts, &lg.ErrorCode, &lg.Error); err == nil {
				lg.Stream = st == 1
				lg.Fallback = fb == 1
				loaded = append(loaded, lg)
			}
		}
		for i := len(loaded) - 1; i >= 0; i-- {
			p.logs[p.logHead] = loaded[i]
			p.logHead = (p.logHead + 1) % maxLogEntries
			if p.logCount < maxLogEntries {
				p.logCount++
			}
			if loaded[i].ID > p.nextLogID {
				p.nextLogID = loaded[i].ID
			}
		}
	}
	return nil
}

func (p *KeyPool) enqueueWrite(ev writeEvent) {
	select {
	case p.writeCh <- ev:
	default:
	}
}

func (p *KeyPool) flushBatch(events []writeEvent) {
	if len(events) == 0 {
		return
	}
	dirtyKeys := make(map[string]bool)
	dirtyClients := make(map[string]bool)
	var newLogs []*RequestLog
	for _, ev := range events {
		if ev.providerKeyID != "" {
			dirtyKeys[ev.providerKeyID] = true
		}
		if ev.clientKeyID != "" {
			dirtyClients[ev.clientKeyID] = true
		}
		if ev.logEntry != nil {
			newLogs = append(newLogs, ev.logEntry)
		}
	}

	type pkSnap struct {
		id string
		st KeyStat
	}
	type ckSnap struct {
		id     string
		usage  int64
		tokens int64
		last   int64
	}
	var pkList []pkSnap
	var ckList []ckSnap

	p.mu.RLock()
	for id := range dirtyKeys {
		if k, ok := p.byID[id]; ok {
			pkList = append(pkList, pkSnap{id: id, st: k.Stat})
		}
	}
	for id := range dirtyClients {
		if c, ok := p.clientByID[id]; ok {
			ckList = append(ckList, ckSnap{id: id, usage: c.UsageCount, tokens: c.TotalTokens, last: c.LastUsedUnix})
		}
	}
	p.mu.RUnlock()

	tx, err := p.db.Begin()
	if err != nil {
		return
	}
	for _, item := range pkList {
		_, _ = tx.Exec(
			`UPDATE provider_keys SET health=?,cooldown_until=?,last_used_unix=?,ok_count=?,fail_429=?,fail_auth=?,fail_other=?,consec_fails=?,total_tokens=?,last_latency_ms=?,last_error_code=?,last_error=? WHERE id=?`,
			item.st.Health, item.st.CooldownUntil, item.st.LastUsedUnix, item.st.OkCount, item.st.Fail429, item.st.FailAuth, item.st.FailOther, item.st.ConsecFails, item.st.TotalTokens, item.st.LastLatencyMs, item.st.LastErrorCode, item.st.LastError, item.id,
		)
	}
	for _, item := range ckList {
		_, _ = tx.Exec(
			`UPDATE client_keys SET usage_count=?,total_tokens=?,last_used_unix=? WHERE id=?`,
			item.usage, item.tokens, item.last, item.id,
		)
	}
	for _, lg := range newLogs {
		stInt, fbInt := 0, 0
		if lg.Stream {
			stInt = 1
		}
		if lg.Fallback {
			fbInt = 1
		}
		_, _ = tx.Exec(
			`INSERT INTO request_logs(ts,provider,provider_name,key_index,key_masked,client_name,scope,requested_model,upstream_model,status,route_us,latency_ms,tokens,stream,fallback,attempts,error_code,error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			lg.Timestamp, lg.Provider, lg.ProviderName, lg.KeyIndex, lg.KeyMasked, lg.ClientName, lg.Scope, lg.RequestedModel, lg.UpstreamModel, lg.Status, lg.RouteUs, lg.LatencyMs, lg.Tokens, stInt, fbInt, lg.Attempts, lg.ErrorCode, lg.Error,
		)
	}
	if len(newLogs) > 0 {
		_, _ = tx.Exec(`DELETE FROM request_logs WHERE id <= (SELECT COALESCE(MAX(id),0) - 250 FROM request_logs)`)
	}
	_ = tx.Commit()
}

func (p *KeyPool) StartDBWriter(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var batch []writeEvent
	drainAndFlush := func() {
		for {
			select {
			case ev := <-p.writeCh:
				batch = append(batch, ev)
				if len(batch) >= 64 {
					p.flushBatch(batch)
					batch = batch[:0]
				}
			default:
				if len(batch) > 0 {
					p.flushBatch(batch)
					batch = batch[:0]
				}
				return
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			drainAndFlush()
			return
		case ev := <-p.writeCh:
			batch = append(batch, ev)
			if len(batch) >= 64 {
				p.flushBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			drainAndFlush()
		}
	}
}

func (p *KeyPool) FlushPending() {
	var batch []writeEvent
	for {
		select {
		case ev := <-p.writeCh:
			batch = append(batch, ev)
		default:
			p.flushBatch(batch)
			return
		}
	}
}

func (p *KeyPool) reindexProvidersLocked() {
	idx := map[string]int{}
	for _, k := range p.keys {
		k.ProviderIndex = idx[k.Provider]
		idx[k.Provider]++
	}
}
