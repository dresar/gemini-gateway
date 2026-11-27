export type RotationMode = 'weighted' | 'priority' | 'model_prefix';
export type ModelRoutingPolicy = 'auto_or_override' | 'always_rotate';
export type ModelRotationMode = 'off' | 'round_robin' | 'random' | 'smart';
export type KeySelectionMode = 'smart' | 'random' | 'lru_jitter' | 'round_robin';
export type ThemeChoice = 'system' | 'light' | 'dark';
export type APIType = 'openai' | 'gemini';
export type AuthMode = 'bearer' | 'x-api-key' | 'query_key';

export interface ProviderInput {
  id?: string;
  name: string;
  category: string;
  api_type: APIType;
  auth_mode: AuthMode;
  base_url: string;
  default_model: string;
  icon: string;
  enabled: boolean;
  weight: number;
  priority: number;
  model_rotation_enabled?: boolean;
  rotation_models?: string[];
}

export interface ProviderView extends ProviderInput {
  id: string;
  created_at: number;
  total_keys: number;
  healthy_keys: number;
  eligible_keys: number;
  req_count: number;
}

export interface RouterConfig {
  rotation_mode: RotationMode;
  model_routing_policy: ModelRoutingPolicy;
  model_rotation_mode: ModelRotationMode;
  key_selection: KeySelectionMode;
  cross_fallback: boolean;
  max_retries_per_provider: number;
  cooldown_base_sec: number;
  default_max_tokens: number;
  request_timeout_sec: number;
  host?: string;
  port?: string;
  db_path?: string;
}

export interface ProbeModelsResponse {
  ok: boolean;
  provider: ProviderView;
  working_models: string[];
  tested_count: number;
  working_count: number;
  duration_ms: number;
}

export interface BackupConfig {
  has_neon_url: boolean;
  neon_url_masked: string;
  auto_backup: boolean;
  interval_days: number;
  last_backup_unix: number;
  next_backup_unix: number;
  last_status: string;
  last_error: string;
  last_summary: string;
}

export interface NeonBackupMeta {
  id: number;
  created_at: number;
  trigger_type: string;
  providers_count: number;
  provider_keys_count: number;
  client_keys_count: number;
}

export interface KeyView {
  id: string;
  index: number;
  provider: string;
  label: string;
  masked: string;
  enabled: boolean;
  status: 'healthy' | 'cooldown' | 'dead' | 'paused';
  health: string;
  cooldown_until: number;
  cooldown_remaining_sec: number;
  last_used_unix: number;
  ok_count: number;
  fail_429: number;
  fail_auth: number;
  fail_other: number;
  consec_fails: number;
  total_tokens: number;
  last_latency_ms: number;
  last_error_code?: string;
  last_error: string;
}

export interface ClientKeyView {
  id: string;
  name: string;
  scope: string;
  masked: string;
  enabled: boolean;
  usage_count: number;
  total_tokens: number;
  last_used_unix: number;
  created_at: number;
}

export interface RequestLog {
  id: number;
  ts: number;
  provider: string;
  provider_name?: string;
  key_index: number;
  key_masked: string;
  client_name?: string;
  scope?: string;
  requested_model: string;
  upstream_model: string;
  status: number;
  route_us?: number;
  latency_ms: number;
  tokens: number;
  stream: boolean;
  fallback: boolean;
  attempts: number;
  error_code?: string;
  error?: string;
}

export interface UsageTotals {
  requests: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  reasoning_tokens: number;
  total_tokens: number;
}

export interface UsageModelSummary extends UsageTotals {
  provider: string;
  model: string;
}

export interface UsageDaySummary extends UsageTotals {
  day: string;
}

export interface UsageReport {
  ok?: boolean;
  days: number;
  cutoff_day: string;
  totals: UsageTotals;
  by_model: UsageModelSummary[];
  by_provider: UsageModelSummary[];
  daily: UsageDaySummary[];
}

