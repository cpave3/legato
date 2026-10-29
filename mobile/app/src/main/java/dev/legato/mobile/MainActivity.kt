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

    // Opens a server's page, at path when one is given (an approval).
    fun open(connection: Connection, path: String = "") {
        state.error.value = ""
        val cached = page
        if (cached != null && cached.connection == connection) {
            if (path.isNotEmpty()) cached.show(path)
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
                    showPage(connection, token, path)
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

    // Signed approvals: the sheet, enrolling this phone, and signing.

    private val keys by lazy { AndroidDeviceKeys() }
    private val main = java.util.concurrent.Executor { runOnUiThread(it) }
    private var sheetDone: ((String, String?, String?) -> Unit)? = null

    private fun client(connection: Connection): ApprovalClient {
        val token = store.token(connection.id) ?: throw IllegalStateException("This server has no saved token. Pair it again.")
        return ApprovalClient(AndroidServerHttp(connection.baseUrl, token, connection.caCertificate))
    }

    // openApprovalSheet shows one approval to sign. done tells whoever asked
    // (a page, or nobody) how it closed.
    fun openApprovalSheet(connection: Connection, id: String, done: (String, String?, String?) -> Unit) {
        sheetDone?.invoke("cancelled", null, null)
        sheetDone = done
        val device = connection.approvalDevice
        if (device == null || !keys.has(approvalKeyAlias(connection.id))) {
            state.sheet.value = SheetState.NotEnrolled(connection, id)
            return
        }
        state.sheet.value = SheetState.Loading(connection, id)
        io.execute {
            val outcome = runCatching {
                val challenge = client(connection).challenge(id, device)
                SheetState.Ready(connection, id, parseSheet(challenge.canonical), device, challenge)
            }
            runOnUiThread {
                if (state.sheet.value?.approvalId != id) return@runOnUiThread
                state.sheet.value = outcome.getOrElse { SheetState.Failed(connection, id, it.message ?: it.toString()) }
            }
        }
    }

    fun closeSheet() {
        val current = state.sheet.value
        state.sheet.value = null
        val done = sheetDone
        sheetDone = null
        val status = when (current) {
            is SheetState.Signed, is SheetState.Denied -> "decided"
            is SheetState.Failed -> "error"
            else -> "cancelled"
        }
        done?.invoke(status, (current as? SheetState.Signed)?.optionLabel, (current as? SheetState.Failed)?.message)
        refreshApprovals()
    }

    // enrollPhone makes this phone's key for a server and has the server
    // accept it.
    fun enrollPhone(connection: Connection, done: (String, String?) -> Unit) {
        io.execute {
            val outcome = runCatching {
                Enrollment(client(connection), keys, android.os.Build.MODEL ?: "Phone").enroll(approvalKeyAlias(connection.id))
            }
            runOnUiThread {
                outcome.onSuccess { result ->
                    store.rememberApprovalDevice(connection.id, result.deviceId)
                    state.connections.value = store.load()
                    done(if (result.status == "active") "enrolled" else "pending", null)
                }.onFailure { done("error", it.message ?: it.toString()) }
            }
        }
    }

    // enrollThenOpen enrolls from the sheet, then shows the approval.
    fun enrollThenOpen(connection: Connection, id: String) {
        state.sheet.value = SheetState.NotEnrolled(connection, id, busy = true)
        enrollPhone(connection) { status, error ->
            val fresh = store.load().firstOrNull { it.id == connection.id } ?: connection
            when (status) {
                "enrolled" -> openApprovalSheet(fresh, id, sheetDone ?: { _, _, _ -> })
                "pending" -> state.sheet.value = SheetState.Failed(fresh, id, "This phone waits to be allowed: approve it on a phone that already signs for ${fresh.name}, then try again.")
                else -> state.sheet.value = SheetState.NotEnrolled(fresh, id, error = error)
            }
        }
    }

    // signDecision unlocks the key with the person's fingerprint or PIN for
    // this one signature, signs the request, the option and the challenge,
    // and sends the decision.
    fun signDecision(ready: SheetState.Ready, option: SheetOption) {
        val alias = approvalKeyAlias(ready.connection.id)
        val signature = try {
            keys.signature(alias)
        } catch (cause: android.security.keystore.KeyPermanentlyInvalidatedException) {
            keys.delete(alias)
            store.rememberApprovalDevice(ready.connection.id, null)
            state.sheet.value = SheetState.NotEnrolled(ready.connection, ready.approvalId, error = "A fingerprint was added or removed, so this phone's key was retired. Enroll again; another enrolled phone, or legato device reset at the host, allows it.")
            return
        }
        val message = signedMessage(ready.challenge.challenge, ready.device, option.id, ready.challenge.canonical)
        state.sheet.value = ready.copy(busy = true, error = null)
        val scope = (ready.model.operations.firstOrNull() ?: ready.model.title) + (ready.model.resources.firstOrNull()?.let { " · $it" } ?: "")
        askToSign(this, signature, "Sign: ${option.label}", "$scope\n${ready.connection.name}", main, signed = { unlocked ->
            val value = runCatching { unlocked.update(message); unlocked.sign() }
            value.onFailure { state.sheet.value = ready.copy(busy = false, error = it.message ?: it.toString()) }
            value.onSuccess { sig ->
                io.execute {
                    val sent = runCatching { client(ready.connection).decide(ready.approvalId, "allow", option.id, ready.device, ready.challenge.challenge, sig) }
                    runOnUiThread {
                        sent.onSuccess {
                            val hash = java.security.MessageDigest.getInstance("SHA-256").digest(message)
                            state.sheet.value = SheetState.Signed(ready.connection, ready.approvalId, ready.model, option.label, ready.device, sig, hash, keys.strongBox || isStrongBox(alias))
                        }.onFailure { state.sheet.value = ready.copy(busy = false, error = it.message ?: it.toString()) }
                    }
                }
            }
        }, failed = { why ->
            state.sheet.value = ready.copy(busy = false, error = why)
        })
    }

    private fun isStrongBox(alias: String): Boolean = runCatching {
        val key = java.security.KeyStore.getInstance("AndroidKeyStore").apply { load(null) }.getKey(alias, null) as java.security.PrivateKey
        val info = java.security.KeyFactory.getInstance(key.algorithm, "AndroidKeyStore").getKeySpec(key, android.security.keystore.KeyInfo::class.java)
        android.os.Build.VERSION.SDK_INT >= 31 && info.securityLevel == android.security.keystore.KeyProperties.SECURITY_LEVEL_STRONGBOX
    }.getOrDefault(false)

    // denyFromSheet denies without a signature: saying no is always safe.
    fun denyFromSheet(ready: SheetState.Ready) {
        state.sheet.value = ready.copy(busy = true, error = null)
        io.execute {
            val sent = runCatching {
                val token = store.token(ready.connection.id) ?: throw IllegalStateException("No saved token.")
                val answer = AndroidServerHttp(ready.connection.baseUrl, token, ready.connection.caCertificate)
                    .call("POST", "/api/approvals/${ready.approvalId}/decision", org.json.JSONObject().put("decision", "deny").put("by", "phone"))
                if (answer.first !in 200..299) throw IllegalStateException(runCatching { org.json.JSONObject(answer.second).optString("error") }.getOrNull() ?: "The server answered ${answer.first}.")
            }
            runOnUiThread {
                sent.onSuccess { state.sheet.value = SheetState.Denied(ready.connection, ready.approvalId, ready.model.title) }
                    .onFailure { state.sheet.value = ready.copy(busy = false, error = it.message ?: it.toString()) }
            }
        }
    }

    // refreshApprovals reads the open approvals of every saved server.
    fun refreshApprovals(done: () -> Unit = {}) {
        val connections = state.connections.value
        io.execute {
            val lists = listOpenApprovals(connections.map { it.id to it.name }) { id -> client(connections.first { it.id == id }) }
            runOnUiThread {
                state.approvals.value = lists
                done()
            }
        }
    }

    fun showApprovals() {
        state.screen.value = Screen.Approvals
        pageVisibility()
        refreshApprovals()
    }

    // openListedApproval opens one approval of the list: the sheet when it
    // needs a signature, else its server's page on it.
    fun openListedApproval(serverId: String, approval: OpenApproval) {
        val connection = state.connections.value.firstOrNull { it.id == serverId } ?: return
        if (approval.signed) openApprovalSheet(connection, approval.id) { _, _, _ -> } else open(connection, "/?approval=${approval.id}")
    }

    private fun showPage(connection: Connection, token: String, path: String = "") {
        closePage()
        page = ServerPage(this, connection, token, AndroidMicrophone(this, ::askForMicrophone), pageCallbacks, path)
        state.screen.value = Screen.Web(connection)
        pageVisibility()
    }

    private fun closePage() {
        page?.destroy()
        page = null
        state.canGoBack.value = false
    }

    private val pageCallbacks = object : ServerPage.Callbacks {
        override fun openApproval(connection: Connection, id: String, done: (String, String?, String?) -> Unit) {
            runOnUiThread { openApprovalSheet(store.load().firstOrNull { it.id == connection.id } ?: connection, id, done) }
        }

        override fun enroll(connection: Connection, done: (String, String?) -> Unit) {
            runOnUiThread { enrollPhone(store.load().firstOrNull { it.id == connection.id } ?: connection, done) }
        }

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
        runCatching { keys.delete(approvalKeyAlias(connection.id)) }
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
        if (link.startsWith("legato://approval")) {
            openApproval(link)
            return
        }
        if (!link.startsWith("legato://pair")) return
        try {
            followPairingLink(link)
        } catch (cause: IllegalArgumentException) {
            state.error.value = cause.message ?: cause.toString()
        }
    }

    // A notification of an approval opens its server's page on it.
    private fun openApproval(link: String) {
        val approval = parseApprovalUri(link)
        if (approval == null) {
            state.error.value = "That approval link could not be read."
            return
        }
        val connection = serverForApproval(state.connections.value, approval)
        if (connection == null) {
            state.error.value = "No saved server is at ${approval.baseUrl}. Open the approval from that server's page."
            return
        }
        if (connection.approvalDevice != null) openApprovalSheet(connection, approval.id) { _, _, _ -> } else open(connection, approval.path)
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
    data object Approvals : Screen
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
    // The approval sheet, over whatever is on screen.
    val sheet = mutableStateOf<SheetState?>(null)
    // The open approvals of every saved server.
    val approvals = mutableStateOf<List<ServerApprovals>?>(null)
}
