package dev.legato.mobile

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Handler
import android.os.Looper
import android.util.Log

/**
 * Lists the Legato servers announced on the local network, through Android's
 * DNS-SD. [onChange] is called on the main thread with the whole list.
 */
class AndroidDiscovery(context: Context, private val onChange: (List<DiscoveredServer>) -> Unit) {
    private val nsd = context.getSystemService(NsdManager::class.java)
    private val main = Handler(Looper.getMainLooper())
    private val found = linkedMapOf<String, DiscoveredServer>()
    private val waiting = ArrayDeque<NsdServiceInfo>()
    private var resolving = false
    private var listener: NsdManager.DiscoveryListener? = null

    fun start() {
        if (listener != null) return
        val l = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(serviceType: String) {}
            override fun onDiscoveryStopped(serviceType: String) {}
            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                Log.w(TAG, "discovery did not start: $errorCode")
                main.post { if (listener === this) listener = null }
            }
            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {}
            override fun onServiceFound(info: NsdServiceInfo) = main.post { resolve(info) }.let {}
            override fun onServiceLost(info: NsdServiceInfo) = main.post {
                if (found.remove(info.serviceName) != null) onChange(found.values.toList())
            }.let {}
        }
        listener = l
        nsd.discoverServices(LEGATO_SERVICE_TYPE, NsdManager.PROTOCOL_DNS_SD, l)
    }

    fun stop() {
        val l = listener ?: return
        listener = null
        runCatching { nsd.stopServiceDiscovery(l) }
        waiting.clear()
        found.clear()
        onChange(emptyList())
    }

    // Before Android 14 one service is resolved at a time.
    private fun resolve(info: NsdServiceInfo) {
        if (resolving) {
            waiting.addLast(info)
            return
        }
        resolving = true
        @Suppress("DEPRECATION")
        nsd.resolveService(info, object : NsdManager.ResolveListener {
            override fun onResolveFailed(info: NsdServiceInfo, errorCode: Int) = main.post { next() }.let {}
            override fun onServiceResolved(resolved: NsdServiceInfo) = main.post {
                @Suppress("DEPRECATION")
                val host = resolved.host?.hostAddress
                if (listener != null && host != null) {
                    val scheme = resolved.attributes["scheme"]?.toString(Charsets.UTF_8)
                    runCatching { discoveredBaseUrl(scheme, host, resolved.port) }.onSuccess { url ->
                        found[resolved.serviceName] = DiscoveredServer(resolved.serviceName, url)
                        onChange(found.values.toList())
                    }
                }
                next()
            }.let {}
        })
    }

    private fun next() {
        resolving = false
        waiting.removeFirstOrNull()?.let(::resolve)
    }

    private companion object {
        const val TAG = "LegatoDiscovery"
    }
}
