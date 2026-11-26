package com.karouter.mobile.data

import android.content.Context
import com.karouter.mobile.BuildConfig
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL

class SessionExpiredException(message: String) : Exception(message)

class KaRouterClient(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("karouter_prefs", Context.MODE_PRIVATE)

    var baseUrl: String
        get() = prefs.getString("base_url", BuildConfig.DEFAULT_BASE_URL)
            ?.trim()
            ?.trimEnd('/')
            ?.ifEmpty { BuildConfig.DEFAULT_BASE_URL }
            ?: BuildConfig.DEFAULT_BASE_URL
        set(value) {
            prefs.edit().putString("base_url", value.trim().trimEnd('/')).apply()
        }

    var token: String
        get() = prefs.getString("jwt_token", "") ?: ""
        private set(value) {
            prefs.edit().putString("jwt_token", value).apply()
        }

    var expiresAt: Long
        get() = prefs.getLong("jwt_expires_at", 0L)
        private set(value) {
            prefs.edit().putLong("jwt_expires_at", value).apply()
        }

    var darkTheme: Boolean
        get() = prefs.getBoolean("dark_theme", true)
        set(value) {
            prefs.edit().putBoolean("dark_theme", value).apply()
        }

    fun hasValidSession(): Boolean {
        val tok = token
        val exp = expiresAt
        if (tok.isBlank() || exp <= 0L) return false
        val now = System.currentTimeMillis() / 1000L
        if (now >= exp) {
            clearSession()
            return false
        }
        return true
    }

    fun clearSession() {
        prefs.edit().remove("jwt_token").remove("jwt_expires_at").apply()
    }

    suspend fun login(urlInput: String, password: String): LoginResponse = withContext(Dispatchers.IO) {
        val cleanUrl = urlInput.trim().trimEnd('/')
        if (!cleanUrl.startsWith("http://") && !cleanUrl.startsWith("https://")) {
            throw Exception("Endpoint harus diawali http:// atau https://")
        }
        baseUrl = cleanUrl
        val body = JSONObject().put("password", password).toString()
        val raw = doHttp("POST", "/api/auth/login", body, authRequired = false)
        val obj = JSONObject(raw)
        val jwt = obj.optString("token")
        val exp = obj.optLong("expires_at", (System.currentTimeMillis() / 1000L) + 86400L)
        val expIn = obj.optLong("expires_in", 86400L)
        if (jwt.isBlank()) {
            throw Exception("Server tidak mengembalikan JWT")
        }
        token = jwt
        expiresAt = exp
        LoginResponse(ok = obj.optBoolean("ok", true), token = jwt, expiresAt = exp, expiresIn = expIn)
    }

    suspend fun checkSession(): Boolean = withContext(Dispatchers.IO) {
        if (!hasValidSession()) return@withContext false
        return@withContext try {
            val raw = doHttp("GET", "/api/auth/session", null, authRequired = true)
            val obj = JSONObject(raw)
            val auth = obj.optBoolean("authenticated", false)
            val srvExp = obj.optLong("expires_at", 0L)
            if (srvExp > 0L) expiresAt = srvExp
            if (!auth) clearSession()
            auth
        } catch (_: SessionExpiredException) {
            clearSession()
            false
        }
    }

    suspend fun logout() = withContext(Dispatchers.IO) {
        try {
            if (token.isNotBlank()) {
                doHttp("POST", "/api/auth/logout", "{}", authRequired = true)
            }
        } catch (_: Exception) {
        } finally {
            clearSession()
        }
    }

    suspend fun fetchStatus(): RouterSnapshot = withContext(Dispatchers.IO) {
        val raw = doHttp("GET", "/api/status", null, authRequired = true)
        val root = JSONObject(raw)
        val stats = root.optJSONObject("stats") ?: JSONObject()
        val pool = root.optJSONObject("pool") ?: JSONObject()
        val cfg = root.optJSONObject("config") ?: JSONObject()
        val bk = root.optJSONObject("backup") ?: JSONObject()

        val providers = mutableListOf<ProviderItem>()
        val pArr = root.optJSONArray("providers") ?: JSONArray()
        var enabledProvs = 0
        for (i in 0 until pArr.length()) {
            val p = pArr.getJSONObject(i)
            val en = p.optBoolean("enabled", true)
            if (en) enabledProvs++
            providers.add(
                ProviderItem(
                    id = p.optString("id"),
                    name = p.optString("name"),
                    category = p.optString("category", "gratis"),
                    apiType = p.optString("api_type", "openai"),
                    authMode = p.optString("auth_mode", "bearer"),
                    baseUrl = p.optString("base_url"),
                    defaultModel = p.optString("default_model"),
                    enabled = en,
                    weight = p.optInt("weight", 50),
                    priority = p.optInt("priority", 1),
                    totalKeys = p.optInt("total_keys", 0),
                    healthyKeys = p.optInt("healthy_keys", 0),
                    eligibleKeys = p.optInt("eligible_keys", 0)
                )
            )
        }

        val keys = mutableListOf<PoolKeyItem>()
        val kArr = root.optJSONArray("keys") ?: JSONArray()
        for (i in 0 until kArr.length()) {
            val k = kArr.getJSONObject(i)
            keys.add(
                PoolKeyItem(
                    id = k.optString("id"),
                    provider = k.optString("provider"),
                    label = k.optString("label"),
                    masked = k.optString("masked"),
                    enabled = k.optBoolean("enabled", true),
                    status = k.optString("status", "healthy"),
                    consecFails = k.optInt("consec_fails", 0),
                    okCount = k.optInt("ok_count", 0),
                    failCount = k.optInt("fail_429", 0) + k.optInt("fail_auth", 0) + k.optInt("fail_other", 0),
                    lastLatencyMs = k.optLong("last_latency_ms", 0),
                    lastErrorCode = k.optString("last_error_code"),
                    lastError = k.optString("last_error"),
                    cooldownRemainingSec = k.optLong("cooldown_remaining_sec", 0)
                )
            )
        }

        val clientKeys = mutableListOf<ClientKeyItem>()
        val cArr = root.optJSONArray("client_keys") ?: JSONArray()
        for (i in 0 until cArr.length()) {
            val c = cArr.getJSONObject(i)
            clientKeys.add(
                ClientKeyItem(
                    id = c.optString("id"),
                    name = c.optString("name"),
                    scope = c.optString("scope", "global"),
                    masked = c.optString("masked"),
                    enabled = c.optBoolean("enabled", true),
                    usageCount = c.optLong("usage_count", 0)
                )
            )
        }

        val logs = mutableListOf<LogItem>()
        val lArr = root.optJSONArray("logs") ?: JSONArray()
        for (i in 0 until lArr.length()) {
            val l = lArr.getJSONObject(i)
            logs.add(
                LogItem(
                    id = l.optLong("id", i.toLong()),
                    ts = l.optLong("ts", 0),
                    provider = l.optString("provider"),
                    providerName = l.optString("provider_name"),
                    keyIndex = l.optInt("key_index", -1),
                    keyMasked = l.optString("key_masked"),
                    clientName = l.optString("client_name"),
                    requestedModel = l.optString("requested_model"),
                    upstreamModel = l.optString("upstream_model"),
                    status = l.optInt("status", 200),
                    routeUs = l.optLong("route_us", 0),
                    latencyMs = l.optLong("latency_ms", 0),
                    errorCode = l.optString("error_code"),
                    error = l.optString("error")
                )
            )
        }

        RouterSnapshot(
            version = root.optString("version", "4.1.0"),
            totalProviders = providers.size,
            enabledProviders = enabledProvs,
            eligibleKeys = pool.optInt("eligible", 0),
            totalKeys = pool.optInt("total", 0),
            totalRequests = stats.optLong("total_requests", 0),
            totalSuccess = stats.optLong("total_success", 0),
            avgRouteUs = stats.optLong("avg_route_us", 0),
            rotationMode = cfg.optString("rotation_mode", "weighted"),
            keySelection = cfg.optString("key_selection", "smart"),
            backup = BackupSummary(
                hasNeonUrl = bk.optBoolean("has_neon_url", false),
                neonUrlMasked = bk.optString("neon_url_masked"),
                autoBackup = bk.optBoolean("auto_backup", true),
                intervalDays = bk.optInt("interval_days", 7),
                lastBackupUnix = bk.optLong("last_backup_unix", 0),
                nextBackupUnix = bk.optLong("next_backup_unix", 0),
                lastSummary = bk.optString("last_summary"),
                lastError = bk.optString("last_error")
            ),
            providers = providers,
            keys = keys,
            clientKeys = clientKeys,
            logs = logs
        )
    }

    suspend fun updateRouterMode(rotationMode: String? = null, keySelection: String? = null) = withContext(Dispatchers.IO) {
        val statusRaw = doHttp("GET", "/api/status", null, authRequired = true)
        val cfg = JSONObject(statusRaw).optJSONObject("config") ?: JSONObject()
        if (rotationMode != null) cfg.put("rotation_mode", rotationMode)
        if (keySelection != null) cfg.put("key_selection", keySelection)
        doHttp("PUT", "/api/config", cfg.toString(), authRequired = true)
    }

    suspend fun addKeys(providerId: String, labelPrefix: String, items: List<ParsedKeyInput>): Pair<Int, Int> =
        withContext(Dispatchers.IO) {
            val arr = JSONArray()
            for (it in items) {
                arr.put(JSONObject().put("key", it.key).put("label", it.label))
            }
            val payload = JSONObject()
                .put("provider", providerId)
                .put("label_prefix", labelPrefix)
                .put("items", arr)
                .toString()
            val obj = JSONObject(doHttp("POST", "/api/keys", payload, authRequired = true))
            Pair(obj.optInt("added", 0), obj.optInt("duplicates", 0))
        }

    suspend fun testKey(id: String): String = withContext(Dispatchers.IO) {
        val obj = JSONObject(doHttp("POST", "/api/keys/$id/test", "{}", authRequired = true))
        val ok = obj.optBoolean("ok", false)
        if (ok) "OK · ${obj.optLong("latency_ms")} ms" else obj.optString("error", "Gagal")
    }

    suspend fun reviveKey(id: String) = withContext(Dispatchers.IO) {
        doHttp("PATCH", "/api/keys/$id", JSONObject().put("action", "revive").toString(), authRequired = true)
    }

    suspend fun deleteKey(id: String) = withContext(Dispatchers.IO) {
        doHttp("DELETE", "/api/keys/$id", null, authRequired = true)
    }

    suspend fun createClientKey(name: String, scope: String): String = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("name", name).put("scope", scope).toString()
        val obj = JSONObject(doHttp("POST", "/api/client-keys", payload, authRequired = true))
        obj.optString("secret")
    }

    suspend fun deleteClientKey(id: String) = withContext(Dispatchers.IO) {
        doHttp("DELETE", "/api/client-keys/$id", null, authRequired = true)
    }

    suspend fun runNeonBackup(): String = withContext(Dispatchers.IO) {
        val obj = JSONObject(doHttp("POST", "/api/backup/run", "{}", authRequired = true))
        val snap = obj.optJSONObject("snapshot")
        "Backup #${snap?.optLong("id") ?: 0} sukses"
    }

    private fun doHttp(method: String, path: String, jsonBody: String?, authRequired: Boolean): String {
        if (authRequired && !hasValidSession()) {
            throw SessionExpiredException("Sesi 24 jam berakhir")
        }
        val conn = (URL("$baseUrl$path").openConnection() as HttpURLConnection).apply {
            requestMethod = method
            connectTimeout = 12000
            readTimeout = 25000
            setRequestProperty("Accept", "application/json")
            if (authRequired) {
                setRequestProperty("Authorization", "Bearer $token")
            }
            if (jsonBody != null) {
                doOutput = true
                setRequestProperty("Content-Type", "application/json; charset=utf-8")
            }
        }
        try {
            if (jsonBody != null) {
                OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { w ->
                    w.write(jsonBody)
                    w.flush()
                }
            }
            val code = conn.responseCode
            val stream = if (code in 200..299) conn.inputStream else conn.errorStream
            val text = stream?.bufferedReader(Charsets.UTF_8)?.use(BufferedReader::readText) ?: ""
            if (code == 401) {
                clearSession()
                throw SessionExpiredException("Sesi 24 jam habis")
            }
            if (code !in 200..299) {
                val msg = try {
                    JSONObject(text).optJSONObject("error")?.optString("message")
                } catch (_: Exception) {
                    null
                }
                throw Exception(msg?.takeIf { it.isNotBlank() } ?: "HTTP $code")
            }
            return text
        } finally {
            conn.disconnect()
        }
    }
}
