package dev.legato.mobile

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.security.cert.X509Certificate
import java.util.Base64

private fun resource(name: String) = PhrasePairingTest::class.java.getResource("/$name")!!.readText()
private fun hex(s: String): ByteArray = s.chunked(2).map { it.toInt(16).toByte() }.toByteArray()
private val b64 = Base64.getEncoder()

/**
 * A server that answers as the Go server did when it recorded
 * pairing-exchange.json, so a phone using the recorded random value pairs.
 */
private class RecordedServer(
    private val certificates: List<X509Certificate> = emptyList(),
    private val trustedBySystem: Boolean = false,
    private val secret: String? = null,
    private val confirmation: String? = null,
    private val exchangeStatus: Int = 200,
    private val confirmStatus: Int = 200,
) : PairingTransport {
    val f = JSONObject(resource("pairing-exchange.json"))
    var device: String? = null

    override fun post(path: String, body: JSONObject): PairingReply = when (path) {
        "/api/pair/exchange" -> {
            assertEquals(f.getString("share_a"), Base64.getDecoder().decode(body.getString("share")).joinToString("") { "%02x".format(it) })
            if (exchangeStatus != 200) {
                PairingReply(exchangeStatus, JSONObject().put("error", "No pairing request is open."), certificates, trustedBySystem)
            } else {
                PairingReply(200, JSONObject().put("session", "s1").put("share", b64.encodeToString(hex(f.getString("share_b")))), certificates, trustedBySystem)
            }
        }
        "/api/pair/confirm" -> {
            assertEquals("s1", body.getString("session"))
            val key = hex(f.getString("key"))
            device = JSONObject(String(Pairing.open(key, Pairing.LABEL_PHONE, Base64.getDecoder().decode(body.getString("phone"))))).getString("device")
            if (confirmStatus != 200) {
                PairingReply(confirmStatus, JSONObject().put("error", "The phrase does not match."), certificates, trustedBySystem)
            } else {
                val sealed = if (secret == null) hex(f.getString("server_sealed")) else Pairing.seal(key, Pairing.LABEL_SERVER, secret.toByteArray())
                PairingReply(
                    200,
                    JSONObject()
                        .put("confirmation", b64.encodeToString(hex(confirmation ?: f.getString("confirm_b"))))
                        .put("sealed", b64.encodeToString(sealed)),
                    certificates,
                    trustedBySystem,
                )
            }
        }
        else -> error("unexpected $path")
    }
}

class PhrasePairingTest {
    private val f = JSONObject(resource("pairing-exchange.json"))
    private fun phone(server: RecordedServer) =
        PhrasePairing(server, "Pixel 4 XL", FixedRandom(hex(f.getString("x"))))
    private val words = f.getString("phrase").split(" ")

    /** The recorded secret first, then anything (the nonce of what the phone seals). */
    private class FixedRandom(private val first: ByteArray) : RandomBytes {
        private var used = false
        override fun fill(bytes: ByteArray) {
            if (used) SecureRandomBytes.fill(bytes) else first.copyInto(bytes)
            used = true
        }
    }

    private fun expectFailure(kind: Class<out PairingFailure>, block: () -> Unit): PairingFailure {
        try {
            block()
        } catch (failure: PairingFailure) {
            assertTrue("got ${failure::class.simpleName}: ${failure.message}", kind.isInstance(failure))
            return failure
        }
        fail("pairing succeeded")
        error("unreachable")
    }

    @Test
    fun aPhoneWithThePhraseGetsTheTokenAndTellsTheServerWhoItIs() {
        val server = RecordedServer()
        val paired = phone(server).pair(words)
        assertEquals("0123456789abcdef", paired.token)
        assertEquals("desktop", paired.name)
        assertNull(paired.caCertificate)
        assertEquals("Pixel 4 XL", server.device)
    }

    @Test
    fun aServerThatCannotConfirmThePhraseIsNotTrusted() {
        val server = RecordedServer(confirmation = "00".repeat(32))
        expectFailure(PairingFailure.NotGenuine::class.java) { phone(server).pair(words) }
    }

    @Test
    fun theServersRefusalsAreToldApart() {
        expectFailure(PairingFailure.Mismatch::class.java) { phone(RecordedServer(confirmStatus = 403)).pair(words) }
        val closed = expectFailure(PairingFailure.Closed::class.java) { phone(RecordedServer(exchangeStatus = 410)).pair(words) }
        assertEquals("No pairing request is open.", closed.message)
        expectFailure(PairingFailure.Unsupported::class.java) { phone(RecordedServer(exchangeStatus = 404)).pair(words) }
    }

    @Test
    fun overHttpsTheCertificateSentWhilePairingMustBeTheOneTheServerUses() {
        val ca = resource("pin-ca.pem")
        val secret = JSONObject().put("token", "t").put("name", "desktop").put("ca_certificate", ca).toString()

        val genuine = RecordedServer(CertificatePin.parse(resource("pin-server.pem")), secret = secret)
        assertEquals(ca, phone(genuine).pair(words).caCertificate)

        val between = RecordedServer(CertificatePin.parse(resource("pin-other-server.pem")), secret = secret)
        expectFailure(PairingFailure.NotGenuine::class.java) { phone(between).pair(words) }
    }

    @Test
    fun aCertificateTheSystemTrustsNeedsNoPin() {
        val secret = JSONObject().put("token", "t").put("name", "desktop").toString()
        val server = RecordedServer(CertificatePin.parse(resource("pin-other-server.pem")), trustedBySystem = true, secret = secret)
        assertNull(phone(server).pair(words).caCertificate)
    }
}

class CertificatePinTest {
    private val ca = CertificatePin.parse(resource("pin-ca.pem"))
    private val leaf = CertificatePin.parse(resource("pin-server.pem"))
    private val otherLeaf = CertificatePin.parse(resource("pin-other-server.pem"))
    private val during = leaf[0].notBefore.time + 60_000

    @Test
    fun aServerCertificateIssuedByThePinnedCaIsTrusted() {
        assertTrue(CertificatePin.trusts(leaf, ca, during))
        assertFalse(CertificatePin.trusts(otherLeaf, ca, during))
    }

    @Test
    fun aPinnedServerCertificateTrustsItself() {
        assertTrue(CertificatePin.trusts(leaf, leaf, during))
    }

    @Test
    fun aCertificateOutsideItsDatesIsNotTrusted() {
        assertFalse(CertificatePin.trusts(leaf, ca, leaf[0].notAfter.time + 60_000))
    }

    @Test
    fun textThatIsNotACertificateHoldsNone() {
        assertTrue(CertificatePin.parse("nonsense").isEmpty())
        assertFalse(CertificatePin.trusts(leaf, emptyList(), during))
    }
}
