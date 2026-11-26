package com.karouter.mobile.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.karouter.mobile.data.*
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainApp(
    client: KaRouterClient,
    dark: Boolean,
    onToggleTheme: () -> Unit,
    onSessionExpired: (String) -> Unit
) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbarHostState = remember { SnackbarHostState() }

    var tab by remember { mutableIntStateOf(0) }
    var snap by remember { mutableStateOf<RouterSnapshot?>(null) }
    var loading by remember { mutableStateOf(false) }
    var remainingSec by remember { mutableLongStateOf((client.expiresAt - System.currentTimeMillis() / 1000L).coerceAtLeast(0L)) }

    var showAddKeyDialog by remember { mutableStateOf(false) }
    var showCreateClientDialog by remember { mutableStateOf(false) }
    var revealedSecret by remember { mutableStateOf<String?>(null) }
    var logErrorOnly by remember { mutableStateOf(false) }

    fun toast(msg: String) {
        val clipped = msg.replace(Regex("\\s+"), " ").trim().let {
            if (it.length > 72) it.take(71) + "…" else it
        }
        scope.launch {
            snackbarHostState.currentSnackbarData?.dismiss()
            snackbarHostState.showSnackbar(clipped, duration = SnackbarDuration.Short)
        }
    }

    fun copyToClipboard(text: String, msg: String = "Disalin") {
        val cm = ctx.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        cm.setPrimaryClip(ClipData.newPlainText("KaRouter", text))
        toast(msg)
    }

    suspend fun refresh(silent: Boolean = false) {
        if (!client.hasValidSession()) {
            onSessionExpired("Sesi 24 jam berakhir")
            return
        }
        if (!silent) loading = true
        try {
            snap = client.fetchStatus()
        } catch (_: SessionExpiredException) {
            onSessionExpired("Sesi 24 jam berakhir")
        } catch (e: Exception) {
            if (!silent) toast(e.message ?: "Gagal memuat")
        } finally {
            if (!silent) loading = false
        }
    }

    LaunchedEffect(Unit) {
        refresh()
    }

    LaunchedEffect(client.expiresAt) {
        while (true) {
            val rem = client.expiresAt - (System.currentTimeMillis() / 1000L)
            remainingSec = rem.coerceAtLeast(0L)
            if (rem <= 0L) {
                onSessionExpired("Sesi 24 jam berakhir")
                break
            }
            delay(15000)
            refresh(silent = true)
        }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbarHostState) },
        topBar = {
            TopAppBar(
                title = { Text("KaRouter", fontWeight = FontWeight.Bold) },
                actions = {
                    IconButton(onClick = { scope.launch { refresh() } }, enabled = !loading) {
                        Icon(Icons.Rounded.Refresh, contentDescription = "Muat ulang")
                    }
                    IconButton(onClick = onToggleTheme) {
                        Icon(
                            imageVector = if (dark) Icons.Rounded.LightMode else Icons.Rounded.DarkMode,
                            contentDescription = "Tema"
                        )
                    }
                }
            )
        },
        bottomBar = {
            NavigationBar {
                NavigationBarItem(
                    selected = tab == 0,
                    onClick = { tab = 0 },
                    icon = { Icon(Icons.Rounded.Dashboard, contentDescription = null) },
                    label = { Text("Beranda") }
                )
                NavigationBarItem(
                    selected = tab == 1,
                    onClick = { tab = 1 },
                    icon = { Icon(Icons.Rounded.Storage, contentDescription = null) },
                    label = { Text("Pool") }
                )
                NavigationBarItem(
                    selected = tab == 2,
                    onClick = { tab = 2 },
                    icon = { Icon(Icons.Rounded.Key, contentDescription = null) },
                    label = { Text("Akses") }
                )
                NavigationBarItem(
                    selected = tab == 3,
                    onClick = { tab = 3 },
                    icon = { Icon(Icons.Rounded.Tune, contentDescription = null) },
                    label = { Text("Atur") }
                )
            }
        }
    ) { padding ->
        Box(modifier = Modifier.fillMaxSize().padding(padding)) {
            val s = snap
            if (s == null) {
                Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator()
                }
            } else {
                when (tab) {
                    0 -> HomeTab(
                        snap = s,
                        onRotationChange = { mode ->
                            scope.launch {
                                try {
                                    client.updateRouterMode(rotationMode = mode)
                                    refresh(silent = true)
                                    toast("Mode disimpan")
                                } catch (e: Exception) {
                                    toast(e.message ?: "Gagal")
                                }
                            }
                        },
                        onSelectionChange = { sel ->
                            scope.launch {
                                try {
                                    client.updateRouterMode(keySelection = sel)
                                    refresh(silent = true)
                                    toast("Seleksi disimpan")
                                } catch (e: Exception) {
                                    toast(e.message ?: "Gagal")
                                }
                            }
                        },
                        onOpenAddKey = { showAddKeyDialog = true }
                    )
                    1 -> PoolTab(
                        snap = s,
                        onOpenAddKey = { showAddKeyDialog = true },
                        onTestKey = { id ->
                            scope.launch {
                                try {
                                    val res = client.testKey(id)
                                    refresh(silent = true)
                                    toast(res)
                                } catch (e: Exception) {
                                    toast(e.message ?: "Gagal")
                                }
                            }
                        },
                        onReviveKey = { id ->
                            scope.launch {
                                try {
                                    client.reviveKey(id)
                                    refresh(silent = true)
                                    toast("Key dipulihkan")
                                } catch (e: Exception) {
                                    toast(e.message ?: "Gagal")
                                }
                            }
                        },
                        onDeleteKey = { id ->
                            scope.launch {
                                try {
                                    client.deleteKey(id)
                                    refresh(silent = true)
                                    toast("Key dihapus")
                                } catch (e: Exception) {
                                    toast(e.message ?: "Gagal")
                                }
                            }
                        }
                    )
                    2 -> AccessTab(
                        snap = s,
                        revealedSecret = revealedSecret,
                        onDismissSecret = { revealedSecret = null },
                        onCopy = { txt, msg -> copyToClipboard(txt, msg) },
                        onOpenCreate = { showCreateClientDialog = true },
                        onDelete = { id ->
                            scope.launch {
                                try {
                                    client.deleteClientKey(id)
                                    refresh(silent = true)
                                    toast("Dihapus")
                                } catch (e: Exception) {
                                    toast(e.message ?: "Gagal")
                                }
                            }
                        }
                    )
                    3 -> SettingsTab(
                        snap = s,
                        serverUrl = client.baseUrl,
                        remainingSec = remainingSec,
                        logErrorOnly = logErrorOnly,
                        onToggleLogFilter = { logErrorOnly = it },
                        onRunBackup = {
                            scope.launch {
                                try {
                                    toast(client.runNeonBackup())
                                    refresh(silent = true)
                                } catch (e: Exception) {
                                    toast(e.message ?: "Backup gagal")
                                }
                            }
                        },
                        onLogout = {
                            scope.launch {
                                client.logout()
                                onSessionExpired("Keluar")
                            }
                        }
                    )
                }
            }
        }
    }

    val currentSnap = snap
    if (showAddKeyDialog && currentSnap != null) {
        MobileAddKeysDialog(
            providers = currentSnap.providers,
            existingKeys = currentSnap.keys,
            ctx = ctx,
            onDismiss = { showAddKeyDialog = false },
            onSubmit = { provId, prefix, items, onErr ->
                scope.launch {
                    try {
                        val (added, dups) = client.addKeys(provId, prefix, items)
                        showAddKeyDialog = false
                        refresh(silent = true)
                        toast(if (dups > 0) "$added ditambah ($dups duplikat)" else "$added key ditambah")
                    } catch (e: Exception) {
                        onErr(e.message ?: "Gagal simpan")
                    }
                }
            }
        )
    }

    if (showCreateClientDialog && currentSnap != null) {
        MobileCreateClientKeyDialog(
            providers = currentSnap.providers,
            onDismiss = { showCreateClientDialog = false },
            onSubmit = { name, scopeVal, onErr ->
                scope.launch {
                    try {
                        revealedSecret = client.createClientKey(name, scopeVal)
                        showCreateClientDialog = false
                        refresh(silent = true)
                        toast("Akses dibuat")
                    } catch (e: Exception) {
                        onErr(e.message ?: "Gagal buat")
                    }
                }
            }
        )
    }
}

