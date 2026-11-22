package dev.legato.mobile

// The glasses link's settings: whether it is switched on (in plain
// preferences) and its pairing code (in the vault, encrypted like a server's
// token). The link reads the code on every request, so a new code revokes
// the old one at once.
class GlassesSettings(
    private val switch: ConnectionStore.Text,
    private val vault: TokenVault,
    private val makeCode: () -> String = ::newGlassesCode,
) {
    val enabled: Boolean get() = switch.read() == "on"

    fun code(): String? = vault.read(CODE_KEY)?.takeIf { it.isNotEmpty() }

    // Switches the link on, making a code when there is none, and returns it.
    fun switchOn(): String {
        val code = code() ?: newCode()
        switch.write("on")
        return code
    }

    // Switches the link off. The code stays, so switching on again needs no
    // new pairing.
    fun switchOff() = switch.write("off")

    // Replaces the code: the glasses paired with the old one are refused.
    fun newCode(): String = makeCode().also { vault.write(CODE_KEY, it) }

    private companion object {
        // Apart from the servers' tokens, which are kept under their IDs.
        const val CODE_KEY = "glasses-link-code"
    }
}
