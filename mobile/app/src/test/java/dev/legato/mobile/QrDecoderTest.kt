package dev.legato.mobile

import com.google.zxing.BarcodeFormat
import com.google.zxing.qrcode.QRCodeWriter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class QrDecoderTest {
    private val link = "legato://pair?url=https%3A%2F%2F192.168.1.200%3A3080&token=abc123&name=erebus"

    // A camera frame's luma plane holding a QR code: the code scaled up,
    // with a margin, on a background, each row padded to rowStride.
    private fun frame(text: String, scale: Int = 4, margin: Int = 24, rowStride: Int? = null, inverted: Boolean = false): Triple<ByteArray, Int, Pair<Int, Int>> {
        val matrix = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, 0, 0)
        val size = matrix.width * scale + margin * 2
        val stride = rowStride ?: size
        val dark: Byte = if (inverted) 230.toByte() else 20
        val light: Byte = if (inverted) 20 else 230.toByte()
        val luma = ByteArray(stride * size) { light }
        for (y in 0 until size) for (x in 0 until size) {
            val mx = (x - margin) / scale
            val my = (y - margin) / scale
            val inCode = x >= margin && y >= margin && mx < matrix.width && my < matrix.height
            if (inCode && matrix.get(mx, my)) luma[y * stride + x] = dark
        }
        return Triple(luma, stride, size to size)
    }

    @Test
    fun readsAPairingCodeFromACameraFrame() {
        val (luma, stride, size) = frame(link)
        assertEquals(link, QrDecoder.decode(luma, stride, size.first, size.second))
    }

    @Test
    fun readsAFrameWhoseRowsArePadded() {
        val (luma, stride, size) = frame(link, rowStride = 512)
        assertEquals(link, QrDecoder.decode(luma, stride, size.first, size.second))
    }

    // legato pair prints its code in a terminal, light on dark.
    @Test
    fun readsACodeDrawnLightOnDark() {
        val (luma, stride, size) = frame(link, inverted = true)
        assertEquals(link, QrDecoder.decode(luma, stride, size.first, size.second))
    }

    @Test
    fun aFrameWithoutACodeGivesNothing() {
        val size = 200
        val luma = ByteArray(size * size) { i -> ((i * 31) % 256).toByte() }
        assertNull(QrDecoder.decode(luma, size, size, size))
    }
}
