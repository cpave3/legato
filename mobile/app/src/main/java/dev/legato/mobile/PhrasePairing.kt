package dev.legato.mobile

import org.json.JSONException
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.security.GeneralSecurityException
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.util.Base64
import java.util.Date

/** A server's answer to one pairing step, and the certificates it presented (none over HTTP). */
class PairingReply(
    val status: Int,
    val body: JSONObject,
    val certificates: List<X509Certificate>,
    val trustedBySystem: Boolean,
)

/** Posts a pairing step to one server. Throws IOException when it cannot reach it. */
interface PairingTransport {
    fun post(path: String, body: JSONObject): PairingReply
}

sealed class PairingFailure(message: String) : Exception(message) {
    class Mismatch : PairingFailure("The words do not match. Check them and try again.")
    class Closed(message: String) : PairingFailure(message)
    class Unsupported : PairingFailure("This server cannot pair by phrase. Update Legato on it, or pair with a QR code.")
    class NotGenuine(message: String) : PairingFailure(message)
    class Other(message: String) : PairingFailure(message)
}

/** What a paired phone keeps: the token, the server's name, and the certificate to trust. */
class PairedServer(val token: String, val name: String, val caCertificate: String?)

/**
 * The phone's side of pairing by phrase (the server's is
 * internal/service/pairing.go): SPAKE2 proves both sides know the phrase, then
 * the server sends the token sealed under the key the exchange gave.
 */
class PhrasePairing(
    private val transport: PairingTransport,
    private val device: String,
    private val random: RandomBytes = SecureRandomBytes,
) {
    private val b64e = Base64.getEncoder()
    private val b64d = Base64.getDecoder()

    fun pair(words: List<String>): PairedServer {
        val exchange = Spake2.start(Spake2.Party.A, Pairing.protocol, Pairing.phraseScalar(words), random)
        val first = transport.post("/api/pair/exchange", JSONObject().put("share", b64e.encodeToString(exchange.share)))
        refuseUnlessOk(first)
        val keys = try {
            exchange.finish(b64d.decode(first.body.getString("share")))
        } catch (cause: Exception) {
            throw PairingFailure.NotGenuine("The server's answer is not part of a pairing.")
        }
        val phone = Pairing.seal(keys.key, Pairing.LABEL_PHONE, JSONObject().put("device", device).toString().toByteArray(), random)
        val second = transport.post(
            "/api/pair/confirm",
            JSONObject()
                .put("session", first.body.optString("session"))
                .put("confirmation", b64e.encodeToString(keys.confirmation))
                .put("phone", b64e.encodeToString(phone)),
        )
        refuseUnlessOk(second)
        val confirmation = runCatching { b64d.decode(second.body.getString("confirmation")) }.getOrNull()
        if (confirmation == null || !keys.confirms(confirmation)) {
            throw PairingFailure.NotGenuine("The server could not prove it knows the words. Something may be pretending to be it.")
        }
        val secret = try {
            JSONObject(String(Pairing.open(keys.key, Pairing.LABEL_SERVER, b64d.decode(second.body.getString("sealed")))))
        } catch (cause: Exception) {
            throw PairingFailure.NotGenuine("The server's answer does not open with the words.")
        }
        val token = secret.optString("token")
        if (token.isEmpty()) throw PairingFailure.Other("The server sent no token.")
        val ca = secret.optString("ca_certificate").ifEmpty { null }
        checkCertificates(first, second, ca)
        return PairedServer(token, secret.optString("name"), ca)
    }

    private fun refuseUnlessOk(reply: PairingReply) {
        val message = reply.body.optString("error")
        when (reply.status) {
            200 -> return
            403 -> throw PairingFailure.Mismatch()
            404 -> throw PairingFailure.Unsupported()
            410 -> throw PairingFailure.Closed(message.ifEmpty { "No pairing request is open on this server." })
            else -> throw PairingFailure.Other(message.ifEmpty { "The server answered ${reply.status}." })
        }
    }

    // Over HTTPS the server's certificate is not trusted yet. It must be the
    // same in both steps, and trusted by the certificate the server sealed:
    // otherwise something between the phone and the server answered for it.
    private fun checkCertificates(first: PairingReply, second: PairingReply, ca: String?) {
        if (first.certificates.isEmpty() && second.certificates.isEmpty()) return
        if (first.certificates.firstOrNull() != second.certificates.firstOrNull()) {
            throw PairingFailure.NotGenuine("The server's certificate changed while pairing.")
        }
        if (first.trustedBySystem && second.trustedBySystem) return
        val pinned = ca?.let(CertificatePin::parse).orEmpty()
        if (!CertificatePin.trusts(second.certificates, pinned)) {
            throw PairingFailure.NotGenuine("The server's certificate is not the one it sent while pairing. Something may be between the phone and the server.")
        }
    }
}

/** Trusting a server by a certificate pinned at pairing, in place of the system's CAs. */
object CertificatePin {
    fun parse(pem: String): List<X509Certificate> = try {
        CertificateFactory.getInstance("X.509")
            .generateCertificates(ByteArrayInputStream(pem.toByteArray()))
            .filterIsInstance<X509Certificate>()
    } catch (_: GeneralSecurityException) {
        emptyList()
    } catch (_: JSONException) {
        emptyList()
    }

    /**
     * Whether a server presenting [chain] is trusted by [pinned]: its own
     * certificate is pinned, or was issued and signed by a pinned one, and is
     * within its dates at [now].
     */
    fun trusts(chain: List<X509Certificate>, pinned: List<X509Certificate>, now: Long = System.currentTimeMillis()): Boolean {
        val leaf = chain.firstOrNull() ?: return false
        try {
            leaf.checkValidity(Date(now))
        } catch (_: GeneralSecurityException) {
            return false
        }
        return pinned.any { pin ->
            leaf == pin || (leaf.issuerX500Principal == pin.subjectX500Principal && signedBy(leaf, pin))
        }
    }

    private fun signedBy(cert: X509Certificate, issuer: X509Certificate): Boolean = try {
        cert.verify(issuer.publicKey)
        true
    } catch (_: GeneralSecurityException) {
        false
    }
}
