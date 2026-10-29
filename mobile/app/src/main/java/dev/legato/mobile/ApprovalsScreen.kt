package dev.legato.mobile

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

// The open approvals of every saved server. A signed one opens the sheet; any
// other opens its server's page on it.
@Composable
fun ApprovalsScreen(activity: MainActivity) {
    val lists = activity.state.approvals.value
    LaunchedEffect(Unit) {
        while (true) {
            delay(10_000)
            activity.refreshApprovals()
        }
    }
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 32.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp),
    ) {
        item {
            Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                RoundButton(LucideIcons.ChevronLeft, "Back") { activity.back() }
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text("Approvals", fontSize = 28.sp, lineHeight = 34.sp, fontWeight = FontWeight.SemiBold)
                    val count = lists?.sumOf { it.approvals.size }
                    Text(
                        when (count) {
                            null -> "Reading every server"
                            0 -> "Nothing waits for you"
                            1 -> "1 waits for you"
                            else -> "$count wait for you"
                        },
                        color = Lw.text2, fontSize = 13.sp,
                    )
                }
                RoundButton(LucideIcons.RotateCw, "Refresh") { activity.refreshApprovals() }
            }
        }
        if (lists == null) {
            item {
                Box(Modifier.fillMaxWidth().padding(top = 40.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(Modifier.size(20.dp), color = Lw.link, strokeWidth = 2.dp)
                }
            }
        } else {
            lists.forEach { server ->
                item(key = server.serverId) {
                    Group(server.serverName) {
                        when {
                            server.error != null -> Text(server.error, color = Lw.danger, fontSize = 14.sp, modifier = Modifier.padding(14.dp))
                            server.approvals.isEmpty() -> Text("Nothing waiting", color = Lw.faint, fontSize = 14.sp, modifier = Modifier.padding(14.dp))
                            else -> server.approvals.forEachIndexed { index, approval ->
                                if (index > 0) Divider()
                                ApprovalRow(approval) { activity.openListedApproval(server.serverId, approval) }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ApprovalRow(approval: OpenApproval, open: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 64.dp).clickable(role = Role.Button, onClickLabel = "Open", onClick = open)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).background(if (approval.signed) Lw.link else Lw.needs, RoundedCornerShape(2.dp)))
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(approval.title, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(
                approval.source.replace('_', ' ') + if (approval.signed) " · needs your signature" else "",
                style = TextStyle(fontFamily = GeistMono, fontSize = 12.sp, color = if (approval.signed) Lw.link else Lw.text2),
            )
        }
        if (approval.signed) {
            Box(
                Modifier.clip(RoundedCornerShape(8.dp)).background(Lw.accent.copy(alpha = 0.16f)).padding(8.dp),
            ) { Icon(LucideIcons.Fingerprint, contentDescription = "Signed approval", tint = Lw.link, modifier = Modifier.size(18.dp)) }
        } else {
            Icon(LucideIcons.ChevronRight, contentDescription = null, tint = Lw.faint, modifier = Modifier.size(16.dp))
        }
    }
}
