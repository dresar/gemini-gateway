package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func (p *KeyPool) BackupSnapshotConfig() BackupConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := p.backup
	cp.NeonURL = ""
	return cp
}

func (p *KeyPool) UpdateBackupConfig(neonURL *string, autoBackup *bool, intervalDays *int) (BackupConfig, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if neonURL != nil {
		trimmed := strings.TrimSpace(*neonURL)
		if trimmed != "" && !strings.Contains(trimmed, "••••") {
			if !strings.HasPrefix(trimmed, "postgres://") && !strings.HasPrefix(trimmed, "postgresql://") {
				return p.backup, errors.New("URL harus diawali postgresql:// atau postgres://")
			}
			if err := p.setMeta("neon_db_url", trimmed); err != nil {
				return p.backup, err
			}
		} else if trimmed == "" {
			if err := p.setMeta("neon_db_url", ""); err != nil {
				return p.backup, err
			}
		}
	}
	if autoBackup != nil {
		val := "false"
		if *autoBackup {
			val = "true"
		}
		if err := p.setMeta("neon_auto_backup", val); err != nil {
			return p.backup, err
		}
	}
	if intervalDays != nil {
		d := *intervalDays
		if d < 1 {
			d = 1
		}
		if d > 90 {
			d = 90
		}
		if err := p.setMeta("neon_interval_days", strconv.Itoa(d)); err != nil {
			return p.backup, err
		}
	}
	p.loadBackupConfigLocked()
	cp := p.backup
	cp.NeonURL = ""
	return cp, nil
}

func (p *KeyPool) exportSnapshot() (BackupSnapshot, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	snap := BackupSnapshot{
		Version:      1,
		ExportedAt:   time.Now().Unix(),
		Settings:     p.settings,
		Providers:    make([]Provider, 0, len(p.providers)),
		ProviderKeys: make([]BackupKeyRow, 0, len(p.keys)),
		ClientKeys:   make([]BackupClient, 0, len(p.clients)),
	}
	for _, pr := range p.providers {
		snap.Providers = append(snap.Providers, *pr)
	}
	for _, k := range p.keys {
		snap.ProviderKeys = append(snap.ProviderKeys, BackupKeyRow{
			ID:            k.ID,
			Provider:      k.Provider,
			APIKey:        k.Key,
			Label:         k.Stat.Label,
			Enabled:       k.Stat.Enabled,
			Health:        k.Stat.Health,
			CooldownUntil: k.Stat.CooldownUntil,
			LastUsedUnix:  k.Stat.LastUsedUnix,
			OkCount:       k.Stat.OkCount,
			Fail429:       k.Stat.Fail429,
			FailAuth:      k.Stat.FailAuth,
			FailOther:     k.Stat.FailOther,
			ConsecFails:   k.Stat.ConsecFails,
			TotalTokens:   k.Stat.TotalTokens,
			LastLatencyMs: k.Stat.LastLatencyMs,
			LastErrorCode: k.Stat.LastErrorCode,
			LastError:     k.Stat.LastError,
			CreatedAt:     k.CreatedAt,
		})
	}
	for _, c := range p.clients {
		snap.ClientKeys = append(snap.ClientKeys, BackupClient{
			ID:           c.ID,
			Name:         c.Name,
			Scope:        c.Scope,
			KeyHash:      c.KeyHash,
			Masked:       c.Masked,
			Enabled:      c.Enabled,
			UsageCount:   c.UsageCount,
			TotalTokens:  c.TotalTokens,
			LastUsedUnix: c.LastUsedUnix,
			CreatedAt:    c.CreatedAt,
		})
	}
	return snap, p.backup.NeonURL
}

func (p *KeyPool) recordBackupOutcome(status, summary, errMsg string, ts int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ts > 0 {
		_ = p.setMeta("neon_last_backup_unix", strconv.FormatInt(ts, 10))
	}
	_ = p.setMeta("neon_last_backup_status", status)
	_ = p.setMeta("neon_last_backup_summary", summary)
	_ = p.setMeta("neon_last_backup_error", errMsg)
	p.loadBackupConfigLocked()
}

