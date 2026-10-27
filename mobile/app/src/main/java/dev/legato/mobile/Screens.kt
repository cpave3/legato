package dev.legato.mobile

import android.view.ViewGroup
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.pulltorefresh.PullToRefreshDefaults
import androidx.compose.material3.pulltorefresh.rememberPullToRefreshState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.coroutines.delay

// The colours of the web interface's new look (web/src/index.css, `--color-lw-*`).
private object Lw {
    val rail = Color(0xFF0C0C0F)
    val ground = Color(0xFF111114)
    val surface = Color(0xFF17171B)
    val raised = Color(0xFF1D1D22)
    val line = Color(0xFF26262D)
    val lineStrong = Color(0xFF31313A)
    val divider = Color(0xFF222228)
    val text = Color(0xFFE6E5EB)
    val text2 = Color(0xFF9A99A5)
    val faint = Color(0xFF66656F)
    val accent = Color(0xFF6D5EF6)
    val accentRing = Color(0xFF4F46B8)
    val link = Color(0xFFA99CFF)
    val live = Color(0xFF3ECF8E)
    val needs = Color(0xFFF0B44C)
    val error = Color(0xFFF0646B)
    val errorSoft = Color(0xFF33181B)
    val danger = Color(0xFFF0868C)
    val dangerLine = Color(0xFF5A2A2E)
    val webPage = Color(0xFF0A0A0F)
}

// Geist is one variable font file; each weight is a setting of it.
@OptIn(androidx.compose.ui.text.ExperimentalTextApi::class)
private fun geist(resource: Int, weight: Int) =
    Font(resource, FontWeight(weight), variationSettings = FontVariation.Settings(FontVariation.weight(weight)))

private val GeistSans = FontFamily(geist(R.font.geist, 400), geist(R.font.geist, 500), geist(R.font.geist, 600))
private val GeistMono = FontFamily(geist(R.font.geist_mono, 400))

private val mono = TextStyle(fontFamily = GeistMono, fontSize = 12.sp, color = Lw.text2)

