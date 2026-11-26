package com.karouter.mobile.data

data class LoginResponse(
    val ok: Boolean,
    val token: String,
    val expiresAt: Long,
    val expiresIn: Long
)

data class ProviderItem(
    val id: String,
    val name: String,
    val category: String,
    val apiType: String,
    val authMode: String,
    val baseUrl: String,
    val defaultModel: String,
    val enabled: Boolean,
    val weight: Int,
    val priority: Int,
    val totalKeys: Int,
    val healthyKeys: Int,
    val eligibleKeys: Int
)

data class PoolKeyItem(
    val id: String,
    val provider: String,
    val label: String,
    val masked: String,
    val enabled: Boolean,
    val status: String,
    val consecFails: Int,
    val okCount: Int,
    val failCount: Int,
    val lastLatencyMs: Long,
    val lastErrorCode: String,
    val lastError: String,
    val cooldownRemainingSec: Long
)

data class ClientKeyItem(
    val id: String,
    val name: String,
    val scope: String,
    val masked: String,
    val enabled: Boolean,
    val usageCount: Long
)

data class LogItem(
    val id: Long,
    val ts: Long,
    val provider: String,
    val providerName: String,
    val keyIndex: Int,
    val keyMasked: String,
    val clientName: String,
    val requestedModel: String,
    val upstreamModel: String,
    val status: Int,
    val routeUs: Long,
    val latencyMs: Long,
    val errorCode: String,
    val error: String
)

data class BackupSummary(
    val hasNeonUrl: Boolean,
    val neonUrlMasked: String,
    val autoBackup: Boolean,
    val intervalDays: Int,
    val lastBackupUnix: Long,
    val nextBackupUnix: Long,
    val lastSummary: String,
    val lastError: String
)

data class RouterSnapshot(
    val version: String,
    val totalProviders: Int,
    val enabledProviders: Int,
    val eligibleKeys: Int,
    val totalKeys: Int,
    val totalRequests: Long,
    val totalSuccess: Long,
    val avgRouteUs: Long,
    val rotationMode: String,
    val keySelection: String,
    val backup: BackupSummary,
    val providers: List<ProviderItem>,
    val keys: List<PoolKeyItem>,
    val clientKeys: List<ClientKeyItem>,
    val logs: List<LogItem>
)

data class ParsedKeyInput(
    val key: String,
    val label: String = ""
)
