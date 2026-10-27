package dev.legato.mobile

import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.security.MessageDigest

private fun hex(s: String): ByteArray = s.chunked(2).map { it.toInt(16).toByte() }.toByteArray()
private fun ByteArray.hex(): String = joinToString("") { "%02x".format(it) }

/** Hands out fixed bytes, as a random source that is not random. */
private class FixedBytes(vararg chunks: ByteArray) : RandomBytes {
    private val queue = ArrayDeque(chunks.toList())
    override fun fill(bytes: ByteArray) {
        queue.removeFirst().copyInto(bytes)
    }
}

class PairingCryptoTest {
    // RFC 9382 Appendix B, first vector.
    @Test
    fun spake2MatchesTheRfcVectors() {
        val config = Spake2.Config("server".toByteArray(), "client".toByteArray(), ByteArray(0))
        val w = hex("2ee57912099d31560b3a44b1184b9b4866e904c49d12ac5042c97dca461b1a5f")
        val a = Spake2.start(Spake2.Party.A, config, w, FixedBytes(hex("43dd0fd7215bdcb482879fca3220c6a968e66d70b1356cac18bb26c84a78d729")))
        val b = Spake2.start(Spake2.Party.B, config, w, FixedBytes(hex("dcb60106f276b02606d8ef0a328c02e4b629f84f89786af5befb0bc75b6e66be")))
        assertEquals(
            "04a56fa807caaa53a4d28dbb9853b9815c61a411118a6fe516a8798434751470f9010153ac33d0d5f2047ffdb1a3e42c9b4e6be662766e1eeb4116988ede5f912c",
            a.share.hex(),
        )
        val keysA = a.finish(b.share)
        val keysB = b.finish(a.share)
        assertEquals("0e0672dc86f8e45565d338b0540abe69", keysA.key.hex())
        assertEquals("58ad4aa88e0b60d5061eb6b5dd93e80d9c4f00d127c65b3b35b1b5281fee38f0", keysA.confirmation.hex())
        assertEquals("d3e2e547f1ae04f2dbdbf0fc4b79f8ecff2dff314b5d32fe9fcef2fb26dc459b", keysB.confirmation.hex())
        assertTrue(keysA.confirms(keysB.confirmation))
    }

    // The exchange the server's tests recorded (internal/engine/pairing/testdata).
    @Test
    fun theAppReachesWhatTheServerReached() {
        val f = JSONObject(javaClass.getResource("/pairing-exchange.json")!!.readText())
        val words = f.getString("phrase").split(" ")
        val w = Pairing.phraseScalar(words)
        assertEquals(f.getString("w"), w.hex())

        val phone = Spake2.start(Spake2.Party.A, Pairing.protocol, w, FixedBytes(hex(f.getString("x"))))
        assertEquals(f.getString("share_a"), phone.share.hex())
        val keys = phone.finish(hex(f.getString("share_b")))
        assertEquals(f.getString("key"), keys.key.hex())
        assertEquals(f.getString("confirm_a"), keys.confirmation.hex())
        assertTrue(keys.confirms(hex(f.getString("confirm_b"))))

        val sealed = Pairing.seal(keys.key, Pairing.LABEL_PHONE, f.getString("phone_plaintext").toByteArray(), FixedBytes(ByteArray(12) { 1 }))
        assertEquals(f.getString("phone_sealed"), sealed.hex())
        val opened = Pairing.open(keys.key, Pairing.LABEL_SERVER, hex(f.getString("server_sealed")))
        assertEquals(f.getString("server_plaintext"), String(opened))
    }

    @Test
    fun anotherPhraseDoesNotConfirm() {
        val f = JSONObject(javaClass.getResource("/pairing-exchange.json")!!.readText())
        val w = Pairing.phraseScalar(listOf("river", "candle", "otter", "mule"))
        val phone = Spake2.start(Spake2.Party.A, Pairing.protocol, w, FixedBytes(hex(f.getString("x"))))
        val keys = phone.finish(hex(f.getString("share_b")))
        assertFalse(keys.confirms(hex(f.getString("confirm_b"))))
    }

    @Test
    fun aSealedTextDoesNotOpenWithTheWrongKey() {
        val f = JSONObject(javaClass.getResource("/pairing-exchange.json")!!.readText())
        try {
            Pairing.open(ByteArray(16), Pairing.LABEL_SERVER, hex(f.getString("server_sealed")))
            fail("opened with the wrong key")
        } catch (_: PairingException) {
        }
    }

    @Test
    fun aShareThatIsNotAPointIsRefused() {
        val phone = Spake2.start(Spake2.Party.A, Pairing.protocol, ByteArray(32) { 1 }, SecureRandomBytes)
        try {
            phone.finish(byteArrayOf(4, 1, 2, 3))
            fail("a garbage share was accepted")
        } catch (_: PairingException) {
        }
    }

    @Test
    fun theWordListIsTheServers() {
        val digest = MessageDigest.getInstance("SHA-256").digest((PairingWords.all.joinToString("\n") + "\n").toByteArray())
        assertEquals("79fb63e0b81dfda90fea676b7080a352ce560178360dce20a18e8f49f7570dcf", digest.hex())
        assertArrayEquals(arrayOf("aardvark"), PairingWords.all.take(1).toTypedArray())
    }
}
