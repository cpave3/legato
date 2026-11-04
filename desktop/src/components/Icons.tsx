// Small line icons, drawn at 16px on a 24px grid.

type Props = { size?: number }

function Svg({ size = 16, children }: Props & { children: React.ReactNode }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {children}
    </svg>
  )
}

export const Minimize = (p: Props) => <Svg {...p}><path d="M5 12h14" /></Svg>
export const Maximize = (p: Props) => <Svg {...p}><rect x="5" y="5" width="14" height="14" rx="2" /></Svg>
export const Restore = (p: Props) => <Svg {...p}><rect x="5" y="8" width="11" height="11" rx="2" /><path d="M8 5h9a2 2 0 0 1 2 2v9" /></Svg>
export const Close = (p: Props) => <Svg {...p}><path d="M6 6l12 12M18 6L6 18" /></Svg>
export const Plus = (p: Props) => <Svg {...p}><path d="M12 5v14M5 12h14" /></Svg>
export const KeyIcon = (p: Props) => <Svg {...p}><circle cx="8" cy="15" r="4" /><path d="M11 12l9-9M16 7l3 3M14 9l2 2" /></Svg>
export const Servers = (p: Props) => <Svg {...p}><rect x="4" y="4" width="16" height="7" rx="2" /><rect x="4" y="13" width="16" height="7" rx="2" /><path d="M8 7.5h.01M8 16.5h.01" /></Svg>
export const Reload = (p: Props) => <Svg {...p}><path d="M20 11a8 8 0 1 0-2.3 5.7" /><path d="M20 4v7h-7" /></Svg>
export const Seal = (p: Props) => <Svg {...p}><path d="M12 3l7.8 4.5v9L12 21l-7.8-4.5v-9z" /><path d="M8.5 12.2l2.4 2.4 4.6-4.8" /></Svg>
export const Lock = (p: Props) => <Svg {...p}><rect x="5" y="11" width="14" height="9" rx="2" /><path d="M8 11V8a4 4 0 0 1 8 0v3" /></Svg>
export const Fingerprint = (p: Props) => <Svg {...p}><path d="M6 13c0-3.9 2.7-7 6-7s6 3.1 6 7" /><path d="M9 18c-.6-1.4-.9-3-.9-5 0-2.3 1.7-4 3.9-4s3.9 1.7 3.9 4c0 1.8-.3 3.6-.9 5.2" /><path d="M12 13c0 2.6-.4 5-1.2 7" /></Svg>
export const Download = (p: Props) => <Svg {...p}><path d="M12 4v11M7 10l5 5 5-5" /><path d="M5 19h14" /></Svg>
