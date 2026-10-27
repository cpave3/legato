package dev.legato.mobile

import org.json.JSONObject
import java.util.Base64
import kotlin.math.log10

// What the app tells the page about a recording. The page turns the audio
// into the samples Legato's server transcribes.
sealed interface VoiceEvent {
    data object Started : VoiceEvent
    data class Level(val level: Double) : VoiceEvent
    class Audio(val data: ByteArray, val mime: String = "audio/mp4") : VoiceEvent
    data object Cancelled : VoiceEvent
    data class Error(val message: String) : VoiceEvent

    fun toJson(): JSONObject = when (this) {
        Started -> JSONObject().put("type", "started")
        is Level -> JSONObject().put("type", "level").put("level", level)
        is Audio -> JSONObject().put("type", "audio").put("data", Base64.getEncoder().encodeToString(data)).put("mime", mime)
        Cancelled -> JSONObject().put("type", "cancelled")
        is Error -> JSONObject().put("type", "error").put("message", message)
    }
}

const val LEVEL_EVERY_MS = 150L

// ShellVoice records the microphone for the page. A page served over plain
// HTTP has no microphone of its own, so the app records in its place: the
// page asks to start, stop or cancel, and hears back through `tell`. Only a
// request carrying the page's key is obeyed.
class ShellVoice(
    private val key: String,
    private val microphone: Microphone,
    private val tell: (VoiceEvent) -> Unit,
    // Runs the action every so often; the returned function stops it.
    private val every: (Long, () -> Unit) -> () -> Unit,
) {
    interface Microphone {
        fun requestPermission(granted: (Boolean) -> Unit)
        fun start()

        // How loud it is now, from 0 to 1.
        fun level(): Double

        // Ends the recording and gives it back.
        fun stop(): ByteArray?
        fun cancel()
    }

    private var recording = false
    private var stopMeter: (() -> Unit)? = null

    fun handle(message: PageMessage.Voice) {
        if (message.key != key) return
        try {
            when (message.action) {
                "start" -> start()
                "stop" -> stop()
                "cancel" -> cancel()
            }
        } catch (cause: Exception) {
            finish()
            tell(VoiceEvent.Error(cause.message ?: cause.toString()))
        }
    }

    // A recording does not outlive the page that asked for it.
    fun release() {
        if (recording) runCatching { microphone.cancel() }
        finish()
    }

    private fun start() {
        if (recording) return
        microphone.requestPermission { granted ->
            if (!granted) {
                tell(VoiceEvent.Error("Legato may not use the microphone. Allow it in the app's settings."))
                return@requestPermission
            }
            try {
                microphone.start()
                recording = true
                tell(VoiceEvent.Started)
                stopMeter = every(LEVEL_EVERY_MS) { if (recording) tell(VoiceEvent.Level(microphone.level())) }
            } catch (cause: Exception) {
                finish()
                tell(VoiceEvent.Error(cause.message ?: cause.toString()))
            }
        }
    }

    private fun stop() {
        if (!recording) return
        finish()
        val audio = microphone.stop()
        tell(if (audio == null || audio.isEmpty()) VoiceEvent.Error("The recording was not saved.") else VoiceEvent.Audio(audio))
    }

    private fun cancel() {
        if (!recording) return
        finish()
        microphone.cancel()
        tell(VoiceEvent.Cancelled)
    }

    private fun finish() {
        recording = false
        stopMeter?.invoke()
        stopMeter = null
    }
}

// levelOf turns MediaRecorder's peak amplitude (0 to 32767) into the 0 to 1
// level the page draws. Speech sits well above -50 dB below full scale.
fun levelOf(amplitude: Int): Double {
    if (amplitude <= 0) return 0.0
    val db = 20 * log10(amplitude / 32767.0)
    return ((db + 50) / 50).coerceIn(0.0, 1.0)
}