@Composable
fun LegatoApp(activity: MainActivity) {
    val state = activity.state
    MaterialTheme(colorScheme = darkColorScheme(primary = Lw.accent, background = Lw.ground, surface = Lw.raised, onSurface = Lw.text)) {
        ProvideTextStyle(TextStyle(fontFamily = GeistSans, color = Lw.text, fontSize = 15.sp)) {
            val screen = state.screen.value
            val web = screen is Screen.Web
            Box(Modifier.fillMaxSize().background(if (web) Lw.rail else Lw.ground).windowInsetsPadding(WindowInsets.safeDrawing)) {
                BackHandler(enabled = screen != Screen.Connections) { activity.back() }
                when (screen) {
                    Screen.Connections -> ConnectionsScreen(activity)
                    is Screen.Edit -> EditScreen(activity, screen.target)
                    is Screen.Web -> WebScreen(activity, screen.connection)
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ConnectionsScreen(activity: MainActivity) {
    val state = activity.state
    var refreshing by remember { mutableStateOf(false) }
    val connections = state.connections.value
    LaunchedEffect(connections) {
        while (true) {
            activity.refreshReachability()
            delay(15_000)
        }
    }
    val pull = rememberPullToRefreshState()
    PullToRefreshBox(
        isRefreshing = refreshing,
        state = pull,
        onRefresh = {
            refreshing = true
            activity.refreshReachability { refreshing = false }
        },
        indicator = {
            PullToRefreshDefaults.Indicator(
                state = pull,
                isRefreshing = refreshing,
                containerColor = Lw.raised,
                color = Lw.link,
                modifier = Modifier.align(Alignment.TopCenter),
            )
        },
    ) {
        LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(18.dp),
        ) {
            item { ServersHeader(activity, connections) }
            if (state.error.value.isNotEmpty()) item { ErrorNote(state.error.value) }
            item {
                if (connections.isEmpty()) {
                    EmptyServers(activity)
                } else {
                    Group(null) {
                        connections.forEachIndexed { index, connection ->
                            if (index > 0) Divider()
                            ServerRow(activity, connection)
                        }
                    }
                }
            }
            item {
                Group("Pairing") {
                    LinkRow("Pair with QR code", LucideIcons.ScanLine) { activity.scanPairingCode() }
                    Divider()
                    LinkRow("Add by address", LucideIcons.Link) { activity.startAdding() }
                    Divider()
                    LinkRow("Certificate settings", LucideIcons.Shield) { activity.openCertificateSettings() }
                }
            }
        }
    }
}

@Composable
private fun ServersHeader(activity: MainActivity, connections: List<Connection>) {
    val reachability = activity.state.reachability.value
    val online = connections.count { reachability[it.id] == Reachability.ONLINE }
    val checking = connections.any { (reachability[it.id] ?: Reachability.CHECKING) == Reachability.CHECKING }
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text("Servers", fontSize = 28.sp, lineHeight = 34.sp, fontWeight = FontWeight.SemiBold)
            Text(
                when {
                    connections.isEmpty() -> "None paired yet"
                    else -> "${connections.size} ${if (connections.size == 1) "server" else "servers"} · " +
                        if (checking && online == 0) "checking" else "$online online"
                },
                color = Lw.text2,
                fontSize = 13.sp,
            )
        }
        RoundButton(LucideIcons.ScanLine, "Pair with QR code") { activity.scanPairingCode() }
        Spacer(Modifier.width(8.dp))
        RoundButton(LucideIcons.Plus, "Add server", filled = true) { activity.startAdding() }
    }
}

@Composable
private fun EmptyServers(activity: MainActivity) {
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(Lw.surface).border(1.dp, Lw.line, RoundedCornerShape(12.dp)).padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text("No servers yet", fontWeight = FontWeight.SemiBold)
        Text("Run legato pair on the machine Legato runs on, then scan the code it prints.", color = Lw.text2, fontSize = 14.sp, lineHeight = 20.sp)
        Spacer(Modifier.height(8.dp))
        PrimaryButton("Scan QR code") { activity.scanPairingCode() }
    }
}

@Composable
private fun ServerRow(activity: MainActivity, connection: Connection) {
    val state = activity.state
    val status = state.reachability.value[connection.id] ?: Reachability.CHECKING
    val version = state.versions.value[connection.id] ?: connection.serverVersion
    val opening = state.opening.value == connection.id
    var menu by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().heightIn(min = 64.dp)
            .clickable(enabled = !opening, role = Role.Button, onClickLabel = "Open") { activity.open(connection) }
            .padding(start = 14.dp, end = 4.dp, top = 10.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier.size(10.dp).background(
                when (status) {
                    Reachability.CHECKING -> Lw.needs
                    Reachability.ONLINE -> Lw.live
                    Reachability.OFFLINE -> Lw.faint
                },
                CircleShape,
            ),
        )
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(connection.name, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val address = connection.baseUrl.removePrefix("http://").removePrefix("https://")
            val detail = when (status) {
                Reachability.CHECKING -> "checking"
                Reachability.ONLINE -> version?.let(::compactVersion) ?: "online"
                Reachability.OFFLINE -> "offline"
            }
            Text("$address · $detail", style = mono, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (opening) {
            Box(Modifier.size(44.dp), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(Modifier.size(16.dp), color = Lw.link, strokeWidth = 2.dp)
            }
        } else {
            Box {
                IconAction(LucideIcons.Ellipsis, "More for ${connection.name}") { menu = true }
                ServerMenu(activity, connection, expanded = menu) { menu = false }
            }
        }
        Icon(LucideIcons.ChevronRight, contentDescription = null, tint = Lw.faint, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(10.dp))
    }
}

@Composable
private fun ServerMenu(activity: MainActivity, connection: Connection, expanded: Boolean, close: () -> Unit) {
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = close,
        containerColor = Lw.raised,
        shape = RoundedCornerShape(12.dp),
        border = androidx.compose.foundation.BorderStroke(1.dp, Lw.lineStrong),
    ) {
        MenuItem("Edit", LucideIcons.Pencil) { close(); activity.startEditing(connection) }
        if (connection.caCertificate != null) MenuItem("Save CA certificate", LucideIcons.Download) { close(); activity.saveCertificate(connection) }
        MenuItem("Remove", LucideIcons.Trash, danger = true) { close(); activity.remove(connection) }
    }
}

@Composable
private fun MenuItem(label: String, icon: ImageVector, danger: Boolean = false, onClick: () -> Unit) {
    val color = if (danger) Lw.danger else Lw.text
    DropdownMenuItem(
        text = { Text(label, color = color, fontSize = 15.sp) },
        leadingIcon = { Icon(icon, contentDescription = null, tint = if (danger) Lw.danger else Lw.text2, modifier = Modifier.size(16.dp)) },
        onClick = onClick,
        modifier = Modifier.heightIn(min = 44.dp),
    )
}

private fun compactVersion(version: String) = if (version.length > 16) version.take(15) + "…" else version

@Composable
private fun Group(label: String?, content: @Composable () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (label != null) {
            Text(label.uppercase(), color = Lw.text2, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 1.sp, modifier = Modifier.padding(start = 4.dp))
        }
        Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(Lw.surface).border(1.dp, Lw.line, RoundedCornerShape(12.dp))) {
            content()
        }
    }
}

