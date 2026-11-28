import { Link } from "react-router-dom"
import { ChangeSetCard } from "./ChangeSetCard"
import { SecretRequestForm } from "./SecretRequestForm"
import { NativeQuestionCard } from "./NativeQuestionCard"
import { approvalAsker, approvalKind, dismissApproval, type Approval, type ApprovalOption } from "../../lib/approvals"
import { approvalReason, approvalRunText } from "../../lib/approvalCard"
import type { NativeQuestion } from "../../lib/nativeAgentSync"
import { shellApprovals } from "../../lib/mobileShell"
import { signWithPasskey } from "../../lib/passkeys"
import type { Device } from "../../lib/devices"
import type { ApprovalDecisionState } from "./useApprovalCard"
import { choiceSize, choiceTone, primaryChoice } from "./choiceStyles"

// The kind of approval, whether the agent waits, who asks, and the title.
export function ApprovalHeader({ approval }: { approval: Approval }) {
  const asker = approvalAsker(approval)
  return (
    <>
      <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-lw-needs md:text-[11px]">
        <span>{approvalKind(approval)}</span>
        {approval.blocking && <span className="text-lw-faint">· waiting</span>}
        {approval.task_id && (
          <Link to={`/agents?task=${encodeURIComponent(approval.task_id)}`} className="-my-2 ml-auto flex min-h-11 items-center px-1 text-sm normal-case tracking-normal text-lw-link hover:underline md:my-0 md:min-h-0 md:px-0 md:text-[11px]">
            Open agent
          </Link>
        )}
      </div>
      <h3 className="mt-1 break-words text-lg font-semibold leading-snug md:text-sm">{approval.title}</h3>
      {asker && <p className="text-sm text-lw-text-2 md:text-xs">{asker}</p>}
    </>
  )
}

// The body of the kinds of approval that draw their own.
export function SourceBody({ approval, baseUrl, onDone }: { approval: Approval; baseUrl: string; onDone: () => Promise<void> }) {
  switch (approval.source) {
    case "credential": return <CredentialScope approval={approval} />
    case "reeve_change_set": return <ChangeSetCard approval={approval} />
    case "reeve_secret": return <SecretRequestForm approval={approval} baseUrl={baseUrl} onDone={onDone} />
    default: return null
  }
}

// An agent's questions, answered on the card.
export function QuestionsBody({ approval, questions, baseUrl, decision }: { approval: Approval; questions: NativeQuestion[]; baseUrl: string; decision: ApprovalDecisionState }) {
  const { busy, error, run, decide } = decision
  return (
    <div className="-mx-4 mt-1">
      <NativeQuestionCard
        question={{ requestId: approval.id, questions, status: busy ? "submitting" : "pending", error: error ?? undefined }}
        touch
        askedBy={approval.details.agent_kind || "The agent"}
        onAnswer={(_, answers) => decide({ decision: "answer", answers })}
        onDismiss={() => run(() => dismissApproval(baseUrl, approval.id))}
      />
    </div>
  )
}

// What the approval would do and why: the command or summary, the plan
// folded away, the reason and the folder.
export function ApprovalDetails({ approval }: { approval: Approval }) {
  const details = approval.details
  const runs = approvalRunText(approval)
  const reason = approvalReason(approval)
  return (
    <>
      {runs && (
        <pre className="mt-3 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-lw-ground p-3 font-lw-mono text-sm md:mt-2 md:max-h-40 md:rounded md:p-2 md:text-xs">{runs}</pre>
      )}
      {details.plan && (
        <details className="mt-3 text-sm md:mt-2 md:text-xs">
          <summary className="flex min-h-11 cursor-pointer items-center text-lw-text-2 md:min-h-0">Show the plan</summary>
          <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded bg-lw-ground p-2 font-lw-mono">{details.plan}</pre>
        </details>
      )}
      {reason && <p className="mt-2 break-words text-sm text-lw-text-2 md:mt-1 md:text-xs">{reason}</p>}
      {details.cwd && <p className="mt-1 truncate font-lw-mono text-xs text-lw-faint md:text-[11px]">{details.cwd}</p>}
    </>
  )
}

interface SignedChoicesProps {
  approval: Approval
  allows: ApprovalOption[]
  baseUrl: string
  passkey: Device | undefined
  decision: ApprovalDecisionState
}

// A signed approval's choices: the app's sheet inside the app, this page's
// passkey in a browser that has one, else where to sign it.
export function SignedChoices({ approval, allows, baseUrl, passkey, decision }: SignedChoicesProps) {
  const { busy, setBusy, setError, run } = decision
  const phone = shellApprovals()
  if (phone) {
    return (
      <button
        disabled={busy}
        onClick={() => { setError(null); setBusy(true); phone.open(approval.id) }}
        className={`${choiceSize} gap-2 ${primaryChoice}`}
      >
        <span aria-hidden>◈</span> Approve and sign
      </button>
    )
  }
  if (!passkey) {
    return (
      <p className="rounded-lg border border-lw-accent/40 bg-lw-accent-soft px-3 py-2 text-sm text-lw-accent-text md:text-xs">
        <span aria-hidden>◈ </span>Needs your phone: open this approval in the Legato app and sign it with your fingerprint or PIN, or add a passkey for this computer in Settings › Security › Approval devices.
      </p>
    )
  }
  return (
    <>
      {allows.map((option, index) => (
        <button
          key={option.id}
          disabled={busy}
          aria-label={`Sign with passkey: ${option.label}`}
          onClick={() => run(() => signWithPasskey(baseUrl, approval.id, passkey, option.id))}
          className={`${choiceSize} gap-2 ${choiceTone(index)}`}
        >
          <span aria-hidden>◈</span> {option.label}
        </button>
      ))}
    </>
  )
}

// What a credential request asks for: the operations, each resource (a
// pattern marked broader), and the agent's reason in its own words.
function CredentialScope({ approval }: { approval: Approval }) {
  const details = approval.details
  const resources = details.resources ?? []
  return (
    <div className="mt-3 space-y-2 text-sm md:mt-2 md:space-y-1.5 md:text-xs">
      <div className="flex flex-wrap items-center gap-1.5">
        {(details.operations ?? []).map((operation) => (
          <span key={operation} className="rounded bg-lw-accent-soft px-1.5 py-0.5 font-lw-mono text-lw-accent-text">{operation}</span>
        ))}
        {details.broader && <span className="rounded bg-lw-needs-soft px-1.5 py-0.5 font-medium text-lw-needs">Broader scope</span>}
        {details.signed && <span className="text-lw-faint">needs a signed decision (not enforced yet)</span>}
      </div>
      {resources.length > 0 ? (
        <ul className="space-y-0.5">
          {resources.map((resource) => (
            <li key={resource} className="break-all rounded-lg bg-lw-ground px-3 py-2 font-lw-mono md:rounded md:px-2 md:py-1">{resource}</li>
          ))}
        </ul>
      ) : (
        <p className="text-lw-text-2">All of {details.credential}</p>
      )}
      {details.reason && (
        <figure>
          <figcaption className="text-xs text-lw-faint md:text-[11px]">The agent's reason</figcaption>
          <blockquote className="break-words text-lw-text-2">“{details.reason}”</blockquote>
        </figure>
      )}
    </div>
  )
}
