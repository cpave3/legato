package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PhraseInputTest {
    @Test
    fun theFirstThreeLettersNameAWordWhateverTheCase() {
        assertEquals("banana", PhraseInput.wordFor("BAN"))
        assertEquals("banana", PhraseInput.wordFor("Banana"))
        assertEquals("banana", PhraseInput.wordFor("banxx"))
        assertNull(PhraseInput.wordFor("ba"))
        assertNull(PhraseInput.wordFor("qqq"))
    }

    @Test
    fun aPhraseIsReadFromAnySeparators() {
        val reading = PhraseInput.read("Banana-tulip,  MOS\nlim")
        assertEquals(listOf("banana", "tulip", "mosquito", "limousine"), reading.words)
        assertTrue(reading.complete)
    }

    @Test
    fun aPhraseWithAnUnknownOrMissingWordIsNotComplete() {
        val unknown = PhraseInput.read("banana qqq mosquito limousine")
        assertEquals(listOf("banana", null, "mosquito", "limousine"), unknown.words)
        assertFalse(unknown.complete)
        assertFalse(PhraseInput.read("banana tulip mosquito").complete)
        assertFalse(PhraseInput.read("banana tulip mosquito limousine acid").complete)
    }

    @Test
    fun theWordBeingTypedIsNotYetAMistake() {
        val reading = PhraseInput.read("banana tu")
        assertEquals(listOf("banana", null), reading.words)
        assertEquals(1, reading.mistakes)
        assertEquals(0, PhraseInput.read("banana tu", typing = true).mistakes)
    }

    @Test
    fun nothingTypedIsNoWordsAndNoMistakes() {
        val reading = PhraseInput.read("", typing = true)
        assertEquals(emptyList<String?>(), reading.words)
        assertEquals(0, reading.mistakes)
    }

    @Test
    fun moreThanFourWordsIsTooMany() {
        assertTrue(PhraseInput.read("banana tulip mosquito limousine acid").tooMany)
        assertFalse(PhraseInput.read("banana tulip mosquito limousine").tooMany)
    }
}
