package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

// The page a WebView would be showing, with a clock and timers of its own, so
// a test describes the page's condition, not the keeper's calls.
private class FakePage : PageKeeper.Host {
    val opened = mutableListOf<String>()
    val events = mutableListOf<String>()
    val unavailable = mutableListOf<String>()

    // false: the renderer is gone, so nothing run in the page answers.
    var alive = true

    // false: the page is running but has nothing on screen.
    var rendered = true

    var now = 1_000_000L
    private val timers = mutableListOf<Pair<Long, () -> Unit>>()

    override fun open(url: String) {
        opened += url
        alive = true
    }

    override fun ask(answer: (Boolean?) -> Unit) {
        if (!alive) return
        events += "legato:shell-return"
        answer(rendered)
    }

    override fun after(delayMs: Long, action: () -> Unit): () -> Unit {
        val timer = now + delayMs to action
        timers += timer
        return { timers.remove(timer) }
    }

    override fun unavailable(reason: String) {
        unavailable += reason
    }

    fun timePasses(ms: Long = 60_000) {
        now += ms
        val due = timers.filter { it.first <= now }
        timers.removeAll(due)
        due.forEach { it.second() }
    }
}

class PageKeeperTest {
    private val server = "http://desk.local:3080"
    private lateinit var page: FakePage
    private lateinit var keeper: PageKeeper

    @Before
    fun opensTheServer() {
        page = FakePage()
        keeper = PageKeeper(server, page) { page.now }
        keeper.start()
    }

    private fun personReturns() {
        keeper.backgrounded()
        keeper.foregrounded()
    }

    @Test
    fun `opens the server`() {
        assertEquals(listOf(server), page.opened)
    }

    @Test
    fun `reopens the page the person was on when the system reclaims it`() {
        keeper.rendererLost("$server/board")

        assertEquals(listOf(server, "$server/board"), page.opened)
    }

    @Test
    fun `reopens the server itself when the lost page was not one of its own`() {
        keeper.rendererLost("https://elsewhere.example/board")

        assertEquals(listOf(server, server), page.opened)
    }

    @Test
    fun `waits for the person to return before reopening a page lost in the background`() {
        keeper.backgrounded()
        keeper.rendererLost("$server/board")
        assertEquals(listOf(server), page.opened)

        keeper.foregrounded()
        assertEquals(listOf(server, "$server/board"), page.opened)
    }

    @Test
    fun `reopens a page that no longer answers when the person returns`() {
        keeper.navigated("$server/review")
        page.alive = false

        personReturns()
        page.timePasses()

        assertEquals(listOf(server, "$server/review"), page.opened)
    }

    @Test
    fun `leaves a page that answers alone when the person returns`() {
        personReturns()
        page.timePasses()

        assertEquals(listOf(server), page.opened)
    }

    @Test
    fun `reopens where the person last went when the lost page is not named`() {
        keeper.navigated("$server/plans")

        keeper.rendererLost(null)

        assertEquals(listOf(server, "$server/plans"), page.opened)
    }

    @Test
    fun `reopens a page that came back with nothing on screen`() {
        keeper.navigated("$server/board")
        page.rendered = false

        personReturns()

        assertEquals(listOf(server, "$server/board"), page.opened)
    }

    @Test
    fun `reopens the view the page asked to come back to`() {
        keeper.navigated("$server/agents")
        keeper.pageAsksToReturnTo("$server/agents?task=abc123")

        keeper.rendererLost("$server/agents")

        assertEquals(listOf(server, "$server/agents?task=abc123"), page.opened)
    }

    @Test
    fun `forgets what the page asked for once the person has gone elsewhere`() {
        keeper.navigated("$server/agents")
        keeper.pageAsksToReturnTo("$server/agents?task=abc123")
        keeper.navigated("$server/board")

        keeper.rendererLost("$server/board")

        assertEquals(listOf(server, "$server/board"), page.opened)
    }

    @Test
    fun `gives up on a page that is lost again as soon as it is reopened`() {
        keeper.rendererLost("$server/board")
        keeper.rendererLost("$server/board")

        assertEquals(listOf(server, "$server/board"), page.opened)
        assertEquals(1, page.unavailable.size)
    }

    @Test
    fun `reopens a page lost again after it had been running for a while`() {
        keeper.rendererLost("$server/board")
        page.timePasses(5 * 60 * 1000)
        keeper.rendererLost("$server/board")

        assertEquals(listOf(server, "$server/board", "$server/board"), page.opened)
        assertEquals(emptyList<String>(), page.unavailable)
    }

    @Test
    fun `reopens once when the loss is reported while the page is being asked`() {
        page.alive = false

        personReturns()
        keeper.rendererLost("$server/board")
        page.timePasses()

        assertEquals(listOf(server, "$server/board"), page.opened)
    }

    @Test
    fun `reopens a page lost again while the person was away, however soon`() {
        keeper.rendererLost("$server/board")

        personReturns()
        keeper.rendererLost("$server/board")

        assertEquals(listOf(server, "$server/board", "$server/board"), page.opened)
        assertEquals(emptyList<String>(), page.unavailable)
    }

    @Test
    fun `says why a page that failed to load is unavailable`() {
        keeper.loadFailed("net::ERR_CONNECTION_REFUSED")

        assertEquals(listOf("net::ERR_CONNECTION_REFUSED"), page.unavailable)
        assertEquals(listOf(server), page.opened)
    }

    @Test
    fun `does not judge a page while the app is in the background`() {
        page.alive = false

        personReturns()
        keeper.backgrounded()
        page.timePasses()
        assertEquals(listOf(server), page.opened)

        keeper.foregrounded()
        page.timePasses()
        assertEquals(listOf(server, server), page.opened)
    }

    @Test
    fun `tells a page that is still there that the person has returned`() {
        assertEquals(emptyList<String>(), page.events)

        personReturns()

        assertEquals(listOf("legato:shell-return"), page.events)
    }

    @Test
    fun `says nothing to a page it is about to reopen`() {
        keeper.backgrounded()
        keeper.rendererLost("$server/agents")
        keeper.foregrounded()

        assertEquals(listOf(server, "$server/agents"), page.opened)
        assertEquals(emptyList<String>(), page.events)
    }

    @Test
    fun `does not ask a page that was just opened, when the app first comes to the front`() {
        keeper.foregrounded()

        assertEquals(emptyList<String>(), page.events)
    }
}
