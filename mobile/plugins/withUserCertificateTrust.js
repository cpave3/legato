const fs = require("fs")
const path = require("path")
const { withAndroidManifest, withDangerousMod } = require("expo/config-plugins")

const networkSecurityConfig = `<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
  <base-config cleartextTrafficPermitted="true">
    <trust-anchors>
      <certificates src="system" />
      <certificates src="user" />
    </trust-anchors>
  </base-config>
</network-security-config>
`

module.exports = function withUserCertificateTrust(config) {
  config = withAndroidManifest(config, (mod) => {
    const application = mod.modResults.manifest.application?.[0]
    if (!application) throw new Error("Android manifest has no application element")
    application.$["android:networkSecurityConfig"] = "@xml/network_security_config"
    return mod
  })

  return withDangerousMod(config, [
    "android",
    async (mod) => {
      const xmlDirectory = path.join(mod.modRequest.platformProjectRoot, "app", "src", "main", "res", "xml")
      fs.mkdirSync(xmlDirectory, { recursive: true })
      fs.writeFileSync(path.join(xmlDirectory, "network_security_config.xml"), networkSecurityConfig)
      return mod
    },
  ])
}
