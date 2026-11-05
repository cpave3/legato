package dev.legato.mobile

import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

// QrScanner shows the back camera and reads the first QR code it sees with
// QrDecoder: the app's own camera, so Google Play services is not needed. It
// is drawn over the screen it was opened from.
@Composable
fun QrScanner(onCode: (String) -> Unit, onCancel: () -> Unit, onError: (String) -> Unit) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val previewView = androidx.compose.runtime.remember { PreviewView(context).apply { scaleType = PreviewView.ScaleType.FILL_CENTER } }

    DisposableEffect(lifecycleOwner) {
        val analysis = Executors.newSingleThreadExecutor()
        val found = AtomicBoolean(false)
        val providerFuture = ProcessCameraProvider.getInstance(context)
        var provider: ProcessCameraProvider? = null
        providerFuture.addListener({
            try {
                val cameras = providerFuture.get()
                provider = cameras
                val preview = Preview.Builder().build().also { it.surfaceProvider = previewView.surfaceProvider }
                val analyzer = ImageAnalysis.Builder()
                    .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                    .build()
                analyzer.setAnalyzer(analysis) { image ->
                    try {
                        if (found.get()) return@setAnalyzer
                        val plane = image.planes[0]
                        val buffer = plane.buffer
                        val luma = ByteArray(buffer.remaining()).also { buffer.get(it) }
                        val text = QrDecoder.decode(luma, plane.rowStride, image.width, image.height)
                        if (text != null && found.compareAndSet(false, true)) {
                            ContextCompat.getMainExecutor(context).execute { onCode(text) }
                        }
                    } finally {
                        image.close()
                    }
                }
                cameras.unbindAll()
                cameras.bindToLifecycle(lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analyzer)
            } catch (cause: Exception) {
                onError("The camera could not start (${cause.message ?: cause}). Pair by phrase, or enter the URL and token.")
            }
        }, ContextCompat.getMainExecutor(context))
        onDispose {
            provider?.unbindAll()
            analysis.shutdown()
        }
    }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        AndroidView(factory = { previewView }, modifier = Modifier.fillMaxSize())
        Box(Modifier.align(Alignment.Center).size(240.dp).border(2.dp, Lw.accent, RoundedCornerShape(16.dp)))
        Column(
            Modifier.align(Alignment.BottomCenter).padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text("Point the camera at the code legato pair shows", color = Color.White, fontSize = 15.sp)
            Box(
                Modifier.clip(RoundedCornerShape(12.dp)).background(Lw.surface).border(1.dp, Lw.line, RoundedCornerShape(12.dp))
                    .clickable { onCancel() }.padding(horizontal = 24.dp, vertical = 12.dp),
            ) { Text("Cancel", color = Lw.text, fontSize = 15.sp) }
        }
    }
}
