package dev.legato.mobile

import org.junit.Assert.*
import org.junit.Test

class AppThemeTest {
    private fun catalog() = ThemeCatalog(javaClass.getResource("/palettes.json")!!.readText())

    @Test
    fun `selected themes keep semantic inks readable on every material and selection`() {
        val settings = ThemeSettings(catalog(), { null }, { true })
        val roles = org.json.JSONObject(javaClass.getResource("/palettes.json")!!.readText())
            .getJSONObject("material_ink_contract").getJSONArray("roles")
        for (id in listOf("dracula", "ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid")) {
            assertTrue(settings.select(id))
            val theme = settings.current
            val backgrounds = listOf(theme.pageBackground, theme.tokens.getValue("selection")) +
                theme.materials.filterKeys { it != "divider" }.values
            fun luminance(hex: String): Double {
                val channels = listOf(1, 3, 5).map { offset ->
                    val byte = hex.substring(offset, offset + 2).toInt(16) / 255.0
                    if (byte <= .04045) byte / 12.92 else Math.pow((byte + .055) / 1.055, 2.4)
                }
                return channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722
            }
            for (index in 0 until roles.length()) {
                val role = roles.getString(index)
                val ink = luminance(theme.inks.getValue(role))
                for (background in backgrounds) {
                    val surface = luminance(background)
                    assertTrue("$id $role on $background (${theme.inks.getValue(role)})",
                        (maxOf(ink, surface) + .05) / (minOf(ink, surface) + .05) >= 4.5)
                }
            }
            for (role in listOf("canvas", "surface", "selection", "border", "focus")) {
                assertEquals("$id $role", theme.tokens.getValue(role), theme.inks.getValue(role))
            }
            assertEquals(theme.tokens.getValue("canvas"), theme.pageBackground)
            assertNotEquals(theme.inks.getValue("text"), theme.inks.getValue("muted"))
        }
        assertTrue(settings.select("legato"))
        assertTrue(settings.current.inks.isEmpty())
    }

    @Test
    fun `selected themes expose distinct materials from the shared readable recipe`() {
        val json = org.json.JSONObject(javaClass.getResource("/palettes.json")!!.readText())
        val settings = ThemeSettings(catalog(), { null }, { true })
        assertTrue(settings.current.materials.isEmpty())
        for (id in listOf("dracula", "ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid")) {
            assertTrue(settings.select(id))
            val theme = settings.current
            val recipe = json.getJSONObject("materials")
                .getJSONObject(if (theme.light) "light" else "dark").getJSONObject("colors")
            val entries = json.getJSONArray("themes")
            val ui = (0 until entries.length()).map { entries.getJSONObject(it) }
                .first { it.getString("id") == id }.getJSONObject("ui")
            val surfaces = ui.getJSONObject("surfaces")
            val roles = recipe.keys().asSequence().toSet() + surfaces.keys().asSequence().toSet()
            val expected = roles.associateWith { role ->
                val mix = if (surfaces.has(role)) surfaces.getJSONObject(role) else recipe.getJSONObject(role)
                fun resolve(key: String): Int {
                    val reference = mix.getString(key)
                    return (theme.tokens[reference] ?: reference).removePrefix("#").toInt(16)
                }
                val base = resolve("base")
                val tint = resolve("tint")
                val amount = mix.getDouble("amount")
                val rgb = listOf(16, 8, 0).fold(0) { color, shift ->
                    val a = (base shr shift) and 255
                    val b = (tint shr shift) and 255
                    val channel = kotlin.math.floor(a * (1 - amount) + b * amount + 0.5).toInt()
                    (color shl 8) or channel
                }
                "#%06X".format(rgb)
            }
            assertEquals(id, expected, theme.materials)
            assertEquals(4, listOf(theme.pageBackground, theme.materials.getValue("rail"),
                theme.materials.getValue("panel"), theme.materials.getValue("raised")).toSet().size)
            assertNotEquals(theme.tokens.getValue("border"), theme.materials.getValue("divider"))
            assertNotEquals(theme.materials.getValue("panel"), theme.materials.getValue("errorSurface"))
            fun luminance(hex: String): Double {
                val channels = listOf(1, 3, 5).map { offset ->
                    val byte = hex.substring(offset, offset + 2).toInt(16) / 255.0
                    if (byte <= .04045) byte / 12.92 else Math.pow((byte + .055) / 1.055, 2.4)
                }
                return channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722
            }
            val boundary = luminance(theme.tokens.getValue("border"))
            for (background in listOf(theme.pageBackground, theme.materials.getValue("panel"), theme.materials.getValue("raised"))) {
                val surface = luminance(background)
                assertTrue("$id control boundary on $background",
                    (maxOf(boundary, surface) + .05) / (minOf(boundary, surface) + .05) >= 3)
            }
        }
        assertTrue(settings.select("legato"))
        assertTrue(settings.current.materials.isEmpty())
    }

