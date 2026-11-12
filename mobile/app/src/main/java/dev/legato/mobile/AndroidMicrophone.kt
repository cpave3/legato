package dev.legato.mobile

import android.annotation.SuppressLint
import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.MediaRecorder
import android.os.Build
import android.os.Handler
import android.os.Looper
import java.io.File

// AndroidMicrophone records speech, small: AAC in an MP4 file, one channel at
// the rate the transcriber wants.
class AndroidMicrophone(
    private val context: Context,
    private val permission: (granted: (Boolean) -> Unit) -> Unit,
) : ShellVoice.Microphone {
    private var recorder: MediaRecorder? = null
    private var file: File? = null

    override fun requestPermission(granted: (Boolean) -> Unit) = permission(granted)

    override fun start() {
        val output = File.createTempFile("dictation", ".m4a", context.cacheDir)
        val recorder = if (Build.VERSION.SDK_INT >= 31) MediaRecorder(context) else @Suppress("DEPRECATION") MediaRecorder()
        try {
            recorder.setAudioSource(MediaRecorder.AudioSource.MIC)
            recorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            recorder.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            recorder.setAudioSamplingRate(16_000)
            recorder.setAudioChannels(1)
            recorder.setAudioEncodingBitRate(64_000)
            recorder.setOutputFile(output.path)
            recorder.prepare()
            recorder.start()
        } catch (cause: Exception) {
            recorder.release()
            output.delete()
            throw cause
        }
        this.recorder = recorder
        this.file = output
    }

    override fun level(): Double = recorder?.let { levelOf(it.maxAmplitude) } ?: 0.0

    override fun stop(): ByteArray? {
        val recorder = recorder ?: return null
        val output = file
        this.recorder = null
        this.file = null
        return try {
            recorder.stop()
            output?.readBytes()
        } catch (cause: RuntimeException) {
            // stop() throws when nothing was recorded (a tap too short).
            null
        } finally {
            recorder.release()
            output?.delete()
        }
    }

    override fun cancel() {
        stop()
    }
}

// AndroidAudioStream listens for a conversation: AudioRecord tuned for
// speech, 16 kHz mono PCM16, read on a thread of its own in frames of
// STREAM_FRAME_BYTES, each handed to the main thread.
class AndroidAudioStream(
    private val permission: (granted: (Boolean) -> Unit) -> Unit,
) : ShellVoiceStream.Recorder {
    private val main = Handler(Looper.getMainLooper())
    private var record: AudioRecord? = null
    private var reader: Thread? = null

    override fun requestPermission(granted: (Boolean) -> Unit) = permission(granted)

    @SuppressLint("MissingPermission") // asked for in requestPermission
    override fun start(frame: (ByteArray) -> Unit, failed: (String) -> Unit) {
        val minimum = AudioRecord.getMinBufferSize(STREAM_RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        if (minimum <= 0) throw IllegalStateException("This phone cannot record 16 kHz audio.")
        val record = AudioRecord(
            MediaRecorder.AudioSource.VOICE_RECOGNITION,
            STREAM_RATE,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
            maxOf(minimum, STREAM_FRAME_BYTES * 4),
        )
        if (record.state != AudioRecord.STATE_INITIALIZED) {
            record.release()
            throw IllegalStateException("The microphone could not be opened.")
        }
        try {
            record.startRecording()
        } catch (cause: Exception) {
            record.release()
            throw cause
        }
        if (record.recordingState != AudioRecord.RECORDSTATE_RECORDING) {
            record.release()
            throw IllegalStateException("Another app is using the microphone.")
        }
        this.record = record
        reader = Thread({
            while (!Thread.currentThread().isInterrupted) {
                val buffer = ByteArray(STREAM_FRAME_BYTES)
                var filled = 0
                // A blocking read can return less than asked; fill the frame.
                while (filled < buffer.size) {
                    val read = record.read(buffer, filled, buffer.size - filled)
                    if (read < 0) {
                        if (!Thread.currentThread().isInterrupted) main.post { failed("The microphone stopped ($read).") }
                        return@Thread
                    }
                    if (read == 0 && Thread.currentThread().isInterrupted) return@Thread
                    filled += read
                }
                main.post { frame(buffer) }
            }
        }, "legato-voice-stream").apply { start() }
    }

    override fun stop() {
        val record = record ?: return
        this.record = null
        val reader = reader
        this.reader = null
        reader?.interrupt()
        // stop() releases a blocked read, so the reader ends before release.
        runCatching { record.stop() }
        runCatching { reader?.join(500) }
        record.release()
    }
}

// AndroidAudioFocus holds AUDIOFOCUS_GAIN_TRANSIENT for a conversation:
// music pauses while the person and the Reeve talk, and resumes after.
class AndroidAudioFocus(context: Context) : ShellVoiceStream.Focus {
    private val audio = context.getSystemService(AudioManager::class.java)
    private var request: AudioFocusRequest? = null

    override fun request(lost: (permanent: Boolean) -> Unit): Boolean {
        abandon()
        val request = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT)
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANT)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setOnAudioFocusChangeListener({ change ->
                // A call takes the focus for a while (LOSS_TRANSIENT): the
                // conversation ends. A loss for good is not one: the page's
                // own <audio>, a line of the Reeve's, takes the focus so in
                // the WebView. A loss we may duck under is ignored.
                when (change) {
                    AudioManager.AUDIOFOCUS_LOSS_TRANSIENT -> lost(true)
                    AudioManager.AUDIOFOCUS_LOSS, AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK -> lost(false)
                }
            }, Handler(Looper.getMainLooper()))
            .build()
        val granted = audio.requestAudioFocus(request) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
        if (granted) this.request = request
        return granted
    }

    override fun abandon() {
        request?.let(audio::abandonAudioFocusRequest)
        request = null
    }
}
