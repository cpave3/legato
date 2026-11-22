package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

private class SwitchText(var value: String? = null) : ConnectionStore.Text {
    override fun read() = value
    override fun write(value: String) {
        this.value = value
    }
}

private class CodeVault : TokenVault {
    val saved = mutableMapOf<String, String>()
    override fun read(id: String) = saved[id]
    override fun write(id: String, token: String) {
        saved[id] = token
    }
    override fun remove(id: String) {
        saved.remove(id)
    }
}

class GlassesSettingsTest {
    private val switch = SwitchText()
    private val vault = CodeVault()
    private var made = 0
    private val settings = GlassesSettings(switch, vault) { "CODE${++made}".padEnd(24, 'A') }

    @Test
    fun `is off with no code at first`() {
        assertFalse(settings.enabled)
        assertNull(settings.code())
    }

    @Test
    fun `switching on makes a code when there is none, and keeps it after`() {
        val code = settings.switchOn()

        assertTrue(settings.enabled)
        assertEquals(code, settings.code())
        settings.switchOff()
        assertEquals(code, settings.switchOn())
        assertEquals(1, made)
    }

    @Test
    fun `a new code replaces the old one`() {
        val old = settings.switchOn()

        val new = settings.newCode()

        assertNotEquals(old, new)
        assertEquals(new, settings.code())
    }

    @Test
    fun `switching off keeps the code`() {
        val code = settings.switchOn()

        settings.switchOff()

        assertFalse(settings.enabled)
        assertEquals(code, settings.code())
    }

    @Test
    fun `keeps the switch and the code apart, the code in the vault only`() {
        val code = settings.switchOn()

        assertFalse(switch.value!!.contains(code))
        assertEquals(listOf(code), vault.saved.values.toList())
        assertTrue(GlassesSettings(switch, vault).enabled)
        assertEquals(code, GlassesSettings(switch, vault).code())
    }
}