func openNeonDB(ctx context.Context, connStr string) (*sql.DB, error) {
	if strings.TrimSpace(connStr) == "" {
		return nil, errors.New("URL database Neon belum diatur")
	}
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, err = db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS karouter_backups (
	id BIGSERIAL PRIMARY KEY,
	created_at BIGINT NOT NULL,
	trigger_type TEXT NOT NULL,
	providers_count INTEGER NOT NULL,
	provider_keys_count INTEGER NOT NULL,
	client_keys_count INTEGER NOT NULL,
	snapshot_json TEXT NOT NULL
);`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (g *Gateway) TestNeonConnection(ctx context.Context, rawURL string) error {
	target := strings.TrimSpace(rawURL)
	if target == "" || strings.Contains(target, "••••") {
		_, stored := g.pool.exportSnapshot()
		target = stored
	}
	tCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	db, err := openNeonDB(tCtx, target)
	if err != nil {
		return err
	}
	_ = db.Close()
	return nil
}

func (g *Gateway) BackupToNeon(ctx context.Context, triggerType string) (NeonBackupMeta, error) {
	g.backupMu.Lock()
	defer g.backupMu.Unlock()

	g.pool.FlushPending()
	snap, neonURL := g.pool.exportSnapshot()
	if neonURL == "" {
		return NeonBackupMeta{}, errors.New("URL database Neon belum diisi")
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return NeonBackupMeta{}, err
	}

	tCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pg, err := openNeonDB(tCtx, neonURL)
	if err != nil {
		g.pool.recordBackupOutcome("error", "", sanitizeError(err.Error(), neonURL), 0)
		return NeonBackupMeta{}, err
	}
	defer pg.Close()

	now := time.Now().Unix()
	var id int64
	err = pg.QueryRowContext(
		tCtx,
		`INSERT INTO karouter_backups(created_at,trigger_type,providers_count,provider_keys_count,client_keys_count,snapshot_json) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
		now, triggerType, len(snap.Providers), len(snap.ProviderKeys), len(snap.ClientKeys), string(payload),
	).Scan(&id)
	if err != nil {
		g.pool.recordBackupOutcome("error", "", sanitizeError(err.Error(), neonURL), 0)
		return NeonBackupMeta{}, err
	}
	_, _ = pg.ExecContext(tCtx, `DELETE FROM karouter_backups WHERE id NOT IN (SELECT id FROM karouter_backups ORDER BY id DESC LIMIT 12)`)

	summary := fmt.Sprintf("%d provider, %d key, %d akses", len(snap.Providers), len(snap.ProviderKeys), len(snap.ClientKeys))
	g.pool.recordBackupOutcome("ok", summary, "", now)
	log.Printf("[neon] backup #%d (%s): %s", id, triggerType, summary)
	return NeonBackupMeta{
		ID:                id,
		CreatedAt:         now,
		TriggerType:       triggerType,
		ProvidersCount:    len(snap.Providers),
		ProviderKeysCount: len(snap.ProviderKeys),
		ClientKeysCount:   len(snap.ClientKeys),
	}, nil
}

