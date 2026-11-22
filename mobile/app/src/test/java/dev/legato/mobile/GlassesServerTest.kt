package dev.legato.mobile

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.IOException
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.concurrent.CopyOnWriteArrayList

class GlassesServerTest {
    private val seen = CopyOnWriteArrayList<GlassesRequest>()
    private val servers = mutableListOf<GlassesServer>()

    private fun server(readTimeoutMs: Int = 2_000, maxBody: Int = 8 shl 20): GlassesServer =
        GlassesServer(
            handler = { request ->
                seen += request
                GlassesResponse(200, mapOf("Content-Type" to "text/plain", "Connection" to "close"), "got ${request.body.size}".toByteArray())
            },
            refusal = { status, message -> GlassesResponse(status, mapOf("X-Refused" to "yes"), message.toByteArray()) },
            port = 0,
            readTimeoutMs = readTimeoutMs,
            maxBody = maxBody,
        ).also { it.start(); servers += it }

    @After
    fun stop() = servers.forEach { it.stop() }

    // Writes the parts with a pause between them and reads the whole answer.
    private fun exchange(server: GlassesServer, vararg parts: ByteArray, pauseMs: Long = 50): String =
        Socket(InetAddress.getLoopbackAddress(), server.boundPort).use { socket ->
            socket.soTimeout = 5_000
            val out = socket.getOutputStream()
            try {
                for (part in parts) {
                    out.write(part)
                    out.flush()
                    Thread.sleep(pauseMs)
                }
            } catch (cause: IOException) {
                // The server may answer and close before every part is sent.
            }
            socket.getInputStream().readBytes().toString(Charsets.ISO_8859_1)
        }

    private fun status(answer: String) = answer.substringBefore("\r\n").split(" ").getOrNull(1)?.toInt()

    @Test
    fun `reads a body sent in several writes, and answers once, closing the connection`() {
        val server = server()
        val body = "{\"content\":\"" + "x".repeat(100_000) + "\"}"
        val bytes = body.toByteArray()
        val head = "POST /v1/x HTTP/1.1\r\nHost: 127.0.0.1\r\nAuthorization: Bearer C\r\nContent-Type: application/json\r\nContent-Length: ${bytes.size}\r\n\r\n"

        val answer = exchange(server, head.toByteArray().copyOfRange(0, 20), head.toByteArray().copyOfRange(20, head.length), bytes.copyOfRange(0, 7), bytes.copyOfRange(7, bytes.size))

        assertEquals(200, status(answer))
        assertTrue(answer, answer.contains("\r\nConnection: close\r\n"))
        assertTrue(answer, answer.contains("\r\nContent-Length: ${"got ${bytes.size}".length}\r\n"))
        assertTrue(answer, answer.endsWith("\r\n\r\ngot ${bytes.size}"))
        val request = seen.single()
        assertEquals("POST", request.method)
        assertEquals("/v1/x", request.target)
        assertEquals("Bearer C", request.headers["authorization"])
        assertEquals("application/json", request.headers["content-type"])
        assertEquals(body, request.body.toString(Charsets.UTF_8))
    }

    @Test
    fun `reads a request with no body`() {
        val answer = exchange(server(), "GET /v1/instances HTTP/1.1\r\nHost: x\r\n\r\n".toByteArray())

        assertEquals(200, status(answer))
        assertEquals(0, seen.single().body.size)
    }

    @Test
    fun `refuses a POST without a Content-Length, or with one it can't read`() {
        val server = server()

        val missing = exchange(server, "POST /v1/x HTTP/1.1\r\nHost: x\r\n\r\n{}".toByteArray())
        val chunked = exchange(server, "POST /v1/x HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n2\r\n{}\r\n0\r\n\r\n".toByteArray())
        val garbled = exchange(server, "POST /v1/x HTTP/1.1\r\nContent-Length: two\r\n\r\n{}".toByteArray())

        assertEquals(411, status(missing))
        assertTrue(missing.contains("X-Refused: yes"))
        assertEquals(411, status(chunked))
        assertEquals(400, status(garbled))
        assertEquals(emptyList<GlassesRequest>(), seen)
    }

    @Test
    fun `refuses a body over the limit without reading it`() {
        val server = server(maxBody = 1_000)

        val answer = exchange(server, "POST /v1/x HTTP/1.1\r\nContent-Length: 1001\r\n\r\n".toByteArray(), ByteArray(1001))

        assertEquals(413, status(answer))
        assertEquals(emptyList<GlassesRequest>(), seen)
    }

    @Test
    fun `refuses a head over 8 KiB`() {
        val answer = exchange(server(), ("GET /v1/instances HTTP/1.1\r\nX-Long: " + "a".repeat(9_000) + "\r\n\r\n").toByteArray())

        assertEquals(431, status(answer))
        assertEquals(emptyList<GlassesRequest>(), seen)
    }

    @Test
    fun `refuses what is not an HTTP request`() {
        assertEquals(400, status(exchange(server(), "hello\r\n\r\n".toByteArray())))
    }

    @Test
    fun `gives up on a client that stops sending`() {
        val server = server(readTimeoutMs = 200)
        val started = System.nanoTime()

        val answer = exchange(server, "POST /v1/x HTTP/1.1\r\nContent-Length: 10\r\n\r\n{\"a\"".toByteArray())

        assertTrue(answer, answer.isEmpty() || status(answer) == 408)
        assertTrue((System.nanoTime() - started) / 1_000_000 < 3_000)
        assertEquals(emptyList<GlassesRequest>(), seen)
    }

    @Test
    fun `gives up on a client that sends too slowly, however it trickles`() {
        val server = server(readTimeoutMs = 300)
        val started = System.nanoTime()
        val head = "GET /v1/instances HTTP/1.1\r\nHost: x\r\n\r\n".toByteArray()

        val answer = exchange(server, *head.map { byteArrayOf(it) }.toTypedArray(), pauseMs = 100)

        assertTrue(answer, answer.isEmpty() || status(answer) == 408)
        assertTrue((System.nanoTime() - started) / 1_000_000 < head.size * 100 + 1_000)
        assertEquals(emptyList<GlassesRequest>(), seen)
    }

    @Test
    fun `listens on loopback only`() {
        val server = server()

        assertTrue(server.address!!.isLoopbackAddress)
    }

    @Test
    fun `fails to start when its port is taken, saying so`() {
        ServerSocket(0, 1, InetAddress.getByName("127.0.0.1")).use { taken ->
            val server = GlassesServer(handler = { GlassesResponse(200, emptyMap(), ByteArray(0)) }, port = taken.localPort)
            val failure = runCatching { server.start() }.exceptionOrNull()
            server.stop()

            assertTrue(failure is IOException)
        }
    }
}