@Composable
private fun Divider() = HorizontalDivider(color = Lw.divider, thickness = 1.dp)

@Composable
private fun LinkRow(label: String, icon: ImageVector, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 52.dp).clickable(role = Role.Button, onClick = onClick).padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = Lw.text2, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(12.dp))
        Text(label, modifier = Modifier.weight(1f))
        Icon(LucideIcons.ChevronRight, contentDescription = null, tint = Lw.faint, modifier = Modifier.size(16.dp))
    }
}

@Composable
private fun RoundButton(icon: ImageVector, description: String, filled: Boolean = false, onClick: () -> Unit) {
    Box(
        Modifier.size(44.dp).clip(CircleShape)
            .background(if (filled) Lw.accent else Lw.raised)
            .border(1.dp, if (filled) Lw.accentRing else Lw.line, CircleShape)
            .clickable(role = Role.Button, onClickLabel = description, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = description, tint = if (filled) Color.White else Lw.text, modifier = Modifier.size(20.dp))
    }
}

@Composable
private fun IconAction(icon: ImageVector, description: String, size: Dp = 44.dp, onClick: () -> Unit) {
    Box(
        Modifier.size(size).clip(CircleShape).clickable(role = Role.Button, onClickLabel = description, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = description, tint = Lw.text2, modifier = Modifier.size(20.dp))
    }
}

@Composable
private fun ErrorNote(message: String) {
    Text(
        message,
        color = Lw.danger,
        fontSize = 14.sp,
        lineHeight = 20.sp,
        modifier = Modifier.fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(Lw.errorSoft)
            .border(1.dp, Lw.dangerLine, RoundedCornerShape(12.dp))
            .padding(horizontal = 14.dp, vertical = 12.dp),
    )
}

@Composable
private fun EditScreen(activity: MainActivity, target: Connection?) {
    val state = activity.state
    val draft = state.draft.value
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(start = 4.dp, end = 16.dp, top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            IconAction(LucideIcons.ChevronLeft, "Back to servers") { activity.showServers() }
            Text(if (target != null) "Edit server" else "Add server", fontSize = 24.sp, lineHeight = 30.sp, fontWeight = FontWeight.SemiBold)
        }
        Column(
            Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            if (state.error.value.isNotEmpty()) ErrorNote(state.error.value)
            Field("Name", draft.name, "Desktop") { state.draft.value = draft.copy(name = it) }
            Field("Server URL", draft.url, "http://192.168.1.20:3080", monospace = true, keyboard = KeyboardType.Uri) { state.draft.value = draft.copy(url = it) }
            Field(
                if (target != null) "New token" else "Token",
                draft.token,
                if (target != null) "Leave blank to keep the current one" else "From legato auth token",
                monospace = true,
                secret = true,
            ) { state.draft.value = draft.copy(token = it) }
            Spacer(Modifier.height(4.dp))
            PrimaryButton(if (target != null) "Save" else "Save and open") { activity.saveDraft(target) }
            if (target == null) SecondaryButton("Scan QR code instead", LucideIcons.ScanLine) { activity.scanPairingCode() }
        }
    }
}

