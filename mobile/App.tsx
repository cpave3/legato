import { useCallback, useEffect, useRef, useState } from "react"
import { BackHandler, Linking, Pressable, RefreshControl, ScrollView, StyleSheet, Text, TextInput, View } from "react-native"
import { CameraView, useCameraPermissions } from "expo-camera"
import * as Crypto from "expo-crypto"
import * as ExpoLinking from "expo-linking"
import * as FileSystem from "expo-file-system/legacy"
import * as IntentLauncher from "expo-intent-launcher"
import * as NavigationBar from "expo-navigation-bar"
import { StatusBar } from "expo-status-bar"
import { SafeAreaProvider, SafeAreaView } from "react-native-safe-area-context"
import { WebView } from "react-native-webview"
import type { WebViewNavigation } from "react-native-webview"
import { allowNavigation, bootstrapScript, probeHealth, probeServer } from "./src/compat"
import { normalizeServerUrl, parsePairUri, type LegatoConnection } from "./src/model"
import { loadConnections, loadToken, removeConnection, saveConnection, updateConnection } from "./src/storage"

type Screen = "connections" | "add" | "scan" | "web"
type Reachability = "checking" | "online" | "offline"

export default function App() {
  const [connections, setConnections] = useState<LegatoConnection[]>([])
  const [screen, setScreen] = useState<Screen>("connections")
  const [active, setActive] = useState<LegatoConnection>()
  const [token, setToken] = useState("")
  const [error, setError] = useState("")
  const [draft, setDraft] = useState<{ name: string; url: string; token: string; caCertificate?: string }>({ name: "", url: "", token: "" })
  const [editTarget, setEditTarget] = useState<LegatoConnection>()
  const [permission, requestPermission] = useCameraPermissions()
  const webRef = useRef<WebView>(null)
  const [canGoBack, setCanGoBack] = useState(false)
  const [reachability, setReachability] = useState<Record<string, Reachability>>({})
  const [serverVersions, setServerVersions] = useState<Record<string, string>>({})
  const [refreshing, setRefreshing] = useState(false)

  const pollReachability = useCallback(async (markChecking = false) => {
    if (!connections.length) return
    if (markChecking) {
      setReachability((current) => Object.fromEntries(connections.map((connection) => [connection.id, current[connection.id] ?? "checking"])))
    }
    const results = await Promise.all(connections.map(async (connection) => [connection.id, await probeHealth(connection.baseUrl)] as const))
    setReachability(Object.fromEntries(results.map(([id, health]) => [id, health.online ? "online" : "offline"])))
    setServerVersions((current) => ({
      ...current,
      ...Object.fromEntries(results.flatMap(([id, health]) => health.version ? [[id, health.version]] : [])),
    }))
  }, [connections])

  const refreshConnections = useCallback(async () => {
    setRefreshing(true)
    try { await pollReachability(true) } finally { setRefreshing(false) }
  }, [pollReachability])

  useEffect(() => {
    loadConnections().then(setConnections)
    NavigationBar.setStyle("dark")
    NavigationBar.setButtonStyleAsync("light").catch(() => {})
  }, [])
  useEffect(() => {
    const apply = ({ url }: { url: string }) => {
      if (url.startsWith("legato://pair")) {
        try {
          const pair = parsePairUri(url)
          setDraft({ name: pair.suggestedName, url: pair.baseUrl, token: pair.token, caCertificate: pair.caCertificate })
          setScreen("add")
        } catch (cause) { setError(String(cause)) }
      }
    }
    ExpoLinking.getInitialURL().then((url) => url && apply({ url }))
    const subscription = ExpoLinking.addEventListener("url", apply)
    return () => subscription.remove()
  }, [])
  useEffect(() => {
    const subscription = BackHandler.addEventListener("hardwareBackPress", () => {
      if (screen === "web" && canGoBack) { webRef.current?.goBack(); return true }
      if (screen !== "connections") { setScreen("connections"); return true }
      return false
    })
    return () => subscription.remove()
  }, [screen, canGoBack])
  useEffect(() => {
    if (screen !== "connections" || !connections.length) return
    pollReachability(true)
    const interval = setInterval(pollReachability, 15000)
    return () => clearInterval(interval)
  }, [screen, connections, pollReachability])

  const open = async (connection: LegatoConnection) => {
    setError("")
    try {
      const secret = await loadToken(connection.id)
      if (!secret) throw new Error("This connection has no saved token. Re-pair it.")
      const meta = await probeServer(connection.baseUrl)
      if (meta.compatibility === "unsupported") throw new Error("This server requires a newer mobile app")
      setReachability((current) => ({ ...current, [connection.id]: "online" }))
      setActive({ ...connection, ...meta, lastConnectedAt: new Date().toISOString() })
      setToken(secret)
      setScreen("web")
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause)
      setError(message === "Network request failed"
        ? "Could not reach this Legato server. Check that the phone is on the same network, the server is running, and the connection URL uses this machine's current IP address."
        : message)
    }
  }
  const save = async () => {
    try {
      const name = draft.name.trim()
      if (!name) throw new Error("Give this connection a name")
      const baseUrl = normalizeServerUrl(draft.url)
      if (!draft.token.trim()) throw new Error("Token is required")
      const connection: LegatoConnection = { id: Crypto.randomUUID(), name, baseUrl, createdAt: new Date().toISOString(), caCertificate: draft.caCertificate }
      const next = await saveConnection(connection, draft.token.trim())
      setConnections(next); setDraft({ name: "", url: "", token: "" }); setScreen("connections")
      await open(next.find((item) => item.baseUrl === baseUrl)!)
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
  }
  const scan = async () => {
    const result = permission?.granted ? permission : await requestPermission()
    if (result.granted) setScreen("scan")
    else setError("Camera access was denied. Enter the URL and token manually.")
  }
  const saveCertificate = async (connection: Pick<LegatoConnection, "name" | "caCertificate">) => {
    if (!connection.caCertificate) { setError("This pairing code did not include a CA certificate. Run a current `legato pair` and re-pair the connection."); return }
    try {
      const permission = await FileSystem.StorageAccessFramework.requestDirectoryPermissionsAsync()
      if (!permission.granted) {
        setError("Choose a folder such as Downloads to save the certificate.")
        return
      }
      const filename = `legato-${connection.name.replace(/[^a-z0-9]+/gi, "-").toLowerCase()}-ca.crt`
      const uri = await FileSystem.StorageAccessFramework.createFileAsync(permission.directoryUri, filename, "application/x-x509-ca-cert")
      await FileSystem.writeAsStringAsync(uri, connection.caCertificate)
      setError(`Saved ${filename}. Tap Open certificate settings, choose Install a certificate → CA certificate, then select the file from Downloads.`)
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
  }

  return <SafeAreaProvider><SafeAreaView style={styles.safe}>
    <StatusBar style="light" />
    {screen === "connections" && <ScrollView contentContainerStyle={styles.page} refreshControl={
      <RefreshControl refreshing={refreshing} onRefresh={refreshConnections} tintColor="#818cf8" colors={["#818cf8"]} progressBackgroundColor="#18181b" />
    }>
      <View style={styles.pageHeader}>
        <Text style={styles.eyebrow}>CONNECTIONS</Text>
        <Text style={styles.title}>Legato servers</Text>
        <Text style={styles.pageDescription}>Your workspaces, wherever they are running.</Text>
      </View>
      {error ? <Text style={styles.error}>{error}</Text> : null}
      {!connections.length && <View style={styles.emptyCard}>
        <Text style={styles.emptyTitle}>No servers yet</Text>
        <Text style={styles.muted}>Pair with a Legato instance or add its address manually.</Text>
      </View>}
      {connections.map((connection) => {
        const status = reachability[connection.id] ?? "checking"
        const version = serverVersions[connection.id] ?? connection.serverVersion
        return <View key={connection.id} style={styles.card}>
        <View style={styles.cardHeader}>
          <View style={styles.serverMark}><Text style={styles.serverMarkText}>{connection.name.trim().charAt(0).toUpperCase() || "L"}</Text></View>
          <View style={styles.cardIdentity}>
            <Text style={styles.cardTitle} numberOfLines={1}>{connection.name}</Text>
            <Text style={styles.serverUrl} numberOfLines={1}>{connection.baseUrl.replace(/^https?:\/\//, "")}</Text>
          </View>
          <Pressable accessibilityRole="button" onPress={() => open(connection)} style={({ pressed }) => [styles.openButton, pressed && styles.pressed]}>
            <Text style={styles.openButtonText}>Open</Text>
            <Text style={styles.openArrow}>›</Text>
          </Pressable>
        </View>
        <View style={styles.cardFooter}>
          <View style={styles.serverMeta}>
            <View style={[styles.statusDot, status === "checking" && styles.statusChecking, status === "offline" && styles.statusOffline]} />
            <Text style={styles.metaText} numberOfLines={1}>{status === "checking" ? "Checking…" : status === "online" ? (version ? `Online · ${compactVersion(version)}` : "Online") : "Offline"}</Text>
          </View>
          <View style={styles.cardActions}>
            <CardAction label="Edit" onPress={() => { setEditTarget(connection); setDraft({ name: connection.name, url: connection.baseUrl, token: "", caCertificate: connection.caCertificate }); setError(""); setScreen("add") }} />
            {connection.caCertificate && <CardAction label="CA cert" onPress={() => saveCertificate(connection)} />}
            <CardAction label="Remove" danger onPress={async () => setConnections(await removeConnection(connection.id))} />
          </View>
        </View>
      </View>})}
      <Pressable accessibilityRole="button" onPress={() => { setError(""); setScreen("add") }} style={({ pressed }) => [styles.addButton, pressed && styles.pressed]}>
        <Text style={styles.addIcon}>＋</Text><Text style={styles.addButtonText}>Add server</Text>
      </Pressable>
      <Pressable accessibilityRole="button" onPress={() => IntentLauncher.startActivityAsync("android.settings.SECURITY_SETTINGS").catch((cause) => setError(String(cause)))} style={styles.settingsLink}>
        <Text style={styles.settingsLinkText}>Certificate settings</Text>
      </Pressable>
    </ScrollView>}
    {screen === "add" && <ScrollView contentContainerStyle={styles.page}>
      <Text style={styles.title}>{editTarget ? "Edit server" : "Add server"}</Text>
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Field label="Name" value={draft.name} onChangeText={(name) => setDraft({ ...draft, name })} placeholder="Desktop" />
      <Field label="Server URL" value={draft.url} onChangeText={(url) => setDraft({ ...draft, url })} autoCapitalize="none" placeholder="http://192.168.1.20:3080" />
      <Field label={editTarget ? "New token (optional)" : "Token"} value={draft.token} onChangeText={(value) => setDraft({ ...draft, token: value })} secureTextEntry autoCapitalize="none" placeholder={editTarget ? "Leave blank to keep current token" : undefined} />
      <Button label="Save connection" onPress={editTarget ? async () => {
        try {
          if (!draft.name.trim()) throw new Error("Give this connection a name")
          setConnections(await updateConnection(editTarget.id, { name: draft.name.trim(), baseUrl: normalizeServerUrl(draft.url) }, draft.token))
          setEditTarget(undefined); setDraft({ name: "", url: "", token: "" }); setScreen("connections")
        } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
      } : save} />
      {!editTarget && <Button label="Scan Legato QR" secondary onPress={scan} />}
      <Button label="Back" secondary onPress={() => { setEditTarget(undefined); setScreen("connections") }} />
    </ScrollView>}
    {screen === "scan" && <View style={styles.camera}>
      <CameraView style={StyleSheet.absoluteFill} barcodeScannerSettings={{ barcodeTypes: ["qr"] }} onBarcodeScanned={({ data }) => {
        try { const pair = parsePairUri(data); setDraft({ name: pair.suggestedName, url: pair.baseUrl, token: pair.token, caCertificate: pair.caCertificate }); setScreen("add") }
        catch { setError("Not a Legato pairing QR code") }
      }} />
      <View style={styles.cameraFooter}><Button label="Cancel" onPress={() => setScreen("add")} /></View>
    </View>}
    {screen === "web" && active && <View style={styles.web}>
      <View style={styles.header}>
        <CompactButton label="‹ Servers" wide onPress={() => setScreen("connections")} />
        <Text style={styles.headerTitle} numberOfLines={1}>{active.name}</Text>
        <CompactButton label="↻" accessibilityLabel="Reload server" onPress={() => webRef.current?.reload()} />
      </View>
      <WebView ref={webRef} key={active.id} source={{ uri: active.baseUrl }} injectedJavaScriptBeforeContentLoaded={bootstrapScript(active.baseUrl, token)}
        mixedContentMode="never" onNavigationStateChange={(state: WebViewNavigation) => setCanGoBack(state.canGoBack)}
        onShouldStartLoadWithRequest={({ url }) => { if (allowNavigation(active.baseUrl, url)) return true; Linking.openURL(url).catch(() => setError(`Blocked link: ${url}`)); return false }}
        onError={({ nativeEvent }) => { setError(nativeEvent.description); setScreen("connections") }} />
    </View>}
  </SafeAreaView></SafeAreaProvider>
}

function Button({ label, onPress, secondary, danger }: { label: string; onPress: () => void; secondary?: boolean; danger?: boolean }) {
  return <Pressable accessibilityRole="button" onPress={onPress} style={[styles.button, secondary && styles.secondary, danger && styles.danger]}><Text style={styles.buttonText}>{label}</Text></Pressable>
}
function CompactButton({ label, accessibilityLabel, wide, onPress }: { label: string; accessibilityLabel?: string; wide?: boolean; onPress: () => void }) {
  return <Pressable accessibilityRole="button" accessibilityLabel={accessibilityLabel} onPress={onPress} hitSlop={8} style={[styles.compactButton, wide && styles.compactButtonWide]}><Text style={styles.compactButtonText}>{label}</Text></Pressable>
}
function CardAction({ label, danger, onPress }: { label: string; danger?: boolean; onPress: () => void }) {
  return <Pressable accessibilityRole="button" onPress={onPress} hitSlop={6} style={({ pressed }) => [styles.cardAction, pressed && styles.pressed]}><Text style={[styles.cardActionText, danger && styles.cardActionDanger]}>{label}</Text></Pressable>
}
function compactVersion(version: string): string {
  return version.length > 12 ? `${version.slice(0, 11)}…` : version
}
function Field(props: React.ComponentProps<typeof TextInput> & { label: string }) {
  return <View><Text style={styles.label}>{props.label}</Text><TextInput {...props} style={styles.input} placeholderTextColor="#71717a" /></View>
}
const styles = StyleSheet.create({
  safe: { flex: 1, backgroundColor: "#09090b" }, page: { paddingHorizontal: 18, paddingTop: 24, paddingBottom: 40, gap: 14 },
  pageHeader: { marginBottom: 6, gap: 4 }, eyebrow: { color: "#818cf8", fontSize: 11, fontWeight: "800", letterSpacing: 1.8 },
  title: { color: "#fafafa", fontSize: 30, fontWeight: "700", letterSpacing: -0.6 }, pageDescription: { color: "#71717a", fontSize: 14 },
  muted: { color: "#a1a1aa" }, error: { color: "#fca5a5", backgroundColor: "#450a0a", padding: 12, borderRadius: 8 },
  emptyCard: { padding: 22, borderRadius: 16, borderWidth: 1, borderStyle: "dashed", borderColor: "#3f3f46", gap: 6, alignItems: "center" },
  emptyTitle: { color: "#e4e4e7", fontSize: 16, fontWeight: "700" },
  card: { overflow: "hidden", backgroundColor: "#18181b", borderColor: "#2c2c31", borderWidth: 1, borderRadius: 16 },
  cardHeader: { minHeight: 82, padding: 14, flexDirection: "row", alignItems: "center", gap: 12 },
  serverMark: { width: 42, height: 42, borderRadius: 13, alignItems: "center", justifyContent: "center", backgroundColor: "#312e81", borderColor: "#4f46e5", borderWidth: 1 },
  serverMarkText: { color: "#c7d2fe", fontSize: 18, fontWeight: "800" }, cardIdentity: { flex: 1, minWidth: 0, gap: 3 },
  cardTitle: { color: "#fafafa", fontSize: 17, fontWeight: "700" }, serverUrl: { color: "#71717a", fontSize: 12 },
  openButton: { height: 38, paddingLeft: 15, paddingRight: 11, borderRadius: 10, flexDirection: "row", alignItems: "center", gap: 7, backgroundColor: "#4f46e5" },
  openButtonText: { color: "#fff", fontSize: 13, fontWeight: "700" }, openArrow: { color: "#c7d2fe", fontSize: 20, lineHeight: 20 },
  cardFooter: { minHeight: 42, paddingHorizontal: 14, borderTopColor: "#27272a", borderTopWidth: StyleSheet.hairlineWidth, flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
  serverMeta: { minWidth: 0, flexDirection: "row", alignItems: "center", gap: 6 }, statusDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: "#34d399" },
  statusChecking: { backgroundColor: "#fbbf24" }, statusOffline: { backgroundColor: "#71717a" },
  metaText: { maxWidth: 105, color: "#71717a", fontSize: 10 }, cardActions: { flexDirection: "row", alignItems: "center", gap: 4 },
  cardAction: { paddingHorizontal: 7, paddingVertical: 7 }, cardActionText: { color: "#a1a1aa", fontSize: 11, fontWeight: "600" }, cardActionDanger: { color: "#f87171" },
  addButton: { height: 50, marginTop: 2, borderRadius: 14, borderWidth: 1, borderColor: "#3f3f46", borderStyle: "dashed", flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8 },
  addIcon: { color: "#818cf8", fontSize: 20 }, addButtonText: { color: "#d4d4d8", fontSize: 14, fontWeight: "700" },
  settingsLink: { alignSelf: "center", padding: 8 }, settingsLinkText: { color: "#52525b", fontSize: 12, textDecorationLine: "underline" }, pressed: { opacity: 0.72 },
  row: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  button: { minHeight: 44, paddingHorizontal: 16, justifyContent: "center", borderRadius: 8, backgroundColor: "#4f46e5" },
  secondary: { backgroundColor: "#3f3f46" }, danger: { backgroundColor: "#991b1b" }, buttonText: { color: "white", fontWeight: "600" },
  label: { color: "#d4d4d8", marginBottom: 6 }, input: { minHeight: 48, color: "#fafafa", backgroundColor: "#18181b", borderColor: "#52525b", borderWidth: 1, borderRadius: 8, paddingHorizontal: 12 },
  camera: { flex: 1 }, cameraFooter: { position: "absolute", bottom: 24, alignSelf: "center" }, web: { flex: 1 },
  header: { height: 38, paddingHorizontal: 10, flexDirection: "row", alignItems: "center", gap: 8, backgroundColor: "#18181b", borderBottomColor: "#27272a", borderBottomWidth: StyleSheet.hairlineWidth },
  headerTitle: { flex: 1, color: "#a1a1aa", fontSize: 12, textAlign: "center", fontWeight: "600" },
  compactButton: { width: 24, height: 30, justifyContent: "center", alignItems: "flex-end" },
  compactButtonWide: { width: 62, alignItems: "flex-start" },
  compactButtonText: { color: "#d4d4d8", fontSize: 12, fontWeight: "600" },
})
