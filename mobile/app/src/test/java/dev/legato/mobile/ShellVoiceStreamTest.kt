package dev.legato.mobile

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

private class FakeStreamRecorder : ShellVoiceStream.Recorder {
    var allowed = true
    // When set, the person has not answered yet: answer() answers.
    var asking = false
    var answer: ((Boolean) -> Unit)? = null
    var failToStart: String? = null
    var recording = false
    var frame: ((ByteArray) -> Unit)? = null
    var failed: ((String) -> Unit)? = null

    override fun requestPermission(granted: (Boolean) -> Unit) {
        if (asking) answer = granted else granted(allowed)
    }
    override fun start(frame: (ByteArray) -> Unit, failed: (String) -> Unit) {
        failToStart?.let { throw IllegalStateException(it) }
        recording = true
        this.frame = frame
        this.failed = failed
    }
    override fun stop() {
        recording = false
    }
}

private class FakeFocus : ShellVoiceStream.Focus {
    var granted = true
    var held = false
    var lost: ((Boolean) -> Unit)? = null

    override fun request(lost: (permanent: Boolean) -> Unit): Boolean {
        this.lost = lost
        held = granted
        return granted
    }
    override fun abandon() {
        held = false
    }
}

class ShellVoiceStreamTest {
    private lateinit var recorder: FakeStreamRecorder
    private lateinit var focus: FakeFocus
    private val told = mutableListOf<JSONObject>()
    private lateinit var stream: ShellVoiceStream

    @Before
    fun pageIsOpen() {
        recorder = FakeStreamRecorder()
        focus = FakeFocus()
        told.clear()
        stream = ShellVoiceStream(key = "page-key", recorder = recorder, focus = focus, tell = { told += it })
    }

    private fun pageAsks(action: String, key: String = "page-key") = stream.handle(PageMessage.VoiceStream(key, action))
    private fun types() = told.map { it.getString("type") }

    @Test
    fun `streams the microphone to the page while the conversation lasts`() {
        pageAsks("start")
        assertTrue(recorder.recording)
        assertTrue(focus.held)
        assertEquals(listOf("stream-started"), types())

        recorder.frame!!(byteArrayOf(1, 0, -1, -1))
        val frame = told.last()
        assertEquals("frame", frame.getString("type"))
        assertEquals("AQD//w==", frame.getString("data"))

        pageAsks("stop")
        assertFalse(recorder.recording)
        assertFalse(focus.held)
        assertEquals(listOf("stream-started", "frame", "stream-stopped"), types())
    }

    @Test
    fun `a stop while the person is asked for the microphone cancels the start`() {
        recorder.asking = true
        pageAsks("start")
        pageAsks("stop")
        recorder.answer!!(true)
        assertFalse(recorder.recording)
        assertFalse(focus.held)
        assertEquals(emptyList<String>(), types())
    }

    @Test
    fun `the page going while the person is asked cancels the start`() {
        recorder.asking = true
        pageAsks("start")
        stream.release()
        recorder.answer!!(true)
        assertFalse(recorder.recording)
        assertEquals(emptyList<String>(), types())
    }

    @Test
    fun `sends no frame once the conversation stopped`() {
        pageAsks("start")
        val late = recorder.frame!!
        pageAsks("stop")

        late(byteArrayOf(1, 2))

        assertEquals(listOf("stream-started", "stream-stopped"), types())
    }

    @Test
    fun `starts once however often the page asks`() {
        pageAsks("start")
        pageAsks("start")

        assertEquals(listOf("stream-started"), types())
    }

    @Test
    fun `does not listen for a frame that lacks the page's key`() {
        pageAsks("start", key = "guessed")

        assertFalse(recorder.recording)
        assertFalse(focus.held)
        assertEquals(emptyList<String>(), types())
    }

    @Test
    fun `says so when the microphone is refused, and takes no focus`() {
        recorder.allowed = false

        pageAsks("start")

        assertFalse(recorder.recording)
        assertFalse(focus.held)
        assertEquals("error", told.single().getString("type"))
        assertEquals("Legato may not use the microphone. Allow it in the app's settings.", told.single().getString("message"))
    }

    @Test
    fun `does not listen while another app holds the audio`() {
        focus.granted = false

        pageAsks("start")

        assertFalse(recorder.recording)
        assertEquals("error", told.single().getString("type"))
    }

    @Test
    fun `gives the focus back when the recorder cannot start`() {
        recorder.failToStart = "The microphone is busy."

        pageAsks("start")

        assertFalse(focus.held)
        assertEquals("The microphone is busy.", told.single().getString("message"))
    }

    @Test
    fun `stops when the recorder fails`() {
        pageAsks("start")

        recorder.failed!!("The microphone stopped.")

        assertFalse(recorder.recording)
        assertFalse(focus.held)
        assertEquals(listOf("stream-started", "error", "stream-stopped"), types())
    }

    @Test
    fun `ends the conversation when another app takes the audio for good`() {
        pageAsks("start")

        focus.lost!!(true)

        assertFalse(recorder.recording)
        assertFalse(focus.held)
        assertEquals(listOf("stream-started", "stream-stopped"), types())
    }

    @Test
    fun `keeps listening through a short loss of focus`() {
        pageAsks("start")

        focus.lost!!(false)

        assertTrue(recorder.recording)
        assertEquals(listOf("stream-started"), types())
    }

    @Test
    fun `ends the conversation when the app leaves the foreground or the page goes away`() {
        pageAsks("start")

        stream.release()

        assertFalse(recorder.recording)
        assertFalse(focus.held)
        assertEquals(listOf("stream-started", "stream-stopped"), types())
        stream.release()
        assertEquals(1, types().count { it == "stream-stopped" })
    }
}
