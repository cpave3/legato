import { useEffect, useRef, useState } from "react"
import { Html5Qrcode } from "html5-qrcode"
import { Camera, X } from "lucide-react"

interface PairData {
  url: string
  token: string
}

interface QRScannerProps {
  onScan: (data: PairData) => void
  onClose: () => void
}

function parsePairURI(text: string): PairData | null {
  try {
    // Accept legato://pair?url=...&token=... format.
    if (!text.startsWith("legato://pair")) return null
    const url = new URL(text.replace("legato://", "https://"))
    const serverUrl = url.searchParams.get("url")
    const token = url.searchParams.get("token")
    if (serverUrl && token) {
      return { url: serverUrl, token }
    }
  } catch {
    // Not a valid URI.
  }
  return null
}

export function QRScanner({ onScan, onClose }: QRScannerProps) {
  const [error, setError] = useState("")
  const scannerRef = useRef<Html5Qrcode | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const scanner = new Html5Qrcode("qr-reader")
    scannerRef.current = scanner

    scanner
      .start(
        { facingMode: "environment" },
        { fps: 10, qrbox: { width: 250, height: 250 } },
        (decodedText) => {
          const data = parsePairURI(decodedText)
          if (data) {
            scanner.stop().catch(() => {})
            onScan(data)
          } else {
            setError("Not a Legato pairing QR code")
            setTimeout(() => setError(""), 2000)
          }
        },
        () => {} // ignore scan failures (no QR in frame)
      )
      .catch((err) => {
        if (String(err).includes("Permission")) {
          setError("Camera access denied. Use manual token entry instead.")
        } else {
          setError(`Camera error: ${err}`)
        }
      })

    return () => {
      scanner.stop().catch(() => {})
    }
  }, [onScan])

  return (
    <div className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-zinc-950/95 backdrop-blur-sm">
      <div className="w-full max-w-sm">
        <div className="flex items-center justify-between mb-4 px-2">
          <div className="flex items-center gap-2 text-zinc-200">
            <Camera size={20} />
            <span className="text-sm font-medium">Scan Pairing QR Code</span>
          </div>
          <button
            onClick={onClose}
            className="rounded p-1 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-300 transition-colors"
          >
            <X size={18} />
          </button>
        </div>
        <div
          id="qr-reader"
          ref={containerRef}
          className="rounded-lg overflow-hidden bg-black"
        />
        {error && (
          <p className="mt-3 text-sm text-red-400 text-center">{error}</p>
        )}
        <p className="mt-3 text-xs text-zinc-500 text-center">
          Run <code className="text-zinc-400 bg-zinc-800 px-1 rounded">legato pair</code> on the server to show the QR code
        </p>
      </div>
    </div>
  )
}
