import type { Approval } from "./legato"

// Agents' question requests, as Legato lists them, for the tests.
export const images: Approval = {
  id: "ap-q1",
  source: "agent_question",
  task_id: "LEG-157",
  title: "Where should Legato keep its copies of a plan's image files?",
  options: [],
  details: {
    questions: [
      {
        id: "q1",
        header: "Image files",
        question: "Where should Legato keep its copies of a plan's image files?",
        options: [
          { label: "Beside the plan (Recommended)", description: "In the plan's own folder, so the files move and are removed with it." },
          { label: "In a shared cache", description: "One folder for every plan, each file kept once by its content hash." },
          { label: "Leave them where they are" },
        ],
        multiSelect: false,
      },
    ],
  },
}

export const twoParts: Approval = {
  id: "ap-q2",
  source: "agent_question",
  task_id: "LEG-163",
  title: "How often should the glasses read Legato while the screen is on?",
  options: [],
  details: {
    questions: [
      {
        id: "poll",
        header: "Polling",
        question: "How often should the glasses read Legato while the screen is on?",
        options: [
          { label: "Every 5 seconds", description: "Uses a little more battery on the phone." },
          { label: "Every 15 seconds", description: "Gentler on the battery." },
          { label: "Only when I tap", description: "Nothing updates by itself." },
        ],
      },
      {
        id: "bg",
        header: "Background",
        question: "What should background mode show across the top?",
        options: [
          { label: "Who needs you", description: "The first agent waiting on you, by task." },
          { label: "Agent counts", description: "Running, waiting and idle, dim on the right." },
          { label: "Glasses battery" },
          { label: "Time" },
        ],
        multiSelect: true,
      },
    ],
  },
}