@Composable
private fun Field(
    label: String,
    value: String,
    placeholder: String,
    monospace: Boolean = false,
    secret: Boolean = false,
    keyboard: KeyboardType = KeyboardType.Text,
    onChange: (String) -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, color = Lw.text2, fontSize = 13.sp, fontWeight = FontWeight.Medium, modifier = Modifier.padding(start = 2.dp))
        val style = TextStyle(fontFamily = if (monospace) GeistMono else GeistSans, fontSize = if (monospace) 14.sp else 15.sp, color = Lw.text)
        BasicTextField(
            value = value,
            onValueChange = onChange,
            singleLine = true,
            textStyle = style,
            cursorBrush = SolidColor(Lw.link),
            visualTransformation = if (secret) PasswordVisualTransformation() else VisualTransformation.None,
            keyboardOptions = KeyboardOptions(keyboardType = if (secret) KeyboardType.Password else keyboard, autoCorrectEnabled = false),
            modifier = Modifier.fillMaxWidth().onFocusChanged { focused = it.isFocused },
            decorationBox = { inner ->
                Box(
                    Modifier.fillMaxWidth().heightIn(min = 48.dp)
                        .clip(RoundedCornerShape(10.dp))
                        .background(Lw.surface)
                        .border(1.dp, if (focused) Lw.accent else Lw.lineStrong, RoundedCornerShape(10.dp))
                        .padding(horizontal = 14.dp),
                    contentAlignment = Alignment.CenterStart,
                ) {
                    // A placeholder is a hint in words, not a value, so it is never monospaced.
                    if (value.isEmpty()) Text(placeholder, style = style.copy(fontFamily = GeistSans, fontSize = 15.sp, color = Lw.faint))
                    inner()
                }
            },
        )
    }
}

@Composable
private fun PrimaryButton(label: String, onClick: () -> Unit) {
    Box(
        Modifier.fillMaxWidth().height(48.dp).clip(RoundedCornerShape(12.dp)).background(Lw.accent)
            .clickable(role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, color = Color.White, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun SecondaryButton(label: String, icon: ImageVector, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().height(48.dp).clip(RoundedCornerShape(12.dp)).background(Lw.surface)
            .border(1.dp, Lw.line, RoundedCornerShape(12.dp))
            .clickable(role = Role.Button, onClick = onClick),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = Lw.text2, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Text(label, fontWeight = FontWeight.Medium)
    }
}

@Composable
private fun WebScreen(activity: MainActivity, connection: Connection) {
    Column(Modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().height(44.dp).background(Lw.rail).padding(horizontal = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconAction(LucideIcons.ChevronLeft, "Back to servers") { activity.showServers() }
            Box(Modifier.size(8.dp).background(Lw.live, CircleShape))
            Spacer(Modifier.width(8.dp))
            Text(connection.name, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.width(8.dp))
            Text(
                connection.baseUrl.removePrefix("http://").removePrefix("https://"),
                style = mono.copy(color = Lw.faint),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            IconAction(LucideIcons.RotateCw, "Reload") { activity.currentPage()?.reload() }
        }
        HorizontalDivider(color = Lw.divider, thickness = 1.dp)
        val page = activity.currentPage()
        if (page != null) {
            AndroidView(
                factory = {
                    // The page outlives this screen; take it from wherever it was shown last.
                    (page.view.parent as? ViewGroup)?.removeView(page.view)
                    page.view
                },
                modifier = Modifier.fillMaxSize().background(Lw.webPage),
            )
        }
    }
}