    @Test
    fun `choosing a canonical theme survives a new settings owner`() {
        var saved: String? = null
        val settings = ThemeSettings(catalog(), { saved }, { saved = it; true })
        assertEquals("legato", settings.current.id)
        assertTrue(settings.select("ocular-light"))
        val restarted = ThemeSettings(catalog(), { saved }, { saved = it; true })
        assertEquals("ocular-light", restarted.current.id)
        assertTrue(restarted.current.light)
        assertEquals("#F5F3ED", restarted.current.tokens["canvas"])
        assertEquals("#252B33", restarted.current.tokens["text"])
        assertFalse(restarted.select("unknown"))
        assertEquals("ocular-light", restarted.current.id)
    }
    @Test
    fun `keyed page requests save before delivery and acknowledge storage failures`() {
        var saved: String? = null
        var canSave = true
        var writeError: Exception? = null
        val events = mutableListOf<String>()
        val settings = ThemeSettings(catalog(), { saved }, {
            events += "save:$it"
            writeError?.let { throw it }
            if (canSave) saved = it
            canSave
        })
        val replies = mutableListOf<Pair<String, String?>>()
        val bridge = ThemeBridge("secret", { id, done ->
            val result = runCatching {
                check(settings.select(id)) { "Invalid theme" }
                events += "apply:${settings.current.id}"
            }
            done(result.exceptionOrNull()?.message)
        }, { id, error ->
            events += "reply:$id"
            replies += id to error
        })
        fun request(key: String, id: String) = bridge.handle(PageMessage.parse(
            """{"legatoShell":"theme","type":"theme","key":"$key","id":"$id"}"""))
        request("wrong", "ocular-light")
        for (data in listOf(
            """{"legatoShell":"theme","id":"ocular-light"}""",
            """{"legatoShell":"theme","key":"secret","id":7}""",
            """{"legatoShell":"theme","key":"secret","theme":"dracula"}""",
        )) bridge.handle(PageMessage.parse(data))
        assertTrue(events.isEmpty())
        for (id in listOf("legato", "ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid", "dracula")) {
            assertTrue(id, validThemeId(id))
            request("secret", id)
            assertEquals(listOf("save:$id", "apply:$id", "reply:$id"), events)
            assertEquals(id to null, replies.last())
            assertEquals(id, ThemeSettings(catalog(), { saved }, { true }).current.id)
            events.clear()
        }
        request("secret", "arbitrary")
        assertEquals(listOf("reply:arbitrary"), events)
        assertNotNull(replies.last().second)
        events.clear()
        canSave = false
        request("secret", "ocular-light")
        assertEquals(listOf("save:ocular-light", "reply:ocular-light"), events)
        assertNotNull(replies.last().second)
        assertEquals("dracula", settings.current.id)
        assertEquals("dracula", saved)
        // A repeated request must still confirm persistence, not assume success.
        request("secret", "dracula")
        assertNotNull(replies.last().second)
        events.clear()
        writeError = java.io.IOException("Storage unavailable")
        request("secret", "ocular-dark")
        assertEquals(listOf("save:ocular-dark", "reply:ocular-dark"), events)
        assertEquals("ocular-dark" to "Storage unavailable", replies.last())
        assertEquals("dracula", settings.current.id)
    }

    @Test
    fun `every picker choice resolves the packaged canonical tokens and polarity`() {
        val json = javaClass.getResource("/palettes.json")!!.readText()
        val entries = org.json.JSONObject(json).getJSONArray("themes")
        val settings = ThemeSettings(ThemeCatalog(json), { "retired-theme" }, { true })
        assertEquals("legato", settings.current.id)
        assertFalse(settings.current.light)
        assertTrue(settings.current.tokens.isEmpty())
        assertTrue(settings.current.ui.isEmpty())
        assertEquals(setOf("legato", "dracula", "ocular-light", "ocular-dark", "ocular-light-vivid", "ocular-dark-vivid"), catalog().themes.map { it.id }.toSet())
        for (i in 0 until entries.length()) {
            val entry = entries.getJSONObject(i)
            assertTrue(settings.select(entry.getString("id")))
            assertEquals(entry.getString("polarity") == "light", settings.current.light)
            val tokens = entry.getJSONObject("tokens")
            val expected = tokens.keys().asSequence().associateWith { tokens.getString(it) }.toMutableMap()
            if (entry.getString("id") == "dracula") {
                expected["muted"] = tokens.getString("text")
                expected["comment"] = tokens.getString("text")
                expected["border"] = tokens.getString("focus")
            }
            assertEquals(expected, settings.current.tokens)
            assertEquals(tokens.getString("canvas"), settings.current.pageBackground)
            val theme = settings.current
            val dracula = theme.id == "dracula"
            assertEquals(theme.inks.getValue(if (dracula) "focus" else "keyword"), theme.ui.getValue("accent"))
            assertEquals(theme.inks.getValue(if (dracula) "link" else "keyword"), theme.ui.getValue("identity"))
            assertEquals(theme.inks.getValue(if (dracula) "success" else "keyword"), theme.ui.getValue("voice_start"))
            assertEquals(tokens.getString("focus"), theme.tokens.getValue("focus"))
            assertEquals(tokens.getString("link"), theme.tokens.getValue("link"))
            if (!dracula) {
                assertNotEquals(theme.tokens.getValue("focus"), theme.ui.getValue("accent"))
                assertEquals(theme.tokens.getValue("link"), theme.inks.getValue("link"))
            }
        }
        assertTrue(settings.select("legato"))
        assertTrue(settings.current.tokens.isEmpty())
        assertTrue(settings.current.ui.isEmpty())
        assertEquals("#0A0A0F", settings.current.pageBackground)
    }
}
