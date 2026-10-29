# Agent instructions

- Never stop, kill, restart, or replace a running Legato process unless the user explicitly asks for that process action. A request to change, build, validate, or deploy Legato does not imply permission to disrupt a running process.
- Do not perform browser or end-to-end testing in this project unless the user explicitly asks for it.
- How Legato is built, and how to work on it, is in `CLAUDE.md` and the documents it lists under "Detailed Documentation" (`docs/ai/*.md`): read the ones for the area you change, and keep them current.
