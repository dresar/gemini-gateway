import type {
  APIType,
  AuthMode,
  BackupConfig,
  ChatTestResult,
  ClientKeyView,
  KeyAuditReport,
  KeyTestResult,
  KeyValidatePreviewResponse,
  AddKeysResponse,
  ModelsCatalogResponse,
  NeonBackupMeta,
  ProbeModelsResponse,
  ProviderInput,
  ProviderView,
  RouterConfig,
  StatusResponse,
  UsageReport,
} from './types';

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'same-origin',
    cache: 'no-store',
  });
  const text = await res.text();
  let data: Record<string, unknown> = {};
  try {
    data = text ? (JSON.parse(text) as Record<string, unknown>) : {};
  } catch {
    throw new ApiError(res.status, text || `HTTP ${res.status}`);
  }
  if (!res.ok) {
    const errObj = data.error as { message?: string } | undefined;
    throw new ApiError(res.status, errObj?.message || `HTTP ${res.status}`);
  }
  return data as unknown as T;
}

export function checkSession(): Promise<{
  authenticated: boolean;
  expires_at?: number;
  expires_in?: number;
}> {
  return request('/api/auth/session');
}

export function loginAdmin(password: string): Promise<{
  ok: boolean;
  authenticated: boolean;
  token?: string;
  expires_at?: number;
  expires_in?: number;
}> {
  return request('/api/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  });
}

export function logoutAdmin(): Promise<{ ok: boolean }> {
  return request('/api/auth/logout', { method: 'POST' });
}

export function changeAdminPassword(
  currentPassword: string,
  newPassword: string
): Promise<{ ok: boolean; token?: string; expires_at?: number; expires_in?: number }> {
  return request('/api/auth/password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  });
}

export function fetchStatus(): Promise<StatusResponse> {
  return request<StatusResponse>('/api/status');
}

export function fetchModelsCatalog(params?: {
  scope?: string;
  provider?: string;
  probe?: boolean;
}): Promise<ModelsCatalogResponse> {
  const qs = new URLSearchParams();
  if (params?.scope) qs.set('scope', params.scope);
  if (params?.provider) qs.set('provider', params.provider);
  if (params?.probe) qs.set('probe', '1');
  const suffix = qs.toString() ? `?${qs.toString()}` : '';
  return request<ModelsCatalogResponse>(`/api/models${suffix}`);
}

export function saveConfig(cfg: RouterConfig): Promise<{ ok: boolean; config: RouterConfig }> {
  return request('/api/config', {
    method: 'PUT',
    body: JSON.stringify(cfg),
  });
}

export function createProvider(payload: ProviderInput): Promise<{ ok: boolean; provider: ProviderView }> {
  return request('/api/providers', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export function updateProvider(
  id: string,
  payload: ProviderInput
): Promise<{ ok: boolean; provider: ProviderView }> {
  return request(`/api/providers/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export function deleteProvider(id: string): Promise<{ ok: boolean }> {
  return request(`/api/providers/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export function probeProviderModels(
  id: string,
  payload?: { models?: string[]; apply?: boolean }
): Promise<ProbeModelsResponse> {
  return request(`/api/providers/${encodeURIComponent(id)}/probe-models`, {
    method: 'POST',
    body: JSON.stringify(payload || { apply: true }),
  });
}

export function addKeys(payload: {
  provider: string;
  raw_text?: string;
  label_prefix?: string;
  items?: Array<{ key: string; label?: string }>;
}): Promise<AddKeysResponse> {
  return request('/api/keys', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export function validateKeysPreflight(payload: {
  provider: string;
  raw_text?: string;
  label_prefix?: string;
  items?: Array<{ key: string; label?: string }>;
}): Promise<KeyValidatePreviewResponse> {
  return request('/api/keys/audit', {
    method: 'POST',
    body: JSON.stringify({ action: 'validate', ...payload }),
  });
}

export function auditPoolKeys(provider?: string): Promise<KeyAuditReport> {
  const qs = provider && provider !== 'all' ? `?provider=${encodeURIComponent(provider)}` : '';
  return request(`/api/keys/audit${qs}`);
}

export function patchKey(
  id: string,
  payload: { enabled?: boolean; label?: string; action?: 'revive' }
): Promise<{ ok: boolean }> {
  return request(`/api/keys/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  });
}

export function deleteKey(id: string): Promise<{ ok: boolean }> {
  return request(`/api/keys/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export function testKey(id: string): Promise<KeyTestResult> {
  return request(`/api/keys/${encodeURIComponent(id)}/test`, {
    method: 'POST',
  });
}

export function testRawKey(payload: {
  provider_id?: string;
  api_key: string;
  model?: string;
  base_url?: string;
  api_type?: APIType;
  auth_mode?: AuthMode;
}): Promise<KeyTestResult> {
  return request('/api/keys/test-raw', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export function bulkKeyAction(payload: {
  action: 'revive_all' | 'purge_dead' | 'test_all' | 'dedupe_all' | 'purge_invalid' | 'auto_clean';
  provider: string;
}): Promise<{ ok: boolean; affected: number; passed?: number; failed?: number; audit?: KeyAuditReport }> {
  return request('/api/keys/bulk', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export function createClientKey(payload: {
  name: string;
  scope: string;
  custom_key?: string;
}): Promise<{ ok: boolean; key: ClientKeyView; secret: string }> {
  return request('/api/client-keys', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export function patchClientKey(
  id: string,
  payload: { enabled?: boolean; name?: string; scope?: string }
): Promise<{ ok: boolean; key: ClientKeyView }> {
  return request(`/api/client-keys/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  });
}

export function deleteClientKey(id: string): Promise<{ ok: boolean }> {
  return request(`/api/client-keys/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
}

export function saveBackupConfig(payload: {
  neon_url?: string;
  auto_backup?: boolean;
  interval_days?: number;
}): Promise<{ ok: boolean; backup: BackupConfig }> {
  return request('/api/backup/config', {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export function testNeonConnection(neonUrl?: string): Promise<{ ok: boolean; error?: string }> {
  return request('/api/backup/test', {
    method: 'POST',
    body: JSON.stringify({ neon_url: neonUrl || '' }),
  });
}

export function runNeonBackup(): Promise<{
  ok: boolean;
  snapshot: NeonBackupMeta;
  backup: BackupConfig;
}> {
  return request('/api/backup/run', { method: 'POST' });
}

export function listNeonBackups(): Promise<{ ok: boolean; backups: NeonBackupMeta[] }> {
  return request('/api/backup/list');
}

export function restoreNeonBackup(backupId?: number): Promise<{
  ok: boolean;
  restored: NeonBackupMeta;
}> {
  return request('/api/backup/restore', {
    method: 'POST',
    body: JSON.stringify({ backup_id: backupId || 0 }),
  });
}

export function runChatTest(payload: {
  target: string;
  model: string;
  system_prompt?: string;
  prompt: string;
  max_tokens: number;
}): Promise<ChatTestResult> {
  return request('/api/chat/test', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export function fetchUsage(days = 60): Promise<UsageReport> {
  return request(`/api/usage?days=${encodeURIComponent(String(days))}`);
}
