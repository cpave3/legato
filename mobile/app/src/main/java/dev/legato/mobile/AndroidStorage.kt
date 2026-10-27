package dev.legato.mobile

import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import android.util.Log
import org.json.JSONObject
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

private const val TAG = "LegatoStorage"

fun connectionStore(context: Context): ConnectionStore {
    val prefs = context.getSharedPreferences("legato.connections", Context.MODE_PRIVATE)
    val text = object : ConnectionStore.Text {
        override fun read(): String? = prefs.getString("connections.v1", null)
        override fun write(value: String) {
            prefs.edit().putString("connections.v1", value).apply()
        }
    }
    return ConnectionStore(text, SecureStoreVault(context)).also { store ->
        store.adoptLegacy { readExpoConnections(context) }
    }
}

// The Expo app kept its list of servers in React Native's AsyncStorage, a
// SQLite file of keys and values.
private fun readExpoConnections(context: Context): String? {
    val file = context.getDatabasePath("RKStorage")
    if (!file.exists()) return null
    return try {
        SQLiteDatabase.openDatabase(file.path, null, SQLiteDatabase.OPEN_READONLY).use { db ->
            db.rawQuery("SELECT value FROM catalystLocalStorage WHERE key = ?", arrayOf("legato.mobile.connections.v1")).use { row ->
                if (row.moveToFirst()) row.getString(0) else null
            }
        }
    } catch (cause: Exception) {
        Log.w(TAG, "Could not read the Expo app's servers", cause)
        null
    }
}

// SecureStoreVault keeps tokens exactly as expo-secure-store kept them: each
// one encrypted with an AES key in the Android Keystore, in the shared
// preferences file "SecureStore". Tokens the Expo app saved are read as they
// are, and the Expo app could read the ones saved here.
class SecureStoreVault(context: Context) : TokenVault {
    private val prefs = context.getSharedPreferences("SecureStore", Context.MODE_PRIVATE)
    private val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }

    override fun read(id: String): String? {
        val raw = prefs.getString(prefKey(id), null) ?: return null
        return try {
            val item = JSONObject(raw)
            if (item.optString("scheme") != "aes") return null
            val alias = if (item.optBoolean("usesKeystoreSuffix")) KEY_ALIAS else LEGACY_KEY_ALIAS
            val key = (keyStore.getEntry(alias, null) as? KeyStore.SecretKeyEntry)?.secretKey ?: return null
            val cipher = Cipher.getInstance(CIPHER)
            val iv = Base64.decode(item.getString("iv"), Base64.DEFAULT)
            cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(item.getInt("tlen"), iv))
            String(cipher.doFinal(Base64.decode(item.getString("ct"), Base64.DEFAULT)), Charsets.UTF_8)
        } catch (cause: Exception) {
            // A token whose key is gone (the app was reinstalled) cannot be
            // read; the person pairs again.
            Log.w(TAG, "Could not read a saved token", cause)
            null
        }
    }

    override fun write(id: String, token: String) {
        val cipher = Cipher.getInstance(CIPHER)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val spec = cipher.parameters.getParameterSpec(GCMParameterSpec::class.java)
        val item = JSONObject()
            .put("ct", Base64.encodeToString(cipher.doFinal(token.toByteArray(Charsets.UTF_8)), Base64.NO_WRAP))
            .put("iv", Base64.encodeToString(spec.iv, Base64.NO_WRAP))
            .put("tlen", spec.tLen)
            .put("scheme", "aes")
            .put("usesKeystoreSuffix", true)
            .put("keystoreAlias", KEYCHAIN)
            .put("requireAuthentication", false)
        prefs.edit().putString(prefKey(id), item.toString()).commit()
    }

    override fun remove(id: String) {
        prefs.edit().remove(prefKey(id)).commit()
    }

    private fun key(): SecretKey {
        (keyStore.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        generator.init(
            KeyGenParameterSpec.Builder(KEY_ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setKeySize(256)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .build(),
        )
        return generator.generateKey()
    }

    private fun prefKey(id: String) = "$KEYCHAIN-legato.mobile.token.$id"

    private companion object {
        const val CIPHER = "AES/GCM/NoPadding"
        const val KEYCHAIN = "key_v1"
        const val KEY_ALIAS = "$CIPHER:$KEYCHAIN:keystoreUnauthenticated"
        const val LEGACY_KEY_ALIAS = "$CIPHER:$KEYCHAIN"
    }
}
