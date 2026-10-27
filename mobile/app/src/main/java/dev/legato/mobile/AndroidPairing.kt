package dev.legato.mobile

import android.content.Context
import android.os.Build
import android.provider.Settings
import org.json.JSONException
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.security.KeyStore
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.HttpsURLConnection
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManagerFactory
import javax.net.ssl.X509TrustManager

/** What the person called this phone, else its model. */
fun deviceName(context: Context): String =
    Settings.Global.getString(context.contentResolver, Settings.Global.DEVICE_NAME)?.takeIf { it.isNotBlank() } ?: Build.MODEL

private fun systemTrust(): X509TrustManager {
    val factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm())
    factory.init(null as KeyStore?)
    return factory.trustManagers.filterIsInstance<X509TrustManager>().first()
}

/**
 * Accepts whatever certificate the server presents, and records it and whether
 * the system would have trusted it. Used only while pairing: PhrasePairing
 * then checks the certificate against the one the server sealed.
 */
private class RecordingTrust : X509TrustManager {
    private val system = systemTrust()
    var chain: List<X509Certificate> = emptyList()
    var systemTrusted = false

    override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
        this.chain = chain.toList()
        systemTrusted = runCatching { system.checkServerTrusted(chain, authType) }.isSuccess
    }

    override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) = throw CertificateException("not a server")
    override fun getAcceptedIssuers(): Array<X509Certificate> = system.acceptedIssuers
}

/** Pairing's two steps over HTTP(S) to one server. */
class HttpPairingTransport(private val baseUrl: String) : PairingTransport {
    override fun post(path: String, body: JSONObject): PairingReply {
        val url = URL(baseUrl + path)
        val connection = url.openConnection() as HttpURLConnection
        val trust = RecordingTrust()
        var hostMatches = false
        if (connection is HttpsURLConnection) {
            val context = SSLContext.getInstance("TLS")
            context.init(null, arrayOf(trust), null)
            connection.sslSocketFactory = context.socketFactory
            connection.hostnameVerifier = HostnameVerifier { host, session ->
                hostMatches = HttpsURLConnection.getDefaultHostnameVerifier().verify(host, session)
                true
            }
        }
        try {
            connection.connectTimeout = 5_000
            connection.readTimeout = 10_000
            connection.requestMethod = "POST"
            connection.doOutput = true
            connection.setRequestProperty("Content-Type", "application/json")
            connection.outputStream.use { it.write(body.toString().toByteArray()) }
            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            val text = stream?.use { it.readBytes().toString(Charsets.UTF_8) }.orEmpty()
            val json = try {
                JSONObject(text)
            } catch (_: JSONException) {
                JSONObject()
            }
            return PairingReply(status, json, trust.chain, trust.systemTrusted && hostMatches)
        } finally {
            connection.disconnect()
        }
    }
}

/**
 * Trusts what the system trusts, and a server whose certificate the pinned
 * one ([pem], from pairing) trusts.
 */
class PinnedTrust(pem: String) : X509TrustManager {
    private val system = systemTrust()
    private val pinned = CertificatePin.parse(pem)

    override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
        try {
            system.checkServerTrusted(chain, authType)
        } catch (cause: CertificateException) {
            if (!CertificatePin.trusts(chain.toList(), pinned)) throw cause
        }
    }

    override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) = throw CertificateException("not a server")
    override fun getAcceptedIssuers(): Array<X509Certificate> = system.acceptedIssuers

    companion object {
        /** Makes [connection] trust the pinned certificate too, when it is HTTPS and there is one. */
        fun apply(connection: HttpURLConnection, pem: String?) {
            if (connection !is HttpsURLConnection || pem == null) return
            val context = SSLContext.getInstance("TLS")
            context.init(null, arrayOf(PinnedTrust(pem)), null)
            connection.sslSocketFactory = context.socketFactory
        }
    }
}