func (g *Gateway) ListNeonBackups(ctx context.Context) ([]NeonBackupMeta, error) {
	_, neonURL := g.pool.exportSnapshot()
	if neonURL == "" {
		return nil, errors.New("URL database Neon belum diisi")
	}
	tCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pg, err := openNeonDB(tCtx, neonURL)
	if err != nil {
		return nil, err
	}
	defer pg.Close()

	rows, err := pg.QueryContext(tCtx, `SELECT id,created_at,trigger_type,providers_count,provider_keys_count,client_keys_count FROM karouter_backups ORDER BY id DESC LIMIT 12`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []NeonBackupMeta
	for rows.Next() {
		var m NeonBackupMeta
		if rows.Scan(&m.ID, &m.CreatedAt, &m.TriggerType, &m.ProvidersCount, &m.ProviderKeysCount, &m.ClientKeysCount) == nil {
			list = append(list, m)
		}
	}
	return list, nil
}

func (g *Gateway) RestoreFromNeon(ctx context.Context, backupID int64) (NeonBackupMeta, error) {
	g.backupMu.Lock()
	defer g.backupMu.Unlock()

	_, neonURL := g.pool.exportSnapshot()
	if neonURL == "" {
		return NeonBackupMeta{}, errors.New("URL database Neon belum diisi")
	}
	tCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pg, err := openNeonDB(tCtx, neonURL)
	if err != nil {
		return NeonBackupMeta{}, err
	}
	defer pg.Close()

	var meta NeonBackupMeta
	var rawJSON string
	if backupID > 0 {
		err = pg.QueryRowContext(
			tCtx,
			`SELECT id,created_at,trigger_type,providers_count,provider_keys_count,client_keys_count,snapshot_json FROM karouter_backups WHERE id=$1`,
			backupID,
		).Scan(&meta.ID, &meta.CreatedAt, &meta.TriggerType, &meta.ProvidersCount, &meta.ProviderKeysCount, &meta.ClientKeysCount, &rawJSON)
	} else {
		err = pg.QueryRowContext(
			tCtx,
			`SELECT id,created_at,trigger_type,providers_count,provider_keys_count,client_keys_count,snapshot_json FROM karouter_backups ORDER BY id DESC LIMIT 1`,
		).Scan(&meta.ID, &meta.CreatedAt, &meta.TriggerType, &meta.ProvidersCount, &meta.ProviderKeysCount, &meta.ClientKeysCount, &rawJSON)
	}
	if err != nil {
		return NeonBackupMeta{}, fmt.Errorf("backup tidak ditemukan: %w", err)
	}

	var snap BackupSnapshot
	if err := json.Unmarshal([]byte(rawJSON), &snap); err != nil {
		return NeonBackupMeta{}, err
	}
	if err := g.pool.applyBackupSnapshot(snap); err != nil {
		return NeonBackupMeta{}, err
	}
	return meta, nil
}

func (p *KeyPool) applyBackupSnapshot(snap BackupSnapshot) error {
	p.FlushPending()
	p.mu.Lock()
	tx, err := p.db.Begin()
	if err != nil {
		p.mu.Unlock()
		return err
	}
	_, _ = tx.Exec(`DELETE FROM providers`)
	_, _ = tx.Exec(`DELETE FROM provider_keys`)
	_, _ = tx.Exec(`DELETE FROM client_keys`)

	for _, pr := range snap.Providers {
		en := 0
		if pr.Enabled {
			en = 1
		}
		rotEn := 0
		if pr.ModelRotationEnabled {
			rotEn = 1
		}
		_, _ = tx.Exec(
			`INSERT INTO providers(id,name,category,api_type,auth_mode,base_url,default_model,icon,enabled,weight,priority,model_rotation_enabled,rotation_models,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			pr.ID, pr.Name, normalizeCategory(pr.Category), pr.APIType, pr.AuthMode, pr.BaseURL, pr.DefaultModel, pr.Icon, en, pr.Weight, pr.Priority, rotEn, encodeRotationModels(pr.RotationModels), pr.CreatedAt,
		)
	}
	for _, k := range snap.ProviderKeys {
		en := 0
		if k.Enabled {
			en = 1
		}
		_, _ = tx.Exec(
			`INSERT INTO provider_keys(id,provider,api_key,masked,label,enabled,health,cooldown_until,last_used_unix,ok_count,fail_429,fail_auth,fail_other,consec_fails,total_tokens,last_latency_ms,last_error_code,last_error,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			k.ID, k.Provider, k.APIKey, maskKey(k.APIKey), k.Label, en, k.Health, k.CooldownUntil, k.LastUsedUnix, k.OkCount, k.Fail429, k.FailAuth, k.FailOther, k.ConsecFails, k.TotalTokens, k.LastLatencyMs, k.LastErrorCode, k.LastError, k.CreatedAt,
		)
	}
	for _, c := range snap.ClientKeys {
		en := 0
		if c.Enabled {
			en = 1
		}
		_, _ = tx.Exec(
			`INSERT INTO client_keys(id,name,scope,key_hash,masked,enabled,usage_count,total_tokens,last_used_unix,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.Name, c.Scope, c.KeyHash, c.Masked, en, c.UsageCount, c.TotalTokens, c.LastUsedUnix, c.CreatedAt,
		)
	}
	normalizeSettings(&snap.Settings)
	cfgBytes, _ := json.Marshal(snap.Settings)
	_, _ = tx.Exec(`INSERT INTO meta_config(k, v) VALUES('router_settings', ?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, string(cfgBytes))
	if err := tx.Commit(); err != nil {
		p.mu.Unlock()
		return err
	}
	p.settings = snap.Settings
	p.mu.Unlock()
	return p.loadAllFromDB()
}

func (g *Gateway) StartBackupScheduler(ctx context.Context) {
	checkDue := func() {
		bc := g.pool.BackupSnapshotConfig()
		_, rawURL := g.pool.exportSnapshot()
		if !bc.AutoBackup || rawURL == "" {
			return
		}
		due := bc.LastBackupUnix == 0 || (time.Now().Unix()-bc.LastBackupUnix) >= int64(bc.IntervalDays)*86400
		if due {
			if _, err := g.BackupToNeon(ctx, "auto_7d"); err != nil {
				log.Printf("[neon] auto backup error: %v", err)
			}
		}
	}

	initTimer := time.NewTimer(30 * time.Second)
	ticker := time.NewTicker(1 * time.Hour)
	defer initTimer.Stop()
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-initTimer.C:
			checkDue()
		case <-ticker.C:
			checkDue()
		}
	}
}
