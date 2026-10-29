package dev.legato.mobile

import android.app.Activity
import android.hardware.biometrics.BiometricManager.Authenticators
import android.hardware.biometrics.BiometricPrompt
import android.os.Build
import android.os.CancellationSignal
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.util.concurrent.Executor

// AndroidDeviceKeys keeps the approval keys in the Android Keystore: EC
// P-256, for signing only, in StrongBox when the phone has one, unlocked by
// the person's fingerprint or PIN for each signature (on Android 10, the
// fingerprint only, and then a new fingerprint retires the key).
class AndroidDeviceKeys : DeviceKeys {
    private val keyStore: KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    // Whether the last key made lives in StrongBox.
    var strongBox = false
        private set

    override fun create(alias: String, challenge: ByteArray): List<ByteArray> {
        strongBox = try {
            generate(keyParams(Build.VERSION.SDK_INT, alias, challenge, strongBox = true))
            true
        } catch (cause: StrongBoxUnavailableException) {
            generate(keyParams(Build.VERSION.SDK_INT, alias, challenge, strongBox = false))
            false
        }
        return keyStore.getCertificateChain(alias).map { it.encoded }
    }

    private fun generate(p: KeyParams) {
        val builder = KeyGenParameterSpec.Builder(p.alias, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setUserAuthenticationRequired(p.perUseAuthentication)
            .setInvalidatedByBiometricEnrollment(p.invalidatedByBiometricEnrollment)
            .setAttestationChallenge(p.challenge)
        if (p.strongBox) builder.setIsStrongBoxBacked(true)
        if (Build.VERSION.SDK_INT >= 30) {
            val kinds = if (p.allowDeviceCredential) KeyProperties.AUTH_BIOMETRIC_STRONG or KeyProperties.AUTH_DEVICE_CREDENTIAL else KeyProperties.AUTH_BIOMETRIC_STRONG
            builder.setUserAuthenticationParameters(0, kinds)
        } else {
            @Suppress("DEPRECATION")
            builder.setUserAuthenticationValidityDurationSeconds(-1)
        }
        KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore").apply {
            initialize(builder.build())
            generateKeyPair()
        }
    }

    override fun has(alias: String) = keyStore.containsAlias(alias)

    override fun delete(alias: String) {
        if (keyStore.containsAlias(alias)) keyStore.deleteEntry(alias)
    }

    // A signature over the key, to unlock with the prompt. Throws
    // KeyPermanentlyInvalidatedException when fingerprints changed since.
    fun signature(alias: String): Signature =
        Signature.getInstance("SHA256withECDSA").apply { initSign(keyStore.getKey(alias, null) as PrivateKey) }
}

// askToSign shows the system's prompt for the person's fingerprint or PIN,
// which unlocks the key for this one signature. The title and description
// say what is signed, in Legato's words; never the agent's.
fun askToSign(
    activity: Activity,
    signature: Signature,
    title: String,
    description: String,
    executor: Executor,
    signed: (Signature) -> Unit,
    failed: (String?) -> Unit,
): CancellationSignal {
    val builder = BiometricPrompt.Builder(activity).setTitle(title).setDescription(description)
    if (Build.VERSION.SDK_INT >= 30) {
        builder.setAllowedAuthenticators(Authenticators.BIOMETRIC_STRONG or Authenticators.DEVICE_CREDENTIAL)
    } else {
        builder.setNegativeButton("Cancel", executor) { _, _ -> failed(null) }
    }
    val cancel = CancellationSignal()
    // The prompt can be refused outright (no permission, no fingerprint or
    // PIN set up): say why on the sheet rather than crash.
    try {
        authenticate(builder, signature, cancel, executor, signed, failed)
    } catch (cause: RuntimeException) {
        failed(cause.message ?: "The fingerprint prompt could not open.")
    }
    return cancel
}

private fun authenticate(
    builder: BiometricPrompt.Builder,
    signature: Signature,
    cancel: CancellationSignal,
    executor: Executor,
    signed: (Signature) -> Unit,
    failed: (String?) -> Unit,
) {
    builder.build().authenticate(BiometricPrompt.CryptoObject(signature), cancel, executor, object : BiometricPrompt.AuthenticationCallback() {
        override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
            val unlocked = result.cryptoObject?.signature
            if (unlocked == null) failed("The key was not unlocked.") else signed(unlocked)
        }

        override fun onAuthenticationError(errorCode: Int, errString: CharSequence?) {
            val cancelled = errorCode == BiometricPrompt.BIOMETRIC_ERROR_USER_CANCELED || errorCode == BiometricPrompt.BIOMETRIC_ERROR_CANCELED
            failed(if (cancelled) null else errString?.toString())
        }
    })
}

// AndroidServerHttp calls a saved server's API with its token, trusting
// the certificate pinned at pairing. Blocking; off the main thread.
class AndroidServerHttp(private val baseUrl: String, private val token: String, private val pin: String?) : ServerHttp {
    override fun call(method: String, path: String, body: JSONObject?): Pair<Int, String> {
        val connection = URL(baseUrl + path).openConnection() as HttpURLConnection
        PinnedTrust.apply(connection, pin)
        try {
            connection.requestMethod = method
            connection.connectTimeout = 8_000
            connection.readTimeout = 70_000
            connection.useCaches = false
            connection.setRequestProperty("Authorization", "Bearer $token")
            connection.setRequestProperty("Accept", "application/json")
            if (body != null) {
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json")
                connection.outputStream.use { it.write(body.toString().toByteArray()) }
            }
            val status = connection.responseCode
            val stream = if (status in 200..299) connection.inputStream else connection.errorStream
            return status to (stream?.use { it.readBytes().toString(Charsets.UTF_8) } ?: "")
        } finally {
            connection.disconnect()
        }
    }
}
