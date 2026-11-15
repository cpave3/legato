package dev.legato.mobile

import org.junit.Assert.*
import org.junit.Test

class MemoryAppearance : AppearanceStore {
    var choice = ThemeChoice("legato", false)
    var writable = true
    val cache = mutableMapOf<String, ThemePreference>()
    override fun readChoice() = choice
    override fun saveChoice(value: ThemeChoice): Boolean {
        if (writable) choice = value
        return writable
    }
    override fun readPreference(connection: String) = cache[connection]
    override fun savePreference(connection: String, value: ThemePreference?): Boolean {
        if (!writable) return false
        if (value == null) cache.remove(connection) else cache[connection] = value
        return true
    }
}

class FollowThemeTest {
    private fun settings(store: AppearanceStore) = ThemeSettings(
        ThemeCatalog(javaClass.getResource("/palettes.json")!!.readText()), store)

    @Test fun `switching uses only own cache and rejects inactive or stale refreshes`() {
        val owner = settings(MemoryAppearance())
        owner.select("server")
        owner.activate("a")
        val old = owner.refreshTicket()!!
        val latest = owner.refreshTicket()!!
        assertTrue(owner.receive(latest, ThemePreference("ocular-light", 3)))
        assertFalse(owner.receive(old, ThemePreference("dracula", 4)))
        owner.activate("b")
        assertEquals("legato", owner.current.id)
        assertFalse(owner.receive(latest, ThemePreference("ocular-dark", 5)))
        owner.receive(owner.refreshTicket()!!, ThemePreference("dracula", 1))
        assertEquals("dracula", owner.current.id)
        owner.activate("a")
        assertEquals("ocular-light", owner.current.id)
        assertFalse(owner.completeRefresh(owner.refreshTicket()!!,
            Result.failure(java.io.IOException("Offline"))))
        assertEquals("ocular-light", owner.current.id)
        assertThrows(IllegalStateException::class.java) {
            owner.completeRefresh(owner.refreshTicket()!!,
                Result.failure(IllegalStateException("Malformed metadata")))
        }
        assertEquals("ocular-light", owner.current.id)
        owner.activate(null)
        assertEquals("legato", owner.current.id)
        assertFalse(owner.receive(latest, ThemePreference("dracula", 6)))
        assertFalse(owner.completeRefresh(latest, Result.failure(IllegalStateException("Old failure"))))
    }

    @Test fun `authoritative none older and unsupported servers use local without exiting follow`() {
        val disk = MemoryAppearance()
        val owner = settings(disk)
        owner.select("dracula")
        owner.select("server")
        owner.activate("a")
        owner.receive(owner.refreshTicket()!!, ThemePreference("ocular-light", 2))
        for (snapshot in listOf(ThemePreference("", 3), ThemePreference("future-theme", 4), null)) {
            owner.receive(owner.refreshTicket()!!, snapshot)
            assertEquals("dracula", owner.current.id)
            assertTrue(owner.followServer)
        }
        assertFalse(disk.cache.containsKey("a"))
        owner.receive(owner.refreshTicket()!!, ThemePreference("ocular-light", 10))
        assertFalse(owner.receive(owner.refreshTicket()!!, ThemePreference("ocular-dark", 9)))
        owner.select("ocular-dark")
        owner.receive(owner.refreshTicket()!!, ThemePreference("legato", 11))
        assertEquals("ocular-dark", owner.current.id)
        assertFalse(owner.followServer)
        owner.select("server")
        assertEquals("legato", owner.current.id)
        owner.forget("a")
        assertEquals("ocular-dark", owner.current.id)
        assertEquals("ocular-dark", settings(disk).current.id)
    }

