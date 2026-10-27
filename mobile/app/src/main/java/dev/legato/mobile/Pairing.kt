package dev.legato.mobile

import org.bouncycastle.crypto.ec.CustomNamedCurves
import org.bouncycastle.math.ec.ECPoint
import java.math.BigInteger
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.GeneralSecurityException
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.Mac
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

// Pairing by phrase: the same protocol as the server's internal/engine/pairing,
// which the shared test fixture (pairing-exchange.json) holds both sides to.

class PairingException(message: String) : Exception(message)

fun interface RandomBytes {
    fun fill(bytes: ByteArray)
}

object SecureRandomBytes : RandomBytes {
    private val random = SecureRandom()
    override fun fill(bytes: ByteArray) = random.nextBytes(bytes)
}

/** SPAKE2 as RFC 9382 defines it, with the SPAKE2-P256-SHA256-HKDF-HMAC suite. */
object Spake2 {
    enum class Party { A, B }

    class Config(val identityA: ByteArray, val identityB: ByteArray, val aad: ByteArray)

    private val curve = CustomNamedCurves.getByName("secp256r1")
    private val order: BigInteger = curve.n
    private val pointM = point(hexBytes("02886e2f97ace46e55ba9dd7242579f2993b64e16ef3dcab95afd497333d8fa12f"))
    private val pointN = point(hexBytes("03d8bbd6c639c62937b04d997f38c3770719c629d7014d49a24b4f98baa1292b49"))

    val groupOrder: BigInteger get() = order

    private fun point(bytes: ByteArray): ECPoint = try {
        curve.curve.decodePoint(bytes)
    } catch (_: IllegalArgumentException) {
        throw PairingException("not a point")
    }

    class Keys(val key: ByteArray, val confirmation: ByteArray, private val peerConfirmation: ByteArray) {
        fun confirms(conf: ByteArray): Boolean = MessageDigest.isEqual(conf, peerConfirmation)
    }

    class Session internal constructor(
        private val party: Party,
        private val config: Config,
        private val w: BigInteger,
        private val secret: BigInteger,
        val share: ByteArray,
    ) {
        fun finish(peerShare: ByteArray): Keys {
            if (peerShare.size != 65) throw PairingException("the server's share is not a point")
            val peer = point(peerShare)
            if (peer.isInfinity || !peer.isValid) throw PairingException("the server's share is not a point")
            val peerBlind = if (party == Party.A) pointN else pointM
            val k = peer.subtract(peerBlind.multiply(w)).multiply(secret).normalize()
            if (k.isInfinity) throw PairingException("the exchange gave no key")
            val kBytes = k.getEncoded(false)
            val (pA, pB) = if (party == Party.A) share to peerShare else peerShare to share
            val tt = transcript(config.identityA, config.identityB, pA, pB, kBytes, scalar(w))
            val digest = MessageDigest.getInstance("SHA-256").digest(tt)
            val ke = digest.copyOfRange(0, 16)
            val ka = digest.copyOfRange(16, 32)
            val kc = Pairing.hkdf(ka, null, "ConfirmationKeys".toByteArray() + config.aad, 32)
            val confA = Pairing.hmac(kc.copyOfRange(0, 16), tt)
            val confB = Pairing.hmac(kc.copyOfRange(16, 32), tt)
            return if (party == Party.A) Keys(ke, confA, confB) else Keys(ke, confB, confA)
        }
    }

    /** Begins an exchange with the password scalar w (32 bytes, big-endian). */
    fun start(party: Party, config: Config, w: ByteArray, random: RandomBytes): Session {
        require(w.size == 32) { "the password scalar must be 32 bytes" }
        val wInt = BigInteger(1, w).mod(order)
        val secret = randomScalar(random)
        val blind = if (party == Party.A) pointM else pointN
        val share = curve.g.multiply(secret).add(blind.multiply(wInt)).normalize().getEncoded(false)
        return Session(party, config, wInt, secret, share)
    }

    private fun randomScalar(random: RandomBytes): BigInteger {
        val buf = ByteArray(32)
        repeat(64) {
            random.fill(buf)
            val n = BigInteger(1, buf)
            if (n.signum() > 0 && n < order) return n
        }
        throw PairingException("no usable random scalar")
    }

    internal fun scalar(n: BigInteger): ByteArray {
        val raw = n.toByteArray()
        val out = ByteArray(32)
        val start = maxOf(0, raw.size - 32)
        raw.copyInto(out, 32 - (raw.size - start), start, raw.size)
        return out
    }

