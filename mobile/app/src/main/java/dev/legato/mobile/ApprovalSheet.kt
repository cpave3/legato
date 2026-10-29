package dev.legato.mobile

import android.os.Build
import android.view.HapticFeedbackConstants
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

// The approval sheet: a signed decision, drawn from the request the phone
// fetched itself, then signed with the key unlocked by the person's
// fingerprint or PIN. A signed decision ends on the seal.
sealed interface SheetState {
    val connection: Connection
    val approvalId: String

    data class Loading(override val connection: Connection, override val approvalId: String) : SheetState
    data class NotEnrolled(override val connection: Connection, override val approvalId: String, val busy: Boolean = false, val error: String? = null) : SheetState
    data class Ready(
        override val connection: Connection,
        override val approvalId: String,
        val model: SheetModel,
        val device: String,
        val challenge: DecisionChallenge,
        val busy: Boolean = false,
        val error: String? = null,
    ) : SheetState
    data class Signed(
        override val connection: Connection,
        override val approvalId: String,
        val model: SheetModel,
        val optionLabel: String,
        val device: String,
        val signature: ByteArray,
        val messageHash: ByteArray,
        val strongBox: Boolean,
    ) : SheetState
    data class Denied(override val connection: Connection, override val approvalId: String, val title: String) : SheetState
    data class Failed(override val connection: Connection, override val approvalId: String, val message: String) : SheetState
}

private val mono = TextStyle(fontFamily = GeistMono, fontSize = 12.sp, color = Lw.text2)
private val signedGreen = Color(0xFF3ECF8E)

@Composable
fun ApprovalSheet(activity: MainActivity, state: SheetState) {
    // A scrim over whatever is on screen; a tap outside closes all but a
    // decision in progress.
    Box(
        Modifier.fillMaxSize().background(Color(0xE6070709))
            .clickable(remember { MutableInteractionSource() }, null) { if (!busy(state)) activity.closeSheet() },
        contentAlignment = Alignment.BottomCenter,
    ) {
        Column(
            Modifier.fillMaxWidth().heightIn(max = 760.dp)
                .clip(RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp))
                .background(Brush.verticalGradient(listOf(Color(0xFF15141C), Lw.ground)))
                .border(BorderStroke(1.dp, Brush.verticalGradient(listOf(Lw.accent.copy(alpha = 0.55f), Lw.line))), RoundedCornerShape(topStart = 22.dp, topEnd = 22.dp))
                .clickable(remember { MutableInteractionSource() }, null) {}
                .padding(horizontal = 20.dp, vertical = 18.dp)
                .verticalScroll(rememberScrollState()),
        ) {
            SheetHeader(state)
            Spacer(Modifier.height(14.dp))
            when (state) {
                is SheetState.Loading -> Box(Modifier.fillMaxWidth().height(180.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(color = Lw.accent, strokeWidth = 2.dp)
                }
                is SheetState.NotEnrolled -> NotEnrolled(activity, state)
                is SheetState.Ready -> Request(activity, state)
                is SheetState.Signed -> SignedSeal(activity, state)
                is SheetState.Denied -> Ended("Denied", "The agent is told you said no.", Lw.text2) { activity.closeSheet() }
                is SheetState.Failed -> Ended("Not signed", state.message, Lw.error) { activity.closeSheet() }
            }
        }
    }
}

private fun busy(state: SheetState) = (state is SheetState.Ready && state.busy) || (state is SheetState.NotEnrolled && state.busy)

@Composable
private fun SheetHeader(state: SheetState) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text("◈", color = Lw.accent, fontSize = 14.sp)
        Spacer(Modifier.width(8.dp))
        Text("SIGNED APPROVAL", style = mono.copy(color = Lw.link, letterSpacing = 2.sp, fontSize = 11.sp))
        Spacer(Modifier.weight(1f))
        Text(state.connection.name, style = mono.copy(fontSize = 11.sp), maxLines = 1)
    }
}

