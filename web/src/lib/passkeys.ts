import { apiFetch } from "./api"
import type { Device } from "./devices"

// Desktop passkeys sign approval decisions in the browser, as the phone
// does in its app. A passkey belongs to the page it is made on: this page's
// host name, whichever Legato server it talks to. The WebAuthn challenge of
// a decision is the SHA-256 of the same message a phone signs, so the
// passkey's signature binds the request, the option and the challenge. The
// server side is internal/service/approval_passkeys.go.

export function toBase64Url(bytes: ArrayBuffer | Uint8Array): string {
  const view = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes)
  let text = ""
  for (const b of view) text += String.fromCharCode(b)
  return btoa(text).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

export function fromBase64Url(text: string): ArrayBuffer {
  const padded = text.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((text.length + 3) % 4)
  const raw = atob(padded)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out.buffer
}

/** The message a decision signs, as the phone and the server build it. */
export function signedMessage(challenge: string, device: string, option: string, canonical: string): string {
  return `{"challenge":"${challenge}","device":"${device}","option":"${option}","request":${canonical},"v":1}`
}

/** Whether this page can make and use a passkey: a secure page at a host name. */
export function passkeysSupported(page = { secure: window.isSecureContext, hostname: window.location.hostname }): boolean {
  if (typeof PublicKeyCredential === "undefined" || !page.secure) return false
  const ip = /^\d{1,3}(\.\d{1,3}){3}$/.test(page.hostname) || page.hostname.includes(":")
  return !ip
}

/** The active passkey that was made on this page, if any. */
export function passkeyForThisPage(devices: Device[], origin = window.location.origin): Device | undefined {
  return devices.find((d) => d.kind === "passkey" && d.status === "active" && d.origin === origin)
}

async function errorOf(resp: Response): Promise<Error> {
  const body = await resp.json().catch(() => ({}))
  return new Error((body as { error?: string }).error || `HTTP ${resp.status}`)
}

type CreationOptions = {
  publicKey: Omit<PublicKeyCredentialCreationOptions, "challenge" | "user" | "excludeCredentials"> & {
    challenge: string
    user: { id: string; name: string; displayName: string }
    excludeCredentials?: { type: "public-key"; id: string; transports?: AuthenticatorTransport[] }[]
  }
}

/** Makes a passkey on this page and enrolls it on the server at baseUrl. */
export async function enrollPasskey(baseUrl: string, name: string): Promise<Device> {
  const origin = window.location.origin
  const resp = await apiFetch(baseUrl, "/api/devices/passkey/options", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ origin }),
  })
  if (!resp.ok) throw await errorOf(resp)
  const { publicKey } = (await resp.json()) as CreationOptions
  const credential = await navigator.credentials.create({
    publicKey: {
      ...publicKey,
      challenge: fromBase64Url(publicKey.challenge),
      user: { ...publicKey.user, id: fromBase64Url(publicKey.user.id) },
      excludeCredentials: (publicKey.excludeCredentials ?? []).map((c) => ({ ...c, id: fromBase64Url(c.id) })),
    },
  }) as PublicKeyCredential | null
  if (!credential) throw new Error("No passkey was made.")
  const response = credential.response as AuthenticatorAttestationResponse
  const body = {
    kind: "passkey", name, origin,
    credential: {
      id: credential.id, rawId: toBase64Url(credential.rawId), type: credential.type,
      authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
      response: {
        clientDataJSON: toBase64Url(response.clientDataJSON),
        attestationObject: toBase64Url(response.attestationObject),
        transports: response.getTransports?.() ?? [],
      },
      clientExtensionResults: credential.getClientExtensionResults?.() ?? {},
    },
  }
  const enrolled = await apiFetch(baseUrl, "/api/devices", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  })
  if (!enrolled.ok) throw await errorOf(enrolled)
  return enrolled.json()
}

/**
 * Signs a decision with this page's passkey: the server's canonical request
 * and challenge, the option, and the person's verification in the browser.
 */
export async function signWithPasskey(baseUrl: string, approvalId: string, device: Device, option: string): Promise<void> {
  const resp = await apiFetch(baseUrl, `/api/approvals/${encodeURIComponent(approvalId)}/challenge`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ device: device.id }),
  })
  if (!resp.ok) throw await errorOf(resp)
  const given = (await resp.json()) as { canonical: string; challenge: string; passkey?: { credential_id: string; rp_id: string } }
  if (!given.passkey) throw new Error("The server did not name this device's passkey.")
  // fromBase64Url reads standard base64 too, which the server sends here.
  const canonical = new TextDecoder().decode(fromBase64Url(given.canonical))
  const message = new TextEncoder().encode(signedMessage(given.challenge, device.id, option, canonical))
  const hash = await crypto.subtle.digest("SHA-256", message)
  const assertion = await navigator.credentials.get({
    publicKey: {
      challenge: hash, rpId: given.passkey.rp_id, userVerification: "required", timeout: 120_000,
      allowCredentials: [{ type: "public-key", id: fromBase64Url(given.passkey.credential_id) }],
    },
  }) as PublicKeyCredential | null
  if (!assertion) throw new Error("The passkey did not sign.")
  const r = assertion.response as AuthenticatorAssertionResponse
  const value = JSON.stringify({
    id: assertion.id, rawId: toBase64Url(assertion.rawId), type: assertion.type,
    response: {
      clientDataJSON: toBase64Url(r.clientDataJSON), authenticatorData: toBase64Url(r.authenticatorData),
      signature: toBase64Url(r.signature), userHandle: r.userHandle ? toBase64Url(r.userHandle) : undefined,
    },
    clientExtensionResults: assertion.getClientExtensionResults?.() ?? {},
  })
  const decided = await apiFetch(baseUrl, `/api/approvals/${encodeURIComponent(approvalId)}/decision`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ decision: "allow", option, by: "passkey", signature: { device: device.id, challenge: given.challenge, value } }),
  })
  if (!decided.ok) throw await errorOf(decided)
}
