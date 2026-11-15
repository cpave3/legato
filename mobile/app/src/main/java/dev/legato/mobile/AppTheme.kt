package dev.legato.mobile

import org.json.JSONObject
import kotlin.math.pow

// Tokens come from the repository's canonical palettes.json, packaged as an asset.
data class AppTheme(val id: String, val name: String, val light: Boolean, val tokens: Map<String, String>) {
    val pageBackground: String get() = tokens["canvas"] ?: "#0A0A0F"
}

class ThemeCatalog(json: String) {
    val themes: List<AppTheme> = listOf(AppTheme("legato", "Legato", false, emptyMap())) +
        JSONObject(json).getJSONArray("themes").let { entries ->
            (0 until entries.length()).map { index ->
                val entry = entries.getJSONObject(index)
                val tokens = entry.getJSONObject("tokens")
                AppTheme(entry.getString("id"), entry.getString("name"), entry.getString("polarity") == "light",
                    readableTokens(tokens.keys().asSequence().associateWith { tokens.getString(it) }))
            }
        }

    fun find(id: String?): AppTheme? = themes.find { it.id == id }
}

// Match themes/render.ts without changing the canonical palette values.
private fun readableTokens(tokens: Map<String, String>): Map<String, String> = tokens.toMutableMap().apply {
    for (role in listOf("muted", "comment")) {
        if (contrast(getValue(role), getValue("canvas")) < 4.5) put(role, getValue("text"))
    }
    if (contrast(getValue("border"), getValue("selection")) < 3) put("border", getValue("focus"))
}

private fun contrast(a: String, b: String): Double {
    fun luminance(hex: String): Double {
        val rgb = listOf(1, 3, 5).map { offset ->
            val value = hex.substring(offset, offset + 2).toInt(16) / 255.0
            if (value <= .04045) value / 12.92 else ((value + .055) / 1.055).pow(2.4)
        }
        return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722
    }
    val first = luminance(a)
    val second = luminance(b)
    return (maxOf(first, second) + .05) / (minOf(first, second) + .05)
}

private val THEME_IDS = setOf("legato", "dracula", "ocular-light", "ocular-dark")

fun validThemeId(id: String): Boolean = id in THEME_IDS

class ThemeSettings(private val catalog: ThemeCatalog, read: () -> String?, private val write: (String) -> Boolean) {
    var current: AppTheme = catalog.find(read()) ?: catalog.themes.first()
        private set

    fun select(id: String): Boolean {
        if (!validThemeId(id)) return false
        val theme = catalog.find(id) ?: return false
        check(write(id)) { "Could not save the appearance choice. Try again." }
        current = theme
        return true
    }
}

class ThemeBridge(
    private val key: String,
    private val select: (String, (String?) -> Unit) -> Unit,
    private val tell: (String, String?) -> Unit,
) {
    fun handle(message: PageMessage) {
        if (message !is PageMessage.Theme || message.key != key) return
        if (!validThemeId(message.id)) {
            tell(message.id, "Invalid theme ID")
            return
        }
        select(message.id) { error -> tell(message.id, error) }
    }
}