@Composable
private fun NotEnrolled(activity: MainActivity, state: SheetState.NotEnrolled) {
    Text("This phone does not sign approvals for ${state.connection.name} yet.", fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
    Spacer(Modifier.height(8.dp))
    Text("Enrolling makes a key in this phone's secure hardware. It signs only after your fingerprint or PIN, and never leaves the phone.", color = Lw.text2, fontSize = 14.sp)
    state.error?.let { Spacer(Modifier.height(10.dp)); ErrorLine(it) }
    Spacer(Modifier.height(18.dp))
    BigButton(if (state.busy) "Enrolling…" else "Sign approvals with this phone", primary = true, enabled = !state.busy) { activity.enrollThenOpen(state.connection, state.approvalId) }
    Spacer(Modifier.height(8.dp))
    BigButton("Not now", primary = false, enabled = !state.busy) { activity.closeSheet() }
}

@Composable
private fun Request(activity: MainActivity, state: SheetState.Ready) {
    val m = state.model
    Text(m.title, fontSize = 22.sp, fontWeight = FontWeight.SemiBold, lineHeight = 28.sp)
    val asker = listOfNotNull(m.agentKind, m.taskTitle).joinToString(" · ")
    if (asker.isNotEmpty()) {
        Spacer(Modifier.height(4.dp))
        Text(asker, color = Lw.text2, fontSize = 14.sp)
    }
    Spacer(Modifier.height(16.dp))
    // What is signed, in Legato's words.
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(Color(0xFF0D0D12)).border(1.dp, Lw.line, RoundedCornerShape(12.dp)).padding(14.dp)) {
        m.credential?.let { ScopeLine("credential", it) }
        if (m.operations.isNotEmpty()) ScopeLine("operation", m.operations.joinToString(", "), Lw.link)
        m.resources.forEach { ScopeLine("resource", it, Lw.text) }
        if (m.credential == null && m.summary.isNotEmpty()) ScopeLine("request", m.summary, Lw.text)
        ScopeLine("server", m.server)
    }
    m.reason?.let { reason ->
        Spacer(Modifier.height(14.dp))
        Text("AGENT'S REASON", style = mono.copy(fontSize = 10.sp, letterSpacing = 1.5.sp, color = Lw.faint))
        Spacer(Modifier.height(4.dp))
        Row(Modifier.height(IntrinsicSize.Min)) {
            Box(Modifier.width(2.dp).fillMaxHeight().background(Lw.lineStrong))
            Spacer(Modifier.width(10.dp))
            Text("“$reason”", color = Lw.text2, fontSize = 14.sp, fontStyle = FontStyle.Italic)
        }
    }
    state.error?.let { Spacer(Modifier.height(12.dp)); ErrorLine(it) }
    Spacer(Modifier.height(20.dp))
    m.allows.forEachIndexed { index, option ->
        BigButton(option.label, primary = index == 0, enabled = !state.busy, glyph = if (index == 0) "◉" else null) {
            activity.signDecision(state, option)
        }
        Spacer(Modifier.height(8.dp))
    }
    BigButton("Deny", primary = false, enabled = !state.busy, danger = true) { activity.denyFromSheet(state) }
    Spacer(Modifier.height(12.dp))
    Text(
        if (state.busy) "Waiting for your fingerprint or PIN…" else "Signs with your fingerprint or PIN · key held in this phone's secure hardware",
        style = mono.copy(fontSize = 10.5.sp, color = Lw.faint), textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun ScopeLine(label: String, value: String, color: Color = Lw.text2) {
    Row(Modifier.padding(vertical = 3.dp)) {
        Text(label.padEnd(10), style = mono.copy(color = Lw.faint, fontSize = 11.sp), modifier = Modifier.width(78.dp))
        Text(value, style = mono.copy(color = color, fontSize = 12.5.sp))
    }
}

@Composable
private fun ErrorLine(text: String) {
    Text(text, color = Lw.danger, fontSize = 13.sp, modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp)).background(Lw.errorSoft).padding(10.dp))
}

@Composable
private fun BigButton(label: String, primary: Boolean, enabled: Boolean = true, glyph: String? = null, danger: Boolean = false, onClick: () -> Unit) {
    val shape = RoundedCornerShape(14.dp)
    val base = Modifier.fillMaxWidth().height(54.dp).clip(shape)
    val styled = when {
        primary -> base.background(Brush.horizontalGradient(listOf(Lw.accent, Color(0xFF8A6CFF))))
        danger -> base.border(1.dp, Lw.dangerLine, shape)
        else -> base.border(1.dp, Lw.lineStrong, shape)
    }
    Box(styled.alpha(if (enabled) 1f else 0.5f).clickable(enabled = enabled, onClick = onClick), contentAlignment = Alignment.Center) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            glyph?.let { Text(it, color = Color.White.copy(alpha = 0.9f), fontSize = 15.sp); Spacer(Modifier.width(10.dp)) }
            Text(label, color = if (danger) Lw.danger else if (primary) Color.White else Lw.text, fontSize = 16.sp, fontWeight = if (primary) FontWeight.SemiBold else FontWeight.Medium)
        }
    }
}

@Composable
private fun Ended(title: String, message: String, color: Color, close: () -> Unit) {
    Text(title, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, color = color)
    Spacer(Modifier.height(8.dp))
    Text(message, color = Lw.text2, fontSize = 14.sp)
    Spacer(Modifier.height(18.dp))
    BigButton("Close", primary = false, onClick = close)
}

private fun hex(bytes: ByteArray, count: Int) = bytes.take(count).joinToString("") { "%02x".format(it) }