@Composable
private fun HomeTab(
    snap: RouterSnapshot,
    onRotationChange: (String) -> Unit,
    onSelectionChange: (String) -> Unit,
    onOpenAddKey: () -> Unit
) {
    val routeMs = String.format("%.2f", snap.avgRouteUs / 1000.0)
    val okPct = if (snap.totalRequests > 0) (snap.totalSuccess * 100 / snap.totalRequests) else 100

    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        item {
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                KpiBox("Provider", "${snap.totalProviders}", "${snap.enabledProviders} aktif", Modifier.weight(1f))
                KpiBox("Key Siap", "${snap.eligibleKeys}", "dari ${snap.totalKeys}", Modifier.weight(1f))
            }
        }
        item {
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                KpiBox("Rotasi", "$routeMs ms", "${snap.avgRouteUs} µs RAM", Modifier.weight(1f))
                KpiBox("Request", "${snap.totalRequests}", "$okPct% OK", Modifier.weight(1f))
            }
        }
        item {
            Card(shape = RoundedCornerShape(18.dp)) {
                Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text("Mode Rotasi", fontWeight = FontWeight.Bold)
                        FilledTonalButton(onClick = onOpenAddKey) {
                            Icon(Icons.Rounded.Add, contentDescription = null)
                            Spacer(Modifier.width(4.dp))
                            Text("Key")
                        }
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        listOf("weighted" to "Seimbang", "priority" to "Prioritas", "model_prefix" to "Prefix").forEach { (id, lbl) ->
                            FilterChip(
                                selected = snap.rotationMode == id,
                                onClick = { onRotationChange(id) },
                                label = { Text(lbl) }
                            )
                        }
                    }
                    Text("Seleksi Key", style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.SemiBold)
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(listOf("smart" to "Otomatis", "random" to "Acak", "lru_jitter" to "LRU", "round_robin" to "Giliran")) { (id, lbl) ->
                            FilterChip(
                                selected = snap.keySelection == id,
                                onClick = { onSelectionChange(id) },
                                label = { Text(lbl) }
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun KpiBox(title: String, value: String, sub: String, modifier: Modifier) {
    Card(modifier = modifier, shape = RoundedCornerShape(18.dp)) {
        Column(modifier = Modifier.padding(14.dp)) {
            Text(title.uppercase(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.height(4.dp))
            Text(value, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            Text(sub, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun PoolTab(
    snap: RouterSnapshot,
    onOpenAddKey: () -> Unit,
    onTestKey: (String) -> Unit,
    onReviveKey: (String) -> Unit,
    onDeleteKey: (String) -> Unit
) {
    var selectedProv by remember { mutableStateOf("all") }
    val filteredKeys = remember(snap.keys, selectedProv) {
        if (selectedProv == "all") snap.keys else snap.keys.filter { it.provider == selectedProv }
    }

    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp)
    ) {
        item {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("Key Pool", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                Button(onClick = onOpenAddKey, shape = RoundedCornerShape(14.dp)) {
                    Icon(Icons.Rounded.Add, contentDescription = null)
                    Spacer(Modifier.width(4.dp))
                    Text("Tambah")
                }
            }
        }
        item {
            LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                item {
                    FilterChip(
                        selected = selectedProv == "all",
                        onClick = { selectedProv = "all" },
                        label = { Text("Semua (${snap.keys.size})") }
                    )
                }
                items(snap.providers) { pr ->
                    FilterChip(
                        selected = selectedProv == pr.id,
                        onClick = { selectedProv = pr.id },
                        label = { Text("${pr.name} ${pr.eligibleKeys}/${pr.totalKeys}") }
                    )
                }
            }
        }
        items(filteredKeys, key = { it.id }) { k ->
            Card(shape = RoundedCornerShape(16.dp)) {
                Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text("${k.label} · ${k.provider}", fontWeight = FontWeight.Bold)
                            Text(k.masked, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
                        }
                        val statusColor = when (k.status) {
                            "healthy" -> OkGreen
                            "cooldown" -> WarnAmber
                            "dead" -> ErrRed
                            else -> MaterialTheme.colorScheme.onSurfaceVariant
                        }
                        val statusTxt = when (k.status) {
                            "healthy" -> "Siap"
                            "cooldown" -> if (k.consecFails > 0) "Gagal ${k.consecFails}/5" else "${k.cooldownRemainingSec}d"
                            "dead" -> "Mati (${k.consecFails}/5)"
                            else -> "Jeda"
                        }
                        Text(statusTxt, color = statusColor, fontWeight = FontWeight.Bold, style = MaterialTheme.typography.labelMedium)
                    }
                    if (k.lastError.isNotBlank()) {
                        Text(
                            k.lastError,
                            color = MaterialTheme.colorScheme.error,
                            style = MaterialTheme.typography.bodySmall,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis
                        )
                    }
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = { onTestKey(k.id) }, contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp)) {
                            Text("Uji")
                        }
                        if (k.status == "dead" || k.status == "cooldown" || k.consecFails > 0) {
                            OutlinedButton(onClick = { onReviveKey(k.id) }, contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp)) {
                                Text("Pulih")
                            }
                        }
                        TextButton(onClick = { onDeleteKey(k.id) }) {
                            Text("Hapus", color = MaterialTheme.colorScheme.error)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun AccessTab(
    snap: RouterSnapshot,
    revealedSecret: String?,
    onDismissSecret: () -> Unit,
    onCopy: (String, String) -> Unit,
    onOpenCreate: () -> Unit,
    onDelete: (String) -> Unit
) {
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp)
    ) {
        item {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("Key Akses", style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                Button(onClick = onOpenCreate, shape = RoundedCornerShape(14.dp)) {
                    Icon(Icons.Rounded.Add, contentDescription = null)
                    Spacer(Modifier.width(4.dp))
                    Text("Buat")
                }
            }
        }
        if (!revealedSecret.isNullOrBlank()) {
            item {
                Card(shape = RoundedCornerShape(16.dp)) {
                    Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text("Key Baru", fontWeight = FontWeight.Bold)
                        Text(revealedSecret, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Button(onClick = { onCopy(revealedSecret, "Key disalin") }) { Text("Salin") }
                            OutlinedButton(onClick = onDismissSecret) { Text("Tutup") }
                        }
                    }
                }
            }
        }
        items(snap.clientKeys, key = { it.id }) { ck ->
            Card(shape = RoundedCornerShape(16.dp)) {
                Column(modifier = Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text(ck.name, fontWeight = FontWeight.Bold)
                        Text(ck.scope, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
                    }
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text(ck.masked, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
                        Text("${ck.usageCount} req", style = MaterialTheme.typography.bodySmall)
                    }
                    TextButton(onClick = { onDelete(ck.id) }) {
                        Text("Hapus", color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
    }
}

@Composable
private fun SettingsTab(
    snap: RouterSnapshot,
    serverUrl: String,
    remainingSec: Long,
    logErrorOnly: Boolean,
    onToggleLogFilter: (Boolean) -> Unit,
    onRunBackup: () -> Unit,
    onLogout: () -> Unit
) {
    val hours = remainingSec / 3600
    val mins = (remainingSec % 3600) / 60
    val filteredLogs = remember(snap.logs, logErrorOnly) {
        if (logErrorOnly) snap.logs.filter { it.status >= 400 || it.error.isNotBlank() } else snap.logs
    }

    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        item {
            Card(shape = RoundedCornerShape(18.dp)) {
                Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Sesi JWT", fontWeight = FontWeight.Bold)
                    Text("Endpoint: $serverUrl", style = MaterialTheme.typography.bodySmall)
                    Text("Logout otomatis: ${hours}j ${mins}m tersisa", style = MaterialTheme.typography.bodySmall)
                    Button(
                        onClick = onLogout,
                        colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.error),
                        shape = RoundedCornerShape(14.dp)
                    ) {
                        Text("Keluar")
                    }
                }
            }
        }
        item {
            Card(shape = RoundedCornerShape(18.dp)) {
                Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Backup Neon", fontWeight = FontWeight.Bold)
                    Text(
                        if (snap.backup.hasNeonUrl) snap.backup.neonUrlMasked else "Belum diatur",
                        style = MaterialTheme.typography.bodySmall
                    )
                    if (snap.backup.lastSummary.isNotBlank()) {
                        Text(snap.backup.lastSummary, style = MaterialTheme.typography.bodySmall, color = OkGreen)
                    }
                    Button(onClick = onRunBackup, shape = RoundedCornerShape(14.dp)) {
                        Text("Backup")
                    }
                }
            }
        }
        item {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text("Riwayat Log", fontWeight = FontWeight.Bold)
                FilterChip(
                    selected = logErrorOnly,
                    onClick = { onToggleLogFilter(!logErrorOnly) },
                    label = { Text(if (logErrorOnly) "Error Saja" else "Semua") }
                )
            }
        }
        items(filteredLogs.take(25), key = { it.id }) { lg ->
            Card(shape = RoundedCornerShape(14.dp)) {
                Column(modifier = Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text("${lg.providerName.ifEmpty { lg.provider }} · ${lg.upstreamModel.ifEmpty { lg.requestedModel }}", fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                        Text(
                            "${lg.status}",
                            color = if (lg.status >= 400) ErrRed else OkGreen,
                            fontWeight = FontWeight.Bold
                        )
                    }
                    if (lg.error.isNotBlank()) {
                        Text(lg.error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}

@Composable
private fun MobileAddKeysDialog(
    providers: List<ProviderItem>,
    existingKeys: List<PoolKeyItem>,
    ctx: Context,
    onDismiss: () -> Unit,
    onSubmit: (String, String, List<ParsedKeyInput>, (String) -> Unit) -> Unit
) {
    var selectedProv by remember { mutableStateOf(providers.firstOrNull()?.id ?: "") }
    var prefix by remember { mutableStateOf("") }
    var importMode by remember { mutableStateOf(false) }
    var rawText by remember { mutableStateOf("") }
    val rows = remember { mutableStateListOf(ParsedKeyInput("", "")) }
    var errMsg by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    val fileLauncher = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri: Uri? ->
        if (uri == null) return@rememberLauncherForActivityResult
        try {
            val content = ctx.contentResolver.openInputStream(uri)?.bufferedReader()?.use { it.readText() } ?: ""
            val parsed = KeyParser.parseSmart(content)
            if (parsed.isEmpty()) {
                errMsg = "File kosong atau format tidak dikenali"
            } else {
                rawText = content
                importMode = true
                errMsg = ""
            }
        } catch (_: Exception) {
            errMsg = "Gagal baca file"
        }
    }

    val effPrefix = KeyParser.cleanPrefix(prefix, selectedProv)
    val startSeq = KeyParser.nextStartIndex(existingKeys, selectedProv, effPrefix)
    val preview = remember(importMode, rawText, rows.toList(), effPrefix, startSeq) {
        if (importMode) {
            KeyParser.parseSmart(rawText).mapIndexed { i, it ->
                ParsedKeyInput(it.key, it.label.ifEmpty { KeyParser.formatSeqLabel(effPrefix, startSeq + i) })
            }
        } else {
            val seen = linkedSetOf<String>()
            val out = mutableListOf<ParsedKeyInput>()
            var seq = 0
            for (r in rows) {
                val k = r.key.trim()
                if (k.isEmpty() || !seen.add(k)) continue
                val lbl = r.label.trim().ifEmpty { KeyParser.formatSeqLabel(effPrefix, startSeq + seq++) }
                out.add(ParsedKeyInput(k, lbl))
            }
            out
        }
    }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Tambah Key (${preview.size})") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                LazyRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    items(providers) { p ->
                        FilterChip(
                            selected = selectedProv == p.id,
                            onClick = { selectedProv = p.id },
                            label = { Text(p.name) }
                        )
                    }
                }
                OutlinedTextField(
                    value = prefix,
                    onValueChange = { prefix = it },
                    label = { Text("Prefix (default: $effPrefix)") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(selected = !importMode, onClick = { importMode = false }, label = { Text("Form") })
                    FilterChip(selected = importMode, onClick = { importMode = true }, label = { Text("Tempel") })
                    OutlinedButton(onClick = { fileLauncher.launch(arrayOf("*/*")) }) {
                        Text("Import File")
                    }
                }
                if (!importMode) {
                    LazyColumn(modifier = Modifier.heightIn(max = 200.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        items(rows.size) { idx ->
                            val seqLbl = KeyParser.formatSeqLabel(effPrefix, startSeq + idx)
                            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                                OutlinedTextField(
                                    value = rows[idx].label,
                                    onValueChange = { rows[idx] = rows[idx].copy(label = it) },
                                    placeholder = { Text(seqLbl) },
                                    singleLine = true,
                                    modifier = Modifier.weight(0.38f)
                                )
                                OutlinedTextField(
                                    value = rows[idx].key,
                                    onValueChange = { nextVal ->
                                        val multi = KeyParser.parseSmart(nextVal)
                                        if (multi.size > 1) {
                                            rows.removeAt(idx)
                                            rows.addAll(idx, multi)
                                        } else {
                                            rows[idx] = rows[idx].copy(key = nextVal)
                                        }
                                    },
                                    placeholder = { Text("Tempel key") },
                                    singleLine = true,
                                    modifier = Modifier.weight(0.62f)
                                )
                            }
                        }
                    }
                    TextButton(onClick = { rows.add(ParsedKeyInput("", "")) }) {
                        Text("+ Baris")
                    }
                } else {
                    OutlinedTextField(
                        value = rawText,
                        onValueChange = { rawText = it },
                        label = { Text("Baris / CSV / JSON / .env") },
                        minLines = 4,
                        maxLines = 6,
                        modifier = Modifier.fillMaxWidth()
                    )
                }
                if (errMsg.isNotBlank()) {
                    Text(errMsg, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            Button(
                enabled = !busy && preview.isNotEmpty(),
                onClick = {
                    busy = true
                    onSubmit(selectedProv, effPrefix, preview) { err ->
                        busy = false
                        errMsg = err
                    }
                }
            ) {
                Text(if (busy) "Proses..." else "Simpan")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("Batal") }
        }
    )
}

@Composable
private fun MobileCreateClientKeyDialog(
    providers: List<ProviderItem>,
    onDismiss: () -> Unit,
    onSubmit: (String, String, (String) -> Unit) -> Unit
) {
    var name by remember { mutableStateOf("") }
    var scope by remember { mutableStateOf("global") }
    var errMsg by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }

    val cats = remember(providers) { providers.map { it.category }.distinct() }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Buat Key") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("Nama") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )
                LazyRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    item {
                        FilterChip(selected = scope == "global", onClick = { scope = "global" }, label = { Text("Global") })
                    }
                    items(cats) { cat ->
                        val valStr = "cat:$cat"
                        FilterChip(selected = scope == valStr, onClick = { scope = valStr }, label = { Text("Grup: $cat") })
                    }
                    items(providers) { p ->
                        FilterChip(selected = scope == p.id, onClick = { scope = p.id }, label = { Text(p.name) })
                    }
                }
                if (errMsg.isNotBlank()) {
                    Text(errMsg, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            Button(
                enabled = !busy && name.isNotBlank(),
                onClick = {
                    busy = true
                    onSubmit(name.trim(), scope) { err ->
                        busy = false
                        errMsg = err
                    }
                }
            ) {
                Text("Buat")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("Batal") }
        }
    )
}