    @Test fun `serialized appearance persistence reopens local mode and each server preference`() {
        val values = mutableMapOf<String, String>()
        var writable = true
        fun disk() = PreferenceAppearanceStore(values::get) { changes ->
            if (writable) changes.forEach { (key, value) ->
                if (value == null) values.remove(key) else values[key] = value
            }
            writable
        }
        val owner = settings(disk())
        owner.select("ocular-dark")
        owner.select("server")
        assertEquals("ocular-dark", owner.current.id)
        owner.activate("a")
        owner.receive(owner.refreshTicket()!!, ThemePreference("ocular-light", 7))
        owner.activate("b")
        owner.receive(owner.refreshTicket()!!, ThemePreference("dracula", 1))
        val reopened = settings(disk())
        assertTrue(reopened.followServer)
        assertEquals("ocular-dark", reopened.current.id)
        reopened.activate("a")
        assertEquals("ocular-light", reopened.current.id)
        assertEquals("ocular-dark", reopened.local.id)
        reopened.activate("b")
        assertEquals("dracula", reopened.current.id)
        writable = false
        assertThrows(IllegalStateException::class.java) { reopened.select("legato") }
        assertEquals("dracula", reopened.current.id)
        assertTrue(reopened.followServer)
        assertThrows(IllegalStateException::class.java) {
            reopened.completeRefresh(reopened.refreshTicket()!!, Result.success(ThemePreference("ocular-light", 2)))
        }
        assertEquals("dracula", reopened.current.id)
        assertEquals("dracula", settings(disk()).apply { activate("b") }.current.id)
        assertEquals("ocular-dark", reopened.local.id)
        writable = true
        reopened.select("ocular-light-vivid")
        val local = settings(disk())
        local.activate("a")
        assertFalse(local.followServer)
        assertEquals("ocular-light-vivid", local.current.id)
        reopened.forget("b")
        val removed = settings(disk())
        removed.select("server")
        removed.activate("b")
        assertEquals("ocular-light-vivid", removed.current.id)
    }

    @Test fun `retained server page keeps its effective theme while shell without a server uses local`() {
        val owner = settings(MemoryAppearance())
        owner.select("dracula")
        owner.select("server")
        owner.activate("a")
        owner.receive(owner.refreshTicket()!!, ThemePreference("ocular-light", 1))
        owner.activate(null)
        assertEquals("dracula", owner.current.id)
        assertEquals("ocular-light", owner.appearanceFor("a").effective.id)
        assertEquals("dracula", owner.appearanceFor("b").effective.id)
        assertEquals("dracula", owner.appearanceFor("a").local.id)
    }

    @Test fun `keyed bridge enables follow and named requests disable it only after saving`() {
        val disk = MemoryAppearance()
        val owner = settings(disk)
        owner.select("dracula")
        owner.activate("a")
        owner.receive(owner.refreshTicket()!!, ThemePreference("ocular-light", 1))
        val deliveries = mutableListOf<ThemeAppearance>()
        val replies = mutableListOf<Pair<String, String?>>()
        val bridge = ThemeBridge("key", { id, done ->
            val error = runCatching {
                check(owner.selectFromPage("a", id))
                deliveries += owner.appearance()
            }.exceptionOrNull()?.message
            done(error)
        }, { id, error -> replies += id to error })
        bridge.handle(PageMessage.Theme("wrong", "server"))
        assertTrue(replies.isEmpty())
        owner.activate(null)
        bridge.handle(PageMessage.Theme("key", "server"))
        assertNotNull(replies.last().second)
        assertTrue(deliveries.isEmpty())
        assertEquals(ThemeChoice("dracula", false), disk.choice)
        owner.activate("b")
        bridge.handle(PageMessage.Theme("key", "ocular-dark"))
        assertNotNull(replies.last().second)
        assertTrue(deliveries.isEmpty())
        owner.activate("a")
        bridge.handle(PageMessage.Theme("key", "server"))
        assertEquals("server" to null, replies.last())
        assertEquals("ocular-light", deliveries.last().effective.id)
        assertEquals("dracula", deliveries.last().local.id)
        assertTrue(deliveries.last().followServer)
        assertEquals(ThemeChoice("dracula", true), disk.choice)
        disk.writable = false
        bridge.handle(PageMessage.Theme("key", "ocular-dark"))
        assertNotNull(replies.last().second)
        assertEquals(1, deliveries.size)
        assertTrue(owner.followServer)
        disk.writable = true
        bridge.handle(PageMessage.Theme("key", "ocular-dark"))
        assertFalse(deliveries.last().followServer)
        assertEquals("ocular-dark", deliveries.last().effective.id)
        assertEquals("ocular-dark", settings(disk).current.id)
    }

}