// The reward for signing: a seal draws itself, a dial of ticks lights up
// around it, the arc sweeps from violet to green, a check lands, and the
// readout types out what was signed and by which key.
@Composable
private fun SignedSeal(activity: MainActivity, state: SheetState.Signed) {
    val view = LocalView.current
    val sweep = remember { Animatable(0f) }
    val check = remember { Animatable(0f) }
    var typed by remember { mutableIntStateOf(0) }
    val glow = rememberInfiniteTransition(label = "glow")
    val pulse by glow.animateFloat(0.35f, 0.75f, infiniteRepeatable(tween(1400, easing = LinearEasing), RepeatMode.Reverse), label = "pulse")
    val readout = listOf(
        "option  ${state.optionLabel}",
        "key     ${state.device} · ${if (state.strongBox) "StrongBox" else "secure hardware"}",
        "sha256  ${hex(state.messageHash, 12)}…",
        "sig     ${hex(state.signature, 12)}…",
        "server  ${state.model.server}",
    )
    LaunchedEffect(state.approvalId) {
        sweep.animateTo(1f, tween(900, easing = FastOutSlowInEasing))
        view.performHapticFeedback(if (Build.VERSION.SDK_INT >= 30) HapticFeedbackConstants.CONFIRM else HapticFeedbackConstants.LONG_PRESS)
        check.animateTo(1f, spring(dampingRatio = Spring.DampingRatioMediumBouncy, stiffness = Spring.StiffnessMediumLow))
        val total = readout.sumOf { it.length }
        while (typed < total) {
            typed += 3
            delay(12)
        }
    }
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
        Box(Modifier.size(196.dp), contentAlignment = Alignment.Center) {
            Canvas(Modifier.fillMaxSize()) {
                val c = center
                val r = size.minDimension / 2
                drawCircle(Brush.radialGradient(listOf(Lw.accent.copy(alpha = 0.32f * pulse), Color.Transparent), c, r), r, c)
                // The dial: 60 ticks, lit as the sweep passes them.
                for (i in 0 until 60) {
                    val lit = i / 60f <= sweep.value
                    val a = (i / 60f) * 2 * PI.toFloat() - PI.toFloat() / 2
                    val long = i % 5 == 0
                    val inner = r * (if (long) 0.80f else 0.84f)
                    val outer = r * 0.90f
                    drawLine(
                        if (lit) lerpColor(Lw.accent, signedGreen, i / 60f) else Lw.line,
                        Offset(c.x + inner * cos(a), c.y + inner * sin(a)), Offset(c.x + outer * cos(a), c.y + outer * sin(a)),
                        strokeWidth = if (long) 3f else 2f, cap = StrokeCap.Round,
                    )
                }
                // The sweep.
                rotate(-90f, c) {
                    drawArc(
                        Brush.sweepGradient(listOf(Lw.accent, Color(0xFF8A6CFF), signedGreen, Lw.accent), c),
                        0f, 360f * sweep.value, false,
                        topLeft = Offset(c.x - r * 0.72f, c.y - r * 0.72f), size = androidx.compose.ui.geometry.Size(r * 1.44f, r * 1.44f),
                        style = Stroke(width = 6f, cap = StrokeCap.Round),
                    )
                }
                // The seal: a hexagon that fills once the sweep is done.
                val hexagon = Path().apply {
                    for (k in 0..5) {
                        val a = k * PI.toFloat() / 3 - PI.toFloat() / 2
                        val p = Offset(c.x + r * 0.5f * cos(a), c.y + r * 0.5f * sin(a))
                        if (k == 0) moveTo(p.x, p.y) else lineTo(p.x, p.y)
                    }
                    close()
                }
                drawPath(hexagon, signedGreen.copy(alpha = 0.16f * check.value))
                drawPath(hexagon, lerpColor(Lw.lineStrong, signedGreen, check.value), style = Stroke(width = 3f))
            }
            Text("✓", color = signedGreen, fontSize = 44.sp, fontWeight = FontWeight.Bold, modifier = Modifier.scale(check.value).alpha(check.value))
        }
        Spacer(Modifier.height(6.dp))
        Text("SIGNED", style = mono.copy(color = signedGreen, fontSize = 15.sp, letterSpacing = 6.sp))
        Spacer(Modifier.height(4.dp))
        Text(state.model.title, color = Lw.text, fontSize = 16.sp, fontWeight = FontWeight.Medium, textAlign = TextAlign.Center)
        Spacer(Modifier.height(14.dp))
        // The readout, typed out.
        Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).background(Color(0xFF0B0B10)).border(1.dp, Lw.line, RoundedCornerShape(10.dp)).padding(12.dp)) {
            var left = typed
            readout.forEach { line ->
                val shown = line.take(left.coerceAtLeast(0))
                left -= line.length
                Text(shown.ifEmpty { " " }, style = mono.copy(fontSize = 11.5.sp, color = if (shown.startsWith("option")) signedGreen else Lw.text2))
            }
        }
        Spacer(Modifier.height(16.dp))
        BigButton("Done", primary = false, onClick = { activity.closeSheet() })
    }
}

private fun lerpColor(a: Color, b: Color, t: Float) = Color(
    a.red + (b.red - a.red) * t, a.green + (b.green - a.green) * t, a.blue + (b.blue - a.blue) * t, a.alpha + (b.alpha - a.alpha) * t,
)
