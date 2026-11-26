package com.karouter.mobile.data

import org.json.JSONArray
import org.json.JSONObject

object KeyParser {
    fun cleanPrefix(rawPrefix: String, providerId: String): String {
        val src = (rawPrefix.trim().ifEmpty { providerId.ifEmpty { "key" } })
            .lowercase()
            .replace(Regex("[^a-z0-9_-]+"), "")
        return src.ifEmpty { "key" }
    }

    fun nextStartIndex(existingKeys: List<PoolKeyItem>, providerId: String, prefix: String): Int {
        var maxNum = 0
        val lowerPrefix = prefix.lowercase()
        for (k in existingKeys) {
            if (k.provider != providerId) continue
            val lbl = k.label.trim().lowercase()
            if (!lbl.startsWith(lowerPrefix)) continue
            val rest = lbl.removePrefix(lowerPrefix).trimStart('#', '_', '-', ' ')
            val n = rest.toIntOrNull()
            if (n != null && n > maxNum) {
                maxNum = n
            }
        }
        return maxNum + 1
    }

    fun formatSeqLabel(prefix: String, seq: Int): String {
        return prefix + seq.toString().padStart(2, '0')
    }

    fun parseSmart(raw: String): List<ParsedKeyInput> {
        val txt = raw.trim()
        if (txt.isEmpty()) return emptyList()
        val out = mutableListOf<ParsedKeyInput>()
        val seen = linkedSetOf<String>()

        fun pushItem(keyRaw: String, labelRaw: String = "") {
            val cleaned = keyRaw.trim().trim('[', ']', '"', '\'', '`', ',', ';')
            if (cleaned.isEmpty() || cleaned.startsWith("#") || cleaned.length < 4 || !seen.add(cleaned)) {
                return
            }
            out.add(ParsedKeyInput(key = cleaned, label = labelRaw.trim()))
        }

        if (txt.startsWith("[") || txt.startsWith("{")) {
            try {
                val arr: JSONArray? = if (txt.startsWith("[")) {
                    JSONArray(txt)
                } else {
                    JSONObject(txt).optJSONArray("keys")
                }
                if (arr != null) {
                    for (i in 0 until arr.length()) {
                        val el = arr.get(i)
                        if (el is String) {
                            pushItem(el)
                        } else if (el is JSONObject) {
                            val k = el.optString("key").ifEmpty {
                                el.optString("api_key").ifEmpty { el.optString("token") }
                            }
                            val l = el.optString("label").ifEmpty { el.optString("name") }
                            pushItem(k, l)
                        }
                    }
                    if (out.isNotEmpty()) return out
                }
            } catch (_: Exception) {
            }
        }

        for (rawLine in txt.split(Regex("\r?\n"))) {
            val line = rawLine.trim()
            if (line.isEmpty() || line.startsWith("#") || line.startsWith("//")) continue

            if (line.contains("=") && !line.lowercase().startsWith("http")) {
                val idx = line.indexOf('=')
                val envVal = line.substring(idx + 1).trim()
                if (envVal.isNotEmpty()) {
                    pushItem(envVal)
                    continue
                }
            }

            if (line.contains(";") || line.count { it == ',' } > 1) {
                for (seg in line.split(Regex("[;,]+"))) {
                    pushItem(seg)
                }
                continue
            }

            if (line.contains(",")) {
                val parts = line.split(",", limit = 2).map { it.trim() }
                if (parts.size == 2 && parts[0].isNotEmpty() && parts[1].isNotEmpty()) {
                    val aLooksKey = parts[0].length >= 16 || parts[0].startsWith("AIza") || parts[0].startsWith("sk-")
                    val bLooksKey = parts[1].length >= 16 || parts[1].startsWith("AIza") || parts[1].startsWith("sk-")
                    if (!aLooksKey && bLooksKey) {
                        pushItem(parts[1], parts[0])
                    } else if (aLooksKey && !bLooksKey) {
                        pushItem(parts[0], parts[1])
                    } else {
                        pushItem(parts[0])
                        pushItem(parts[1])
                    }
                    continue
                }
            }

            val tokens = line.split(Regex("\\s+")).filter { it.isNotEmpty() }
            if (tokens.size > 1 && tokens.all { it.length >= 8 }) {
                for (tok in tokens) pushItem(tok)
                continue
            }
            if (tokens.isNotEmpty()) {
                pushItem(tokens[0])
            }
        }
        return out
    }
}