export interface StatusResponse {
  name?: string;
  version: string;
  uptime_sec: number;
  started_at: number;
  storage?: string;
  db_path?: string;
  stats: {
    total_requests: number;
    total_success: number;
    total_errors: number;
    total_retries: number;
    total_fallbacks: number;
    total_tokens: number;
    avg_latency_ms: number;
    avg_route_us: number;
  };
  pool: {
    total: number;
    healthy: number;
    eligible: number;
    providers: number;
    client_keys?: number;
  };
  config: RouterConfig;
  backup: BackupConfig;
  apk?: {
    available: boolean;
    filename: string;
    size_bytes: number;
  };
  usage?: UsageReport;
  providers: ProviderView[];
  keys: KeyView[];
  client_keys: ClientKeyView[];
  logs: RequestLog[];
}

export interface ModelProviderSummary {
  id: string;
  name: string;
  icon: string;
  category: string;
  api_type: APIType;
  base_url: string;
  default_model: string;
  enabled: boolean;
  eligible_keys: number;
  total_keys: number;
  probed: boolean;
  probe_ok: boolean;
  probe_status: number;
  probe_latency_ms: number;
  probe_count: number;
  probe_error?: string;
}

export interface ModelCatalogItem {
  id: string;
  routed_id: string;
  display_name: string;
  description?: string;
  owned_by: string;
  provider_id: string;
  provider_name: string;
  provider_icon: string;
  category: string;
  api_type: APIType | 'gateway';
  is_default: boolean;
  source: 'virtual' | 'configured' | 'upstream';
  status: 'ready' | 'no_key' | 'disabled';
  input_token_limit?: number;
  output_token_limit?: number;
  context_length?: number;
  methods?: string[];
  protocols: string[];
}

export interface ModelsCatalogResponse {
  ok: boolean;
  scope: string;
  probed: boolean;
  generated_at: number;
  providers: ModelProviderSummary[];
  models: ModelCatalogItem[];
}

export interface ChatTestResult {
  ok: boolean;
  provider?: string;
  provider_name?: string;
  key_index?: number;
  key_masked?: string;
  model?: string;
  route_us?: number;
  latency_ms?: number;
  tokens?: number;
  attempts?: number;
  fallback?: boolean;
  reply?: string;
  error?: string;
}

export interface KeyTestResult {
  ok: boolean;
  id?: string;
  provider: string;
  model: string;
  status: number;
  latency_ms: number;
  reply?: string;
  error_code?: string;
  error?: string;
}

export interface KeyValidationIssue {
  index: number;
  masked: string;
  label?: string;
  code: string;
  reason: string;
}

export interface AddKeysResponse {
  ok: boolean;
  added: number;
  duplicates: number;
  pool_duplicates?: number;
  batch_duplicates?: number;
  invalid?: number;
  cross_duplicates?: number;
  total_provider_keys: number;
  invalid_items?: KeyValidationIssue[];
  warnings?: string[];
}

export interface KeyValidatePreviewResponse {
  ok: boolean;
  total_parsed: number;
  valid: number;
  pool_duplicates: number;
  batch_duplicates: number;
  invalid: number;
  cross_duplicates: number;
  items: Array<{
    index: number;
    masked: string;
    label: string;
    status: 'valid' | 'pool_duplicate' | 'batch_duplicate' | 'invalid';
    code?: string;
    reason?: string;
    warn_code?: string;
    warn_reason?: string;
  }>;
}

export interface KeyAuditReport {
  ok: boolean;
  total_keys: number;
  healthy_count: number;
  exact_duplicates: number;
  cross_duplicates: number;
  invalid_count: number;
  warning_count: number;
  duplicate_groups: Array<{
    provider: string;
    masked: string;
    count: number;
    keep_id: string;
    keep_label: string;
    duplicate_ids: string[];
  }>;
  cross_conflicts: Array<{
    masked: string;
    providers: string[];
    labels: string[];
  }>;
  invalid_items: Array<{
    id: string;
    provider: string;
    label: string;
    masked: string;
    code: string;
    reason: string;
  }>;
}
