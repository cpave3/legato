package dev.legato.mobile

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

private class FakeMicrophone : ShellVoice.Microphone {
    var allowed = true
    var recording = false
    var stopped = false
    var amplitude = 0.4

    override fun requestPermission(granted: (Boolean) -> Unit) = granted(allowed)
    override fun start() {
        recording = true
    }
    override fun level(): Double = amplitude
    override fun stop(): ByteArray? {
        recording = false
        stopped = true
        return "ABC".toByteArray()
    }
    override fun cancel() {
        recording = false
        stopped = true
    }
}

class ShellVoiceTest {
    private lateinit var microphone: FakeMicrophone
    private val told = mutableListOf<JSONObject>()
    private val ticks = mutableListOf<() -> Unit>()
    private lateinit var voice: ShellVoice

    @Before
    fun pageIsOpen() {
        microphone = FakeMicrophone()
        told.clear()
        ticks.clear()
        voice = ShellVoice(key = "page-key", microphone = microphone, tell = { told += it.toJson() }) { _, tick ->
            ticks += tick
            return@ShellVoice { ticks.remove(tick) }
        }
    }

    private fun pageAsks(action: String, key: String = "page-key") = voice.handle(PageMessage.Voice(key, action))
    private fun types() = told.map { it.getString("type") }

    @Test
    fun `records when the page asks, and hands it the recording`() {
        pageAsks("start")
        assertTrue(microphone.recording)
        assertEquals(listOf("started"), types())

        pageAsks("stop")
        assertTrue(microphone.stopped)
        val audio = told.last()
        assertEquals("audio", audio.getString("type"))
        assertEquals("QUJD", audio.getString("data"))
        assertEquals("audio/mp4", audio.getString("mime"))
    }

    @Test
    fun `tells the page how loud the microphone is while it records`() {
        pageAsks("start")
        ticks.toList().forEach { it() }

        val level = told.single { it.getString("type") == "level" }
        assertEquals(0.4, level.getDouble("level"), 0.0001)
    }

    @Test
    fun `stops metering once the recording ends`() {
        pageAsks("start")
        pageAsks("stop")

        assertEquals(emptyList<() -> Unit>(), ticks)
    }

    @Test
    fun `throws a recording away when the page cancels it`() {
        pageAsks("start")
        pageAsks("cancel")

        assertTrue(microphone.stopped)
        assertEquals(listOf("started", "cancelled"), types())
    }

    @Test
    fun `does not record for a frame that lacks the page's key`() {
        pageAsks("start", key = "guessed")

        assertFalse(microphone.recording)
        assertEquals(emptyList<String>(), types())
    }

    @Test
    fun `says so when the microphone is refused`() {
        microphone.allowed = false

        pageAsks("start")

        assertFalse(microphone.recording)
        assertEquals("Legato may not use the microphone. Allow it in the app's settings.", told.single().getString("message"))
    }

    @Test
    fun `stops recording when the page goes away`() {
        pageAsks("start")

        voice.release()

        assertFalse(microphone.recording)
        assertEquals(emptyList<() -> Unit>(), ticks)
    }

    @Test
    fun `turns the microphone's amplitude into a level the page draws`() {
        assertEquals(0.0, levelOf(0), 0.0)
        assertEquals(1.0, levelOf(32767), 0.0001)
        // Quiet speech, about -30 dB below full scale.
        assertEquals(0.4, levelOf(1036), 0.01)
    }
}
