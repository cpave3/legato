package dev.legato.mobile

import org.json.JSONObject
import kotlin.math.pow

// Tokens come from the repository's canonical palettes.json, packaged as an asset.
data class AppTheme(
    val id: String,
    val name: String,
    val light: Boolean,
    val tokens: Map<String, String>,
    val materials: Map<String, String> = emptyMap(),
    val inks: Map<String, String> = tokens,
    val ui: Map<String, String> = emptyMap(),
) {
    val pageBackground: String get() = tokens["canvas"] ?: "#0A0A0F"
}

class ThemeCatalog(json: String) {
    private val source = JSONObject(json)
    val themes: List<AppTheme> = listOf(AppTheme("legato", "Legato", false, emptyMap())) +
        source.getJSONArray("themes").let { entries ->
            (0 until entries.length()).map { index ->
                val entry = entries.getJSONObject(index)
                val tokens = entry.getJSONObject("tokens")
                val original = tokens.keys().asSequence().associateWith { tokens.getString(it) }
                val readable = readableTokens(original)
                val polarity = entry.getString("polarity")
                val recipe = source.getJSONObject("materials").getJSONObject(polarity).getJSONObject("colors")
                val ui = entry.getJSONObject("ui")
                val materials = materialColors(recipe, readable) +
                    materialColors(ui.getJSONObject("surfaces"), readable)
                val inks = materialInks(source.getJSONObject("material_ink_contract"), readable, materials, original)
                AppTheme(entry.getString("id"), entry.getString("name"), polarity == "light",
                    readable, materials, inks,
                    listOf("accent", "identity", "voice_start").associateWith { inks.getValue(ui.getString(it)) })
            }
        }

    fun find(id: String?): AppTheme? = themes.find { it.id == id }
}

// Blend encoded sRGB bytes, matching Math.round in the shared recipe (not linear-light interpolation).
private fun materialColors(recipe: JSONObject, tokens: Map<String, String>): Map<String, String> =
    recipe.keys().asSequence().associateWith { role ->
        val mix = recipe.getJSONObject(role)
        fun resolve(key: String): String = mix.getString(key).let { tokens[it] ?: it }
        mixColor(resolve("base"), resolve("tint"), mix.getDouble("amount"))
    }

private fun mixColor(base: String, tint: String, amount: Double): String =
    "#" + listOf(1, 3, 5).joinToString("") { offset ->
        val a = base.substring(offset, offset + 2).toInt(16)
        val b = tint.substring(offset, offset + 2).toInt(16)
        "%02X".format(java.lang.Math.round(a + (b - a) * amount))
    }

private fun materialInks(
    contract: JSONObject,
    tokens: Map<String, String>,
    materials: Map<String, String>,
    original: Map<String, String>,
): Map<String, String> = tokens.toMutableMap().apply {
    val backgrounds = listOf(tokens.getValue("canvas"), tokens.getValue("selection")) +
        materials.filterKeys { it != "divider" }.values
    val roles = contract.getJSONArray("roles")
    val blendStep = contract.getDouble("blend_step")
    val minimumContrast = contract.getDouble("minimum_contrast")
    for (index in 0 until roles.length()) {
        val role = roles.getString(index)
        for (step in 0..java.lang.Math.round(1 / blendStep).toInt()) {
            val ink = mixColor(original.getValue(role), tokens.getValue("text"), minOf(1.0, step * blendStep))
            if (backgrounds.all { contrast(ink, it) >= minimumContrast }) {
                put(role, ink)
                break
            }
        }
    }
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

private val THEME_IDS = setOf("legato", "dracula", "ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid")

fun validThemeId(id: String): Boolean = id in THEME_IDS

class ThemeBridge(
    private val key: String,
    private val select: (String, (String?) -> Unit) -> Unit,
    private val tell: (String, String?) -> Unit,
) {
    fun handle(message: PageMessage) {
        if (message !is PageMessage.Theme || message.key != key) return
        if (message.id != "server" && !validThemeId(message.id)) {
            tell(message.id, "Invalid theme ID")
            return
        }
        select(message.id) { error -> tell(message.id, error) }
    }
}
