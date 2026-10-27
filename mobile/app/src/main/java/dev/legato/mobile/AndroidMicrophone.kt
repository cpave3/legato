package dev.legato.mobile

import android.content.Context
import android.media.MediaRecorder
import android.os.Build
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
