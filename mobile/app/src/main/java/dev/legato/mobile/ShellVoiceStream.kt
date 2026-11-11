package dev.legato.mobile

import org.json.JSONObject
import java.util.Base64

// The microphone streamed to the page for a conversation with the Reeve. The
// page runs the voice session and plays the Reeve's lines itself; the app
// only listens: 16 kHz mono 16-bit PCM, little-endian, in frames of about
// 100 ms. Dictation keeps its own recording (ShellVoice).

// The samples of one frame: 1,600 samples, 100 ms at 16 kHz.
const val STREAM_RATE = 16_000
const val STREAM_FRAME_BYTES = 3_200

// ShellVoiceStream listens for one page. Only a request carrying the page's
// key is obeyed. Everything, frames and focus changes too, is expected on the
// main thread.
class ShellVoiceStream(
    private val key: String,
    private val recorder: Recorder,
    private val focus: Focus,
    private val tell: (JSONObject) -> Unit,
) {
    interface Recorder {
        fun requestPermission(granted: (Boolean) -> Unit)

        // Starts listening. frame gets each frame of s16le samples; failed
        // says why listening stopped by itself.
        fun start(frame: (ByteArray) -> Unit, failed: (String) -> Unit)
        fun stop()
    }

    // The audio focus held for the conversation, so music pauses while the
    // person and the Reeve talk.
    interface Focus {
        // Whether the focus was granted; lost is told when another app takes
        // it, permanently or for a moment.
        fun request(lost: (permanent: Boolean) -> Unit): Boolean
        fun abandon()
    }

    private var streaming = false

    // Each start gets its own generation, so a frame from a recording already
    // stopped is not sent.
    private var generation = 0

    fun handle(message: PageMessage.VoiceStream) {
        if (message.key != key) return
        when (message.action) {
            "start" -> start()
            "stop" -> end()
        }
    }

    // The conversation does not outlive the page, nor the app in front.
    fun release() = end()

    private fun start() {
        if (streaming) return
        recorder.requestPermission { granted ->
            if (!granted) {
                error("Legato may not use the microphone. Allow it in the app's settings.")
                return@requestPermission
            }
            if (streaming) return@requestPermission
            val current = ++generation
            if (!focus.request { permanent -> if (permanent && current == generation) end() }) {
                error("Another app is using the audio. Try again when it stops.")
                return@requestPermission
            }
            try {
                recorder.start(
                    frame = { data -> if (streaming && current == generation) tell(frameEvent(data)) },
                    failed = { why ->
                        if (streaming && current == generation) {
                            error(why)
                            end()
                        }
                    },
                )
            } catch (cause: Exception) {
                focus.abandon()
                error(cause.message ?: cause.toString())
                return@requestPermission
            }
            streaming = true
            tell(JSONObject().put("type", "stream-started"))
        }
    }

    private fun end() {
        if (!streaming) return
        streaming = false
        generation++
        runCatching { recorder.stop() }
        focus.abandon()
        tell(JSONObject().put("type", "stream-stopped"))
    }

    private fun error(message: String) = tell(JSONObject().put("type", "error").put("message", message))
}

// frameEvent hands the page one frame of samples, as base64.
fun frameEvent(data: ByteArray): JSONObject = JSONObject().put("type", "frame").put("data", Base64.getEncoder().encodeToString(data))
