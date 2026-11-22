package dev.legato.mobile

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import androidx.compose.runtime.mutableStateOf
import java.io.IOException
import java.util.concurrent.Executors

private const val TAG = "LegatoGlasses"

// What the Glasses screen shows of the link.
sealed interface GlassesLinkState {
    data object Off : GlassesLinkState
    data object Starting : GlassesLinkState
    data object Listening : GlassesLinkState
    data class Failed(val reason: String) : GlassesLinkState
}

object GlassesLinkStatus {
    val state = mutableStateOf<GlassesLinkState>(GlassesLinkState.Off)
    private val main = Handler(Looper.getMainLooper())

    fun set(next: GlassesLinkState) {
        main.post { state.value = next }
    }
}

// GlassesLinkService runs the glasses link (GlassesServer answering with
// GlassesHttp) while its switch is on, as a foreground service: the Legato
// app is usually in the background, or the phone locked, while the glasses
// are in use, and Android would otherwise stop the process. It reads the
// saved servers and the code on every request, so a server added later, or a
// new code, applies at once.
class GlassesLinkService : Service() {
    companion object {
        private const val CHANNEL = "glasses-link"
        private const val NOTIFICATION = 8792

        fun start(context: Context) {
            context.startForegroundService(Intent(context, GlassesLinkService::class.java))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, GlassesLinkService::class.java))
            GlassesLinkStatus.set(GlassesLinkState.Off)
        }
    }

    private var server: GlassesServer? = null
    private val starter = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        try {
            startForeground(NOTIFICATION, notification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE)
        } catch (cause: Exception) {
            // Android may refuse a foreground service started from the
            // background (a sticky restart, say).
            Log.w(TAG, "Could not start the glasses link in the foreground", cause)
            GlassesLinkStatus.set(GlassesLinkState.Failed("Android did not let the link start. Open the Legato app to start it again."))
            stopSelf()
            return START_NOT_STICKY
        }
        val settings = glassesSettings(this)
        if (!settings.enabled) {
            stopSelf()
            return START_NOT_STICKY
        }
        if (server == null) listen(settings)
        return START_STICKY
    }

    private fun listen(settings: GlassesSettings) {
        val store = connectionStore(this)
        val link = GlassesServer(
            handler = { request ->
                GlassesHttp.answer(request, settings.code(), store.load()) { connection, timeoutMs ->
                    store.token(connection.id)?.let { AndroidServerHttp(connection.baseUrl, it, connection.caCertificate, timeoutMs) }
                }
            },
            refusal = GlassesHttp::error,
        )
        server = link
        GlassesLinkStatus.set(GlassesLinkState.Starting)
        starter.execute {
            try {
                link.start()
                GlassesLinkStatus.set(GlassesLinkState.Listening)
            } catch (cause: IOException) {
                Log.w(TAG, "The glasses link could not listen", cause)
                GlassesLinkStatus.set(GlassesLinkState.Failed("Could not listen on 127.0.0.1:${GlassesServer.PORT}: ${cause.message ?: cause}"))
                main.post {
                    if (server === link) server = null
                    stopSelf()
                }
            }
        }
    }

    override fun onDestroy() {
        server?.stop()
        server = null
        starter.shutdown()
        if (GlassesLinkStatus.state.value !is GlassesLinkState.Failed) GlassesLinkStatus.set(GlassesLinkState.Off)
        super.onDestroy()
    }

    private fun notification(): Notification {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL, "Glasses link", NotificationManager.IMPORTANCE_LOW).apply {
                description = "Shown while the Even G2 glasses can read Legato through this app"
            },
        )
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        return Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_glasses_link)
            .setContentTitle("Glasses link on")
            .setContentText("Your Even G2 glasses can read Legato through this app.")
            .setOngoing(true)
            .setContentIntent(open)
            .build()
    }
}
