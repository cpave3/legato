package dev.legato.mobile

data class ThemeChoice(val localId: String, val followServer: Boolean)
data class ThemePreference(val id: String, val revision: Long)
data class ThemeAppearance(val local: AppTheme, val effective: AppTheme, val followServer: Boolean)
data class ThemeRefresh(val connection: String, val generation: Long)

interface AppearanceStore {
    fun readChoice(): ThemeChoice
    fun saveChoice(value: ThemeChoice): Boolean
    fun readPreference(connection: String): ThemePreference?
    fun savePreference(connection: String, value: ThemePreference?): Boolean
}

// One owner for native appearance and page delivery. Persistence can run on the
// serial IO executor; active connection changes and snapshots also take this lock.
class ThemeSettings(private val catalog: ThemeCatalog, private val store: AppearanceStore) {
    private var choice = store.readChoice()
    private var active: String? = null
    private var generation = 0L
    private var preference: ThemePreference? = null

    val followServer: Boolean @Synchronized get() = choice.followServer
    val local: AppTheme @Synchronized get() = catalog.find(choice.localId) ?: catalog.themes.first()
    val current: AppTheme @Synchronized get() =
        if (choice.followServer && active != null) catalog.find(preference?.id) ?: local else local

    @Synchronized fun appearance() = ThemeAppearance(local, current, followServer)

    @Synchronized fun appearanceFor(connection: String): ThemeAppearance {
        val snapshot = if (connection == active) preference else store.readPreference(connection)
        val effective = if (followServer) catalog.find(snapshot?.id) ?: local else local
        return ThemeAppearance(local, effective, followServer)
    }

    @Synchronized fun selectFromPage(connection: String, id: String): Boolean {
        check(active == connection) { "Open this server before changing the app theme." }
        return select(id)
    }

    @Synchronized fun select(id: String): Boolean {
        if (id != "server" && !validThemeId(id)) return false
        val next = if (id == "server") choice.copy(followServer = true) else ThemeChoice(id, false)
        check(store.saveChoice(next)) { "Could not save the appearance choice. Try again." }
        choice = next
        return true
    }

    @Synchronized fun activate(connection: String?) {
        if (active == connection) return
        generation++
        active = connection
        preference = connection?.let(store::readPreference)
    }

    @Synchronized fun refreshTicket(): ThemeRefresh? = active?.let { ThemeRefresh(it, ++generation) }

    @Synchronized fun isCurrent(ticket: ThemeRefresh): Boolean = ticket.connection == active && ticket.generation == generation

    @Synchronized fun completeRefresh(ticket: ThemeRefresh, result: Result<ThemePreference?>): Boolean {
        if (!isCurrent(ticket)) return false
        val value = result.getOrElse {
            if (it is java.io.IOException) return false
            throw it
        }
        return receive(ticket, value)
    }

    @Synchronized fun receive(ticket: ThemeRefresh, value: ThemePreference?): Boolean {
        if (!isCurrent(ticket)) return false
        // Older metadata within the same server cannot roll the preference back.
        if (value != null && preference != null && value.revision < preference!!.revision) return false
        if (value == preference) return true
        check(store.savePreference(ticket.connection, value)) { "Could not save the server's theme preference. Try again." }
        preference = value
        return true
    }

    @Synchronized fun forget(connection: String) {
        check(store.savePreference(connection, null)) { "Could not clear the server's theme preference. Try again." }
        if (active == connection) {
            generation++
            preference = null
        }
    }
}

// The Android adapter commits all fields together; the JVM exercises this same
// serialization boundary with an in-memory key/value store.
class PreferenceAppearanceStore(
    private val read: (String) -> String?,
    private val commit: (Map<String, String?>) -> Boolean,
) : AppearanceStore {
    override fun readChoice() = ThemeChoice(read("theme") ?: "legato", read("theme-mode") == "server")
    override fun saveChoice(value: ThemeChoice) = commit(mapOf(
        "theme" to value.localId, "theme-mode" to if (value.followServer) "server" else "local"))
    override fun readPreference(connection: String): ThemePreference? = read("server-theme:$connection")?.let {
        readThemePreference(org.json.JSONObject(it))
    }
    override fun savePreference(connection: String, value: ThemePreference?) = commit(mapOf(
        "server-theme:$connection" to value?.takeIf { it.id.isNotEmpty() }?.let {
            org.json.JSONObject().put("id", it.id).put("revision", it.revision).toString()
        }))
}

fun readThemePreference(json: org.json.JSONObject): ThemePreference {
    val id = json.opt("id") as? String
    val revision = json.opt("revision")
    check(id != null && (revision is Long || revision is Int) && (revision as Number).toLong() >= 0) {
        "The server's theme preference could not be read"
    }
    return ThemePreference(id, revision.toLong())
}
