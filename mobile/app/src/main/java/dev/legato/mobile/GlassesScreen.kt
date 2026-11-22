package dev.legato.mobile

import androidx.compose.foundation.background
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The glasses link: its switch, the pairing code the Even G2 glasses app
// needs, and whether the link is listening.
@Composable
fun GlassesScreen(activity: MainActivity) {
    val state = activity.state
    val on = state.glassesOn.value
    val code = state.glassesCode.value
    val link = GlassesLinkStatus.state.value
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
                    Text("Glasses", fontSize = 28.sp, lineHeight = 34.sp, fontWeight = FontWeight.SemiBold)
                    Text("Even Realities G2", color = Lw.text2, fontSize = 13.sp)
                }
            }
        }
        item {
            Group(null) {
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(horizontal = 14.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        Text("Even G2 glasses link")
                        LinkLine(on, link)
                    }
                    Switch(
                        checked = on,
                        onCheckedChange = { activity.setGlassesLink(it) },
                        colors = SwitchDefaults.colors(checkedTrackColor = Lw.accent, checkedThumbColor = Lw.onAccent),
                        modifier = Modifier.semantics { contentDescription = "Even G2 glasses link" },
                    )
                }
            }
        }
        if (on && link is GlassesLinkState.Failed) item { ErrorNote(link.reason) }
        if (on && code != null) {
            item {
                Group("Pairing code") {
                    Column(Modifier.fillMaxWidth().padding(14.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Text(
                            code.chunked(4).joinToString(" "),
                            style = TextStyle(fontFamily = GeistMono, fontSize = 20.sp, lineHeight = 28.sp, color = Lw.text, letterSpacing = 1.sp),
                            modifier = Modifier.semantics { contentDescription = "Pairing code ${code.toList().joinToString(" ")}" },
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            SecondaryButton("Copy", LucideIcons.Copy, Modifier.weight(1f)) { activity.copyGlassesCode() }
                            SecondaryButton("New code", LucideIcons.RotateCw, Modifier.weight(1f)) { activity.renewGlassesCode() }
                        }
                    }
                }
            }
            item {
                Text(
                    "In the Even app, open the Legato glasses app's page and paste this code. A new code stops the old one working at once.",
                    color = Lw.text2, fontSize = 14.sp, lineHeight = 20.sp, modifier = Modifier.padding(horizontal = 4.dp),
                )
            }
        } else if (!on) {
            item {
                Text(
                    "Switched on, this app lets the Legato app on your Even G2 glasses read every server saved here, with a code you paste into it once. It keeps running in the background while it is on.",
                    color = Lw.text2, fontSize = 14.sp, lineHeight = 20.sp, modifier = Modifier.padding(horizontal = 4.dp),
                )
            }
        }
    }
}

// One line on the link's state, with a dot: green while it listens.
@Composable
private fun LinkLine(on: Boolean, link: GlassesLinkState) {
    val (text, color) = when {
        !on -> "Off" to Lw.faint
        link == GlassesLinkState.Listening -> "Listening on 127.0.0.1:${GlassesServer.PORT}" to Lw.live
        link is GlassesLinkState.Failed -> "Not listening" to Lw.danger
        else -> "Starting…" to Lw.text2
    }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(color))
        Spacer(Modifier.width(6.dp))
        Text(text, color = if (on) color else Lw.text2, fontSize = 13.sp)
    }
}