    private fun transcript(vararg parts: ByteArray): ByteArray {
        val buf = ByteBuffer.allocate(parts.sumOf { 8 + it.size }).order(ByteOrder.LITTLE_ENDIAN)
        for (part in parts) {
            buf.putLong(part.size.toLong())
            buf.put(part)
        }
        return buf.array()
    }

    private fun hexBytes(s: String) = s.chunked(2).map { it.toInt(16).toByte() }.toByteArray()
}

/** Legato's use of SPAKE2: the phone is A, the server B. */
object Pairing {
    const val LABEL_PHONE = "legato pair phone"
    const val LABEL_SERVER = "legato pair server"

    val protocol = Spake2.Config("legato-mobile".toByteArray(), "legato-server".toByteArray(), "legato-pair-v1".toByteArray())

    /** The password scalar of a phrase's words (see PhraseScalar on the server). */
    fun phraseScalar(words: List<String>): ByteArray {
        val out = hkdf(words.joinToString(" ").toByteArray(), "legato pair v1".toByteArray(), "spake2 w".toByteArray(), 40)
        return Spake2.scalar(BigInteger(1, out).mod(Spake2.groupOrder))
    }

    /** AES-256-GCM under a key derived from the exchange's key and the label; the nonce first. */
    fun seal(key: ByteArray, label: String, plaintext: ByteArray, random: RandomBytes = SecureRandomBytes): ByteArray {
        val nonce = ByteArray(12).also(random::fill)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(hkdf(key, null, label.toByteArray(), 32), "AES"), GCMParameterSpec(128, nonce))
        return nonce + cipher.doFinal(plaintext)
    }

    fun open(key: ByteArray, label: String, sealed: ByteArray): ByteArray {
        if (sealed.size < 28) throw PairingException("sealed text is too short")
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(hkdf(key, null, label.toByteArray(), 32), "AES"), GCMParameterSpec(128, sealed, 0, 12))
        return try {
            cipher.doFinal(sealed, 12, sealed.size - 12)
        } catch (_: GeneralSecurityException) {
            throw PairingException("sealed text does not open")
        }
    }

    internal fun hmac(key: ByteArray, message: ByteArray): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(key, "HmacSHA256"))
        return mac.doFinal(message)
    }

    /** HKDF-SHA256 (RFC 5869); no salt is a salt of 32 zero bytes. */
    internal fun hkdf(ikm: ByteArray, salt: ByteArray?, info: ByteArray, length: Int): ByteArray {
        val prk = hmac(if (salt == null || salt.isEmpty()) ByteArray(32) else salt, ikm)
        val out = ByteArray(length)
        var previous = ByteArray(0)
        var filled = 0
        var counter = 1
        while (filled < length) {
            previous = hmac(prk, previous + info + byteArrayOf(counter.toByte()))
            val n = minOf(previous.size, length - filled)
            previous.copyInto(out, filled, 0, n)
            filled += n
            counter++
        }
        return out
    }
}

/** The EFF short word list 2.0 (CC BY 3.0 US), as the server draws phrases from it. */
object PairingWords {
    val all: List<String> by lazy {
        val stream = PairingWords::class.java.getResourceAsStream("/pairing-words.txt")
            ?: error("the pairing word list is missing")
        stream.bufferedReader().readLines().filter { it.isNotBlank() }
    }
}

/** What the person typed for a phrase, read word by word. */
object PhraseInput {
    const val WORDS = 4

    private val byPrefix: Map<String, String> by lazy { PairingWords.all.associateBy { it.take(3) } }

    /** The list word a typed word stands for: its first three letters, case ignored. */
    fun wordFor(typed: String): String? {
        val letters = typed.lowercase().filter { it in 'a'..'z' }
        if (letters.length < 3) return null
        return byPrefix[letters.take(3)]
    }

    /**
     * [words] has one entry per typed word, null where it names no list word.
     * [mistakes] counts those, except the last one while [typing] it.
     */
    class Reading(val words: List<String?>, val mistakes: Int) {
        val complete: Boolean get() = words.size == WORDS && words.all { it != null }
        val tooMany: Boolean get() = words.size > WORDS
    }

    fun read(text: String, typing: Boolean = false): Reading {
        val tokens = text.split(Regex("[^A-Za-z]+")).filter { it.isNotEmpty() }
        val words = tokens.map(::wordFor)
        val checked = if (typing && text.lastOrNull()?.isLetter() == true) words.dropLast(1) else words
        return Reading(words, checked.count { it == null })
    }
}
