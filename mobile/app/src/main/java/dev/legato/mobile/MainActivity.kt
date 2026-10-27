package dev.legato.mobile

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import android.os.Looper
import android.provider.Settings
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebSettings
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.mutableStateOf
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.codescanner.GmsBarcodeScannerOptions
import com.google.mlkit.vision.codescanner.GmsBarcodeScanning
import java.io.IOException
import java.time.Instant
import java.util.UUID
import java.util.concurrent.Executors

class MainActivity : ComponentActivity() {
    private val io = Executors.newCachedThreadPool()
    private lateinit var store: ConnectionStore
    val state = AppState()
    private val discovery by lazy { AndroidDiscovery(this) { state.discovered.value = it } }

    // The open server's page, kept while the person looks at the server list.
    private var page: ServerPage? = null
    private var started = false

    private var micAnswer: ((Boolean) -> Unit)? = null
    private val micPermission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        micAnswer?.invoke(granted)
        micAnswer = null
    }
    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private val fileChooser = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        fileCallback?.onReceiveValue(WebChromeClient.FileChooserParams.parseResult(result.resultCode, result.data))
        fileCallback = null
    }
    private var certificateToSave: String? = null
    private val certificateFile = registerForActivityResult(ActivityResultContracts.CreateDocument("application/x-x509-ca-cert")) { uri ->
        val pem = certificateToSave
        certificateToSave = null
        if (uri == null || pem == null) return@registerForActivityResult
        try {
            contentResolver.openOutputStream(uri)?.use { it.write(pem.toByteArray()) }
            state.error.value = "Saved the certificate. Tap Certificate settings, choose Install a certificate → CA certificate, then select the file."
        } catch (cause: IOException) {
            state.error.value = cause.message ?: cause.toString()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge(SystemBarStyle.dark(android.graphics.Color.TRANSPARENT), SystemBarStyle.dark(android.graphics.Color.TRANSPARENT))
        store = connectionStore(this)
        state.connections.value = store.load()
        setContent { LegatoApp(this) }
        handlePairingLink(intent)
        // Load the WebView's engine while the person reads the list, so the
        // first server opens without that wait.
        Looper.myQueue().addIdleHandler {
            runCatching { WebSettings.getDefaultUserAgent(this) }
            false
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handlePairingLink(intent)
    }

    override fun onStart() {
        super.onStart()
        started = true
        pageVisibility()
        if (state.screen.value == Screen.Pair) discovery.start()
    }

    override fun onStop() {
        started = false
        pageVisibility()
        discovery.stop()
        super.onStop()
    }

    override fun onDestroy() {
        page?.destroy()
        io.shutdown()
        super.onDestroy()
    }

    fun pageVisibility() {
        page?.setInFront(started && state.screen.value is Screen.Web)
    }

    // Actions the screens call.

    fun currentPage(): ServerPage? = page

    fun showServers() {
        state.screen.value = Screen.Connections
        discovery.stop()
        pageVisibility()
    }

    // Pairing by phrase: pick a server found on the network, or type its
    // address, then the four words legato pair or the web UI shows.

    fun startPairing() {
        state.error.value = ""
        state.pairDraft.value = PairDraft()
        state.screen.value = Screen.Pair
        pageVisibility()
        discovery.start()
    }

    fun pickDiscovered(server: DiscoveredServer) {
        state.error.value = ""
        state.pairDraft.value = state.pairDraft.value.copy(name = server.name, address = server.baseUrl)
    }

    fun pairByPhrase() {
        val draft = state.pairDraft.value
        if (draft.busy) return
        val reading = PhraseInput.read(draft.phrase)
        val baseUrl = try {
            normalizeServerUrl(draft.address)
        } catch (cause: IllegalArgumentException) {
            state.error.value = if (draft.address.isBlank()) "Choose a server, or type its address." else cause.message ?: cause.toString()
            return
        }
        if (!reading.complete) {
            state.error.value = "Type the ${PhraseInput.WORDS} words the server shows."
            return
        }
        state.error.value = ""
        state.pairDraft.value = draft.copy(busy = true)
        val device = deviceName(this)
        io.execute {
            val outcome = runCatching {
                PhrasePairing(HttpPairingTransport(baseUrl), device).pair(reading.words.filterNotNull())
            }
            runOnUiThread {
                state.pairDraft.value = state.pairDraft.value.copy(busy = false)
                outcome.onSuccess { paired ->
                    val name = paired.name.ifBlank { draft.name }.ifBlank { baseUrl.substringAfter("://") }
                    val saved = store.save(Connection(UUID.randomUUID().toString(), name, baseUrl, Instant.now().toString(), caCertificate = paired.caCertificate), paired.token)
                    if (page?.connection?.id == saved.id) closePage()
                    state.connections.value = store.load()
                    showServers()
                    open(saved)
                }.onFailure { cause ->
                    state.error.value = when (cause) {
                        is PairingFailure -> cause.message.orEmpty()
                        is IOException -> "Could not reach $baseUrl. Check that the phone is on the same network and Legato's web server is running."
                        else -> cause.message ?: cause.toString()
                    }
                }
            }
        }
    }

    fun back() {
        when (state.screen.value) {
            is Screen.Web -> if (page?.canGoBack() == true) page?.goBack() else showServers()
            else -> showServers()
        }
    }

    fun refreshReachability(done: () -> Unit = {}) {
        val connections = state.connections.value
        if (connections.isEmpty()) {
            done()
            return
        }
        io.execute {
            val results = connections.map { connection -> connection to probeHealth(connection.baseUrl, connection.caCertificate) }
            runOnUiThread {
                state.reachability.value = results.associate { (connection, health) -> connection.id to if (health.online) Reachability.ONLINE else Reachability.OFFLINE }
                state.versions.value = state.versions.value + results.mapNotNull { (connection, health) -> health.version?.let { connection.id to it } }
                done()
            }
        }
    }

    fun open(connection: Connection) {
        state.error.value = ""
        val cached = page
        if (cached != null && cached.connection == connection) {
            state.screen.value = Screen.Web(connection)
            pageVisibility()
            return
        }
        state.opening.value = connection.id
        io.execute {
            val outcome = runCatching {
                val token = store.token(connection.id) ?: throw IllegalStateException("This connection has no saved token. Re-pair it.")
                val meta = probeServer(connection.baseUrl, connection.caCertificate)
                if (meta.compatibility == Compatibility.UNSUPPORTED) throw IllegalStateException("This server requires a newer mobile app")
                meta.version?.let { store.rememberVersion(connection.id, it) }
                token
            }
            runOnUiThread {
                state.opening.value = null
                outcome.onSuccess { token ->
                    state.reachability.value = state.reachability.value + (connection.id to Reachability.ONLINE)
                    showPage(connection, token)
                }.onFailure { cause ->
                    state.error.value = if (cause is IOException) {
                        "Could not reach this Legato server. Check that the phone is on the same network, the server is running, and the connection URL uses this machine's current IP address."
                    } else {
                        cause.message ?: cause.toString()
                    }
                }
            }
        }
    }

    private fun showPage(connection: Connection, token: String) {
        closePage()
        page = ServerPage(this, connection, token, AndroidMicrophone(this, ::askForMicrophone), pageCallbacks)
        state.screen.value = Screen.Web(connection)
        pageVisibility()
    }

    private fun closePage() {
        page?.destroy()
        page = null
        state.canGoBack.value = false
    }

    private val pageCallbacks = object : ServerPage.Callbacks {
        override fun canGoBackChanged(canGoBack: Boolean) {
            state.canGoBack.value = canGoBack
        }

        override fun unavailable(reason: String) {
            state.error.value = reason
            closePage()
            showServers()
        }

        override fun openExternally(url: String) {
            try {
                startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
            } catch (cause: ActivityNotFoundException) {
                state.error.value = "Blocked link: $url"
            }
        }

        override fun chooseFiles(params: WebChromeClient.FileChooserParams, callback: ValueCallback<Array<Uri>>) {
            fileCallback?.onReceiveValue(null)
            fileCallback = callback
            try {
                fileChooser.launch(params.createIntent())
            } catch (cause: ActivityNotFoundException) {
                fileCallback = null
                callback.onReceiveValue(null)
            }
        }
    }

    private fun askForMicrophone(granted: (Boolean) -> Unit) {
        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
            granted(true)
            return
        }
        micAnswer?.invoke(false)
        micAnswer = granted
        micPermission.launch(Manifest.permission.RECORD_AUDIO)
    }

    fun startAdding(code: PairingCode? = null) {
        state.error.value = ""
        state.draft.value = Draft(
            name = code?.suggestedName.orEmpty(),
            url = code?.baseUrl.orEmpty(),
            token = code?.token.orEmpty(),
            caCertificate = code?.caCertificate,
        )
        state.screen.value = Screen.Edit(null)
        pageVisibility()
    }

    fun startEditing(connection: Connection) {
        state.error.value = ""
        state.draft.value = Draft(connection.name, connection.baseUrl, "", connection.caCertificate)
        state.screen.value = Screen.Edit(connection)
    }

    fun saveDraft(target: Connection?) {
        val draft = state.draft.value
        try {
            val name = draft.name.trim()
            require(name.isNotEmpty()) { "Give this connection a name" }
            val baseUrl = normalizeServerUrl(draft.url)
            if (target != null) {
                store.update(target.id, name, baseUrl, draft.token)
                if (page?.connection?.id == target.id) closePage()
                state.connections.value = store.load()
                showServers()
                return
            }
            val token = draft.token.trim()
            require(token.isNotEmpty()) { "Token is required" }
            val saved = store.save(Connection(UUID.randomUUID().toString(), name, baseUrl, Instant.now().toString(), caCertificate = draft.caCertificate), token)
            if (page?.connection?.id == saved.id) closePage()
            state.connections.value = store.load()
            showServers()
            open(saved)
        } catch (cause: IllegalArgumentException) {
            state.error.value = cause.message ?: cause.toString()
        }
    }

    fun remove(connection: Connection) {
        store.remove(connection.id)
        if (page?.connection?.id == connection.id) closePage()
        state.connections.value = store.load()
    }

    fun scanPairingCode() {
        val options = GmsBarcodeScannerOptions.Builder().setBarcodeFormats(Barcode.FORMAT_QR_CODE).build()
        GmsBarcodeScanning.getClient(this, options).startScan()
            .addOnSuccessListener { barcode ->
                try {
                    followPairingLink(barcode.rawValue.orEmpty())
                } catch (cause: IllegalArgumentException) {
                    state.error.value = "Not a Legato pairing QR code"
                }
            }
            .addOnFailureListener { cause ->
                state.error.value = "The QR scanner could not start (${cause.message}). Enter the URL and token manually."
            }
    }

    fun saveCertificate(connection: Connection) {
        val pem = connection.caCertificate
        if (pem == null) {
            state.error.value = "This pairing code did not include a CA certificate. Run a current `legato pair` and re-pair the connection."
            return
        }
        certificateToSave = pem
        certificateFile.launch("legato-${connection.name.replace(Regex("[^a-z0-9]+", RegexOption.IGNORE_CASE), "-").lowercase()}-ca.crt")
    }

    fun openCertificateSettings() {
        try {
            startActivity(Intent(Settings.ACTION_SECURITY_SETTINGS))
        } catch (cause: ActivityNotFoundException) {
            state.error.value = cause.message ?: cause.toString()
        }
    }

    private fun handlePairingLink(intent: Intent?) {
        val link = intent?.data?.toString() ?: return
        if (!link.startsWith("legato://pair")) return
        try {
            followPairingLink(link)
        } catch (cause: IllegalArgumentException) {
            state.error.value = cause.message ?: cause.toString()
        }
    }

    // A code with a phrase pairs at once; one with a token fills in the form.
    private fun followPairingLink(link: String) {
        val phrase = parsePhraseUri(link)
        if (phrase == null) {
            startAdding(parsePairUri(link))
            return
        }
        startPairing()
        state.pairDraft.value = PairDraft(address = phrase.baseUrl, phrase = phrase.words.joinToString(" "))
        pairByPhrase()
    }
}

enum class Reachability { CHECKING, ONLINE, OFFLINE }

sealed interface Screen {
    data object Connections : Screen
    data object Pair : Screen
    data class Edit(val target: Connection?) : Screen
    data class Web(val connection: Connection) : Screen
}

data class PairDraft(val name: String = "", val address: String = "", val phrase: String = "", val busy: Boolean = false)

data class Draft(val name: String = "", val url: String = "", val token: String = "", val caCertificate: String? = null)

class AppState {
    val screen = mutableStateOf<Screen>(Screen.Connections)
    val connections = mutableStateOf<List<Connection>>(emptyList())
    val reachability = mutableStateOf<Map<String, Reachability>>(emptyMap())
    val versions = mutableStateOf<Map<String, String>>(emptyMap())
    val error = mutableStateOf("")
    val draft = mutableStateOf(Draft())
    val canGoBack = mutableStateOf(false)
    val opening = mutableStateOf<String?>(null)
    val pairDraft = mutableStateOf(PairDraft())
    val discovered = mutableStateOf<List<DiscoveredServer>>(emptyList())
}
