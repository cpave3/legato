package dev.legato.mobile

import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.Semaphore

// The glasses link's loopback HTTP server: one request per connection,
// answered once with Connection: close. It knows nothing of Legato or of the
// glasses: `handler` answers each request (GlassesHttp, in the service), and
// `refusal` makes the answers it gives itself to what it cannot read. It
// listens on 127.0.0.1 only. Blocking sockets on its own threads: the app
// has no coroutines.
class GlassesServer(
    private val handler: (GlassesRequest) -> GlassesResponse,
    private val refusal: (Int, String) -> GlassesResponse = { status, message ->
        GlassesResponse(status, mapOf("Content-Type" to "text/plain; charset=utf-8"), message.toByteArray())
    },
    private val port: Int = PORT,
    // How long the server waits for each read, and twice that for the whole
    // request, so a client that trickles bytes cannot hold a thread.
    private val readTimeoutMs: Int = 10_000,
    private val maxBody: Int = MAX_BODY,
) {
    companion object {
        const val PORT = 8792
        const val MAX_HEAD = 8 * 1024
        const val MAX_BODY = 8 shl 20
        // Connections served at once; more are closed unanswered.
        const val MAX_CONNECTIONS = 16
    }

    @Volatile private var socket: ServerSocket? = null
    @Volatile private var stopped = false
    private var pool: ExecutorService? = null
    private val slots = Semaphore(MAX_CONNECTIONS)
    // The connections being served, closed on stop: closing the listening
    // socket and the threads' interrupts do not end a blocked read.
    private val clients = ConcurrentHashMap.newKeySet<Socket>()

    // The address and port it listens on, once started.
    val address: InetAddress? get() = socket?.inetAddress
    val boundPort: Int get() = socket?.localPort ?: -1

    // Binds and starts accepting. Throws an IOException when it cannot bind,
    // such as when the port is taken.
    @Synchronized
    fun start() {
        check(socket == null) { "already started" }
        val listening = ServerSocket()
        try {
            listening.reuseAddress = true
            listening.bind(java.net.InetSocketAddress(InetAddress.getByName("127.0.0.1"), port), 50)
        } catch (cause: IOException) {
            listening.close()
            throw cause
        }
        val workers = Executors.newCachedThreadPool()
        socket = listening
        pool = workers
        workers.execute { accept(listening, workers) }
    }

    // Stops listening and drops every connection, so nothing still arriving
    // reaches the handler afterwards.
    @Synchronized
    fun stop() {
        stopped = true
        socket?.close()
        socket = null
        clients.forEach { runCatching { it.close() } }
        clients.clear()
        pool?.shutdownNow()
        pool = null
    }

    private fun accept(listening: ServerSocket, workers: ExecutorService) {
        while (!listening.isClosed) {
            val client = try {
                listening.accept()
            } catch (cause: IOException) {
                return
            }
            if (!slots.tryAcquire()) {
                runCatching { client.close() }
                continue
            }
            try {
                workers.execute {
                    try {
                        serve(client)
                    } finally {
                        slots.release()
                    }
                }
            } catch (cause: Exception) {
                slots.release()
                runCatching { client.close() }
            }
        }
    }

    private class Refused(val status: Int, message: String) : Exception(message)

    private fun serve(client: Socket) {
        clients += client
        try {
            if (stopped) return
            val answer = try {
                val request = read(client)
                if (stopped) return
                handler(request)
            } catch (cause: Refused) {
                refusal(cause.status, cause.message ?: "")
            } catch (cause: SocketTimeoutException) {
                refusal(408, "the request took too long")
            } catch (cause: IOException) {
                return
            } catch (cause: Exception) {
                refusal(500, "the link failed to answer")
            }
            runCatching { write(client, answer) }
        } finally {
            clients -= client
            runCatching { client.close() }
        }
    }

    private fun read(client: Socket): GlassesRequest {
        val deadline = System.nanoTime() + 2L * readTimeoutMs * 1_000_000
        val input = client.getInputStream()
        fun readSome(buffer: ByteArray, offset: Int, length: Int): Int {
            val left = (deadline - System.nanoTime()) / 1_000_000
            if (left <= 0) throw SocketTimeoutException("the request took too long")
            client.soTimeout = minOf(readTimeoutMs.toLong(), left).toInt().coerceAtLeast(1)
            return input.read(buffer, offset, length)
        }

        // The head: at most MAX_HEAD bytes, up to the blank line.
        val head = ByteArray(MAX_HEAD)
        var filled = 0
        var end = -1
        while (end < 0) {
            if (filled == head.size) throw Refused(431, "the request's head is too large")
            val n = readSome(head, filled, head.size - filled)
            if (n < 0) throw IOException("the client closed the connection")
            val from = maxOf(0, filled - 3)
            filled += n
            end = indexOfBlankLine(head, from, filled)
        }
        val lines = String(head, 0, end, Charsets.ISO_8859_1).split("\r\n")
        val parts = lines.first().split(' ')
        if (parts.size != 3 || !parts[0].matches(Regex("[A-Z]{1,10}")) || !(parts[1].startsWith("/") || parts[1] == "*") || !parts[2].startsWith("HTTP/1.")) {
            throw Refused(400, "not an HTTP request")
        }
        val headers = mutableMapOf<String, String>()
        for (line in lines.drop(1)) {
            val colon = line.indexOf(':')
            if (colon <= 0) throw Refused(400, "a header does not read")
            val name = line.substring(0, colon).trim().lowercase()
            val value = line.substring(colon + 1).trim()
            if (name == "content-length" && name in headers && headers[name] != value) throw Refused(400, "two lengths")
            headers.putIfAbsent(name, value)
        }
        if ("transfer-encoding" in headers) throw Refused(411, "send the body with a Content-Length")
        val declared = headers["content-length"]
        if (declared == null && parts[0] == "POST") throw Refused(411, "send the body with a Content-Length")
        val length = when {
            declared == null -> 0L
            declared.isEmpty() || declared.length > 12 || !declared.all(Char::isDigit) -> throw Refused(400, "the Content-Length does not read")
            else -> declared.toLong()
        }
        if (length > maxBody) throw Refused(413, "the body is too large")

        // The body: exactly Content-Length bytes, across as many reads as it
        // takes, beginning with what came after the head.
        val body = ByteArray(length.toInt())
        val already = minOf(filled - (end + 4), body.size)
        System.arraycopy(head, end + 4, body, 0, already)
        var got = already
        while (got < body.size) {
            val n = readSome(body, got, body.size - got)
            if (n < 0) throw IOException("the client closed the connection")
            got += n
        }
        return GlassesRequest(parts[0], parts[1], headers, body)
    }

    private fun indexOfBlankLine(buffer: ByteArray, from: Int, to: Int): Int {
        for (i in from..to - 4) {
            if (buffer[i] == '\r'.code.toByte() && buffer[i + 1] == '\n'.code.toByte() && buffer[i + 2] == '\r'.code.toByte() && buffer[i + 3] == '\n'.code.toByte()) return i
        }
        return -1
    }

    private fun write(client: Socket, answer: GlassesResponse) {
        val out = ByteArrayOutputStream()
        val head = StringBuilder("HTTP/1.1 ${answer.status} ${reason(answer.status)}\r\n")
        for ((name, value) in answer.headers) {
            if (name.equals("Content-Length", ignoreCase = true) || name.equals("Connection", ignoreCase = true)) continue
            head.append(name).append(": ").append(value).append("\r\n")
        }
        head.append("Content-Length: ${answer.body.size}\r\nConnection: close\r\n\r\n")
        out.write(head.toString().toByteArray(Charsets.ISO_8859_1))
        out.write(answer.body)
        client.getOutputStream().apply {
            write(out.toByteArray())
            flush()
        }
        client.shutdownOutput()
    }

    private fun reason(status: Int) = when (status) {
        200 -> "OK"
        202 -> "Accepted"
        204 -> "No Content"
        400 -> "Bad Request"
        401 -> "Unauthorized"
        404 -> "Not Found"
        408 -> "Request Timeout"
        409 -> "Conflict"
        411 -> "Length Required"
        413 -> "Content Too Large"
        415 -> "Unsupported Media Type"
        431 -> "Request Header Fields Too Large"
        500 -> "Internal Server Error"
        502 -> "Bad Gateway"
        503 -> "Service Unavailable"
        else -> "Status"
    }
}
