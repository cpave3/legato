import { useCallback, useEffect, useState, type RefObject } from "react"
import { listJobClones, type JobClone } from "../../lib/jobClones"
import { cloneLoadError, cloneRepositoriesOf, firstCloneable, ownCloneStart, pickedClone, type CloneableRepository, type CloneStart } from "../../lib/spawnClones"

// What "Where" is: a folder on the server, or a clone Legato manages (a new
// one, or one an agent worked in before).
export type Place = "folder" | "clone"

// The settings the clone choice reads: whether the server offers clones
// (repository_cache.enabled), and the repositories it offers.
export interface CloneSettings {
  fresh_clones_enabled?: boolean
  repositories?: CloneableRepository[] | null
}

// The clones agents worked in, which another can work in again, listed
// each time the dialog opens when the server offers clones. startOn is
// told of the task's own clone, which its next agent most likely goes to.
function useJobClones(openKey: string, baseUrl: string, taskId: string, settingsRef: RefObject<CloneSettings | undefined>, startOn: (start: CloneStart | undefined) => void) {
  const [jobClones, setJobClones] = useState<JobClone[]>([])
  const [jobClonesError, setJobClonesError] = useState("")

  useEffect(() => {
    if (!openKey) return
    setJobClones([])
    setJobClonesError("")
    if (!settingsRef.current?.fresh_clones_enabled) return
    let current = true
    listJobClones(baseUrl)
      .then((listed) => {
        if (!current) return
        setJobClones(listed)
        startOn(ownCloneStart(listed, taskId, settingsRef.current?.repositories))
      })
      .catch((reason) => { if (current) setJobClonesError(cloneLoadError(reason)) })
    return () => { current = false }
  }, [openKey, baseUrl, taskId, settingsRef, startOn])

  return { jobClones, jobClonesError }
}

interface CloneChoiceOptions {
  // Changes whenever the dialog opens afresh; empty while it is closed.
  openKey: string
  baseUrl: string
  taskId: string
  defaultBranch: string
  settings: CloneSettings | undefined
  settingsRef: RefObject<CloneSettings | undefined>
}

// useCloneChoice is the spawn dialog's "Where" as a clone: folder or clone,
// which repository, which of its clones (or a new one), the branch the
// agent works on and the one a new clone starts from.
export function useCloneChoice({ openKey, baseUrl, taskId, defaultBranch, settings, settingsRef }: CloneChoiceOptions) {
  const [whereKind, setWhereKind] = useState<Place>("folder")
  const [cloneName, setCloneName] = useState("")
  const [branch, setBranch] = useState("")
  const [baseBranch, setBaseBranch] = useState("")
  // The clone picked: newClone, or null for the repository's first choice.
  const [jobCloneId, setJobCloneId] = useState<number | null>(null)
  const startOn = useCallback((start: CloneStart | undefined) => {
    if (!start) return
    setCloneName(start.name)
    setBaseBranch(start.baseBranch)
  }, [])
  const { jobClones, jobClonesError } = useJobClones(openKey, baseUrl, taskId, settingsRef, startOn)

  useEffect(() => {
    if (!openKey) return
    setWhereKind("folder")
    startOn(firstCloneable(settingsRef.current?.repositories))
    setBranch(defaultBranch)
    setJobCloneId(null)
  }, [openKey, defaultBranch, settingsRef, startOn])

  const repositories = cloneRepositoriesOf(Boolean(settings?.fresh_clones_enabled), settings?.repositories, jobClones)
  const picked = pickedClone(repositories, cloneName, jobClones, jobCloneId, taskId)

  // Another repository starts from its own base branch, on its first choice.
  const pickRepository = (name: string) => {
    setCloneName(name)
    setBaseBranch(repositories.find((repository) => repository.name === name)?.baseBranch ?? "")
    setJobCloneId(null)
  }

  return {
    whereKind, setWhereKind, branch, setBranch, baseBranch, setBaseBranch, setJobCloneId, jobClonesError,
    repositories,
    ...picked,
    inClone: whereKind === "clone" && repositories.length > 0,
    pickRepository,
  }
}

export type CloneChoice = ReturnType<typeof useCloneChoice>
