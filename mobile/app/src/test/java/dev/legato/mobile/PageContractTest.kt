package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PageContractTest {
    @Test
    fun `signs the page in with the token, escaped`() {
        val script = bootstrapScript("https://mac.example", "a\"b", shellKey = "k1")
        assertTrue(script.contains("legato:token:local"))
        assertTrue(script.contains("legato:token:https://mac.example"))
        assertTrue(script.contains("\"a\\\"b\""))
    }

    @Test
    fun `tells only the top frame it can record, with the page's key`() {
        val script = bootstrapScript("https://mac.example", "t", shellKey = "k1")
        assertTrue(script.contains("window.legatoShell={voice:1,approval:1,enroll:1,key:\"k1\"}"))
        assertTrue(script.startsWith("if(window===window.top)"))
    }

    @Test
    fun `keeps the page on its own server`() {
        assertTrue(allowNavigation("https://mac.example", "https://mac.example/board"))
        assertFalse(allowNavigation("https://mac.example", "https://evil.example"))
        assertFalse(allowNavigation("https://mac.example", "http://mac.example/board"))
        assertFalse(allowNavigation("https://mac.example", "not a url"))
    }

    @Test
    fun `reads what the page posts, and nothing it does not understand`() {
        assertEquals(PageMessage.Restore("http://d:3080/agents?task=a"), PageMessage.parse("""{"legatoShell":"restore","url":"http://d:3080/agents?task=a"}"""))
        assertEquals(PageMessage.Voice(key = "k", action = "start"), PageMessage.parse("""{"legatoShell":"voice","key":"k","action":"start"}"""))
        assertEquals(PageMessage.Unknown, PageMessage.parse("""{"legatoShell":"restore","url":5}"""))
        assertEquals(PageMessage.Unknown, PageMessage.parse("[1,2]"))
        assertEquals(PageMessage.Unknown, PageMessage.parse("not json"))
    }

    @Test
    fun `reads whether the page still has something on screen`() {
        assertEquals(true, readPageAnswer("true"))
        assertEquals(false, readPageAnswer("false"))
        // A renderer that is gone answers nothing, or null.
        assertEquals(null, readPageAnswer("null"))
        assertEquals(null, readPageAnswer(null))
    }

    @Test
    fun `reads an approval request, and only its ID`() {
        assertEquals(PageMessage.Approval("k1", "ap-12345678"), PageMessage.parse("""{"legatoShell":"approval","key":"k1","id":"ap-12345678","title":"forged","canonical":"x"}"""))
        assertEquals(PageMessage.Unknown, PageMessage.parse("""{"legatoShell":"approval","key":"k1"}"""))
        assertEquals(PageMessage.Unknown, PageMessage.parse("""{"legatoShell":"approval","key":"k1","id":"../../api/x"}"""))
        assertEquals(PageMessage.Enroll("k1"), PageMessage.parse("""{"legatoShell":"enroll","key":"k1"}"""))
    }

    @Test
    fun `tells the page how an approval and an enrollment went`() {
        val approval = approvalEventScript("ap-1", "decided", "once", null)
        assertTrue(approval.contains("legato:shell-approval"))
        assertTrue(approval.contains("\"option\":\"once\""))
        val enroll = enrollEventScript("error", "it's \"broken\"")
        assertTrue(enroll.contains("legato:shell-enroll"))
        assertTrue(enroll.contains("it's \\\"broken\\\""))
    }
}
