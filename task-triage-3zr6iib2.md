# Triage: Add web agent macro hotkeys

**Task ID:** `3zr6iib2`  
**Disposition:** Ready for implementation  
**Estimated complexity:** Small — frontend-only behavior across two existing macro menus, with keyboard-focus and async-loading edge cases.

## User outcome

On the web Agents page, a user viewing either a native Chimera agent or a tmux-backed terminal agent can blur the message composer, press `M` to open that panel's macro menu, then press `1` through `9` to run the correspondingly numbered macro. Typing in an input remains unaffected, and clicking a macro continues to work.

## Current behavior

- `AgentsPage` chooses between the two relevant web surfaces (`web/src/pages/Agents.tsx`):
  - agents with `has_chimera_endpoint` render `NativeAgentPanel`;
  - other agents render `TerminalPanel` plus `PromptBar` (the tmux-backed view referred to in the brief).
- Both `NativeAgentPanel` and `PromptBar` already render a Macros button/dropdown and fetch `GET /api/macros`, but they fetch only after `macrosOpen` becomes true (`web/src/components/NativeAgentPanel.tsx`, `web/src/components/PromptBar.tsx`). Neither surface has macro keyboard handling or numbered labels.
- The two surfaces intentionally execute macros differently:
  - `NativeAgentPanel.handleMacro` calls `dispatchAgentInput`, preserving native `!command` handling through the Chimera bang endpoint and normal messages through `agent_message`.
  - `PromptBar` calls its existing `onSubmitMessage`, preserving the tmux agent's normal message transport rather than sending raw terminal keys.
- `AgentsPage` already has global keyboard listeners for voice dictation and modifier-plus-digit agent switching. Its voice shortcut demonstrates the required editable-target guard (`INPUT`, `TEXTAREA`, or `isContentEditable`). Macro digits must not conflict with agent switching, which uses Control/Alt/Meta plus a digit.
- The menu closes on outside click and after click selection. There is no keyboard-visible indication that menu entries map to digits.
- Existing component tests cover click-based macro execution in `web/src/components/__tests__/NativeAgentPanel.test.tsx` and `web/src/components/__tests__/PromptBar.test.tsx`.

## Implementation scope

### 1. Add shared macro-menu keyboard behavior

Implement a small reusable hook/helper under `web/src/hooks/` (for example, `useMacroHotkeys`) and use it from both `NativeAgentPanel` and `PromptBar`. It should receive the current macro list, whether the menu is open, an open/close callback, and the surface's existing macro-selection callback.

The window-level `keydown` behavior should be:

- Ignore events already handled elsewhere (`event.defaultPrevented`), key repeats, and events with Control, Alt, Meta, or Shift active.
- Ignore events whose target is an `INPUT`, `TEXTAREA`, `SELECT`, or content-editable element. This is the required “not focused in the text field” boundary and also avoids stealing keys from any other editable control mounted on the page.
- When the menu is closed, `m` or `M` opens it and calls `preventDefault()`. It should open, not toggle, so repeated setup cannot unexpectedly close the menu.
- When the menu is open:
  - `1` through `9` selects zero-based macro indexes `0` through `8` through the supplied callback and calls `preventDefault()` only when that macro exists;
  - `Escape` closes the menu without running anything;
  - unrelated keys and digits without a corresponding macro are left alone.
- Only the currently rendered agent surface owns the listener: `NativeAgentPanel` and `PromptBar` are mutually exclusive in `AgentsPage`, so no page-level routing or agent-kind branch is needed.

Do not call WebSocket or API transports from the hotkey helper. Selection must invoke the same existing handler used by a menu click so native bang macros, native queued bang behavior, and tmux message submission remain unchanged.

### 2. Make the macro list ready before numbered selection

Change both surfaces from open-triggered fetching to eager fetching when the component mounts (and when `baseUrl` changes). This removes the avoidable race where `M`, then a digit arrives before opening the menu has even started loading its entries.

- Continue using authenticated `apiFetch(baseUrl, "/api/macros")`.
- Preserve the configured API order; it defines the `1`, `2`, `3`, … mapping.
- Keep an empty list on request failure and retain the existing `(no macros configured)` state.
- Do not change the `/api/macros` response or add server-side hotkey fields. Number assignment is presentation-only and deterministic from list order.

A request can still be in flight immediately after a panel first mounts. During that interval, opening the menu should not execute an unknown index; once entries render, numbered selection is active. No keystroke queue or speculative execution is required.

### 3. Expose the shortcuts in both menus

Update both macro dropdown renderers so the first nine rows visibly show a compact `<kbd>` label (`1`–`9`) before the macro name. Keep all configured macros visible and clickable.

- Add `aria-keyshortcuts="m"` (or equivalent accessible shortcut metadata) to each Macros trigger.
- Add the corresponding digit shortcut metadata to the first nine macro buttons.
- Update the Macros button tooltip/title to advertise `M`.
- Macro 10 and later receive no digit badge or keyboard shortcut but remain clickable.

The scoped numbering rule is deliberately **first nine only**. A single-key sequence maps unambiguously to `1`–`9`; supporting `10+` would require a multi-key timeout/state design and is outside this task. The requested third and fourth macro flows are fully covered.

### 4. Document the web shortcut

Update the Macros section in `README.md` and the macro/keyboard notes in `docs/ai/web-ui.md` to state:

- on the web Agents view, with no editable field focused, `M` opens the active agent's macro menu;
- `1`–`9` run the first nine configured macros in list order;
- later macros remain pointer-selectable;
- the native and tmux surfaces retain their existing transport semantics.

No TUI behavior change is part of this task. The TUI already binds `m` to its macro picker, but its picker does not currently assign number keys.

## Acceptance criteria

1. In the web native Chimera agent panel, with the composer unfocused, pressing `M` opens the macro menu.
2. In the web tmux-backed agent view, with the prompt textarea unfocused, pressing `M` opens the `PromptBar` macro menu.
3. In either open menu, pressing `3` runs the third configured macro and closes the menu; pressing `4` runs the fourth and closes it.
4. Numbered selection uses the exact existing click execution path: native `!` macros still use the Chimera bang endpoint/queue behavior, native normal macros use `agent_message`, and tmux macros use `onSubmitMessage` rather than raw `send_keys`.
5. The first nine menu rows display `1`–`9` in API/config order and expose matching accessible keyboard-shortcut metadata. Macros after the ninth remain visible and clickable without a number shortcut.
6. `M`, digits, and Escape do not trigger macro behavior while focus is in an input, textarea, select, or content-editable element.
7. Modified digits continue to switch agents through the existing configured Control/Alt/Meta shortcut and never run macros. Modified `M`, Shift+`M`, and repeated keydown events do not trigger macro behavior.
8. Escape closes an open macro menu without executing a macro. A digit with no corresponding macro has no effect.
9. The macro list is requested before the menu first opens; failed or empty responses show the existing empty state and do not make digit keys executable.
10. Existing click selection, outside-click dismissal, voice dictation, composer keyboard handling, and agent switching remain unchanged.

## Test plan

Follow the repository's TDD instruction: add failing component-level behavior tests first, then implement.

### `web/src/components/__tests__/NativeAgentPanel.test.tsx`

- Return at least four ordered macros from `/api/macros`, render the hydrated panel, blur the composer, press `M` on `window`, and assert the menu opens with numbered labels.
- Press `3`; assert the third macro reaches the same observable native transport as clicking it and the menu closes.
- Include a numbered `!` macro case or retain the existing click bang test while making the shared callback path explicit, so replacing selection with a raw `agent_message` would fail a test.
- Focus the composer and assert `M` does not open the menu and a digit does not execute a macro.
- Assert Escape, missing digits, modifiers, and repeat events do not execute a macro.

### `web/src/components/__tests__/PromptBar.test.tsx`

- Return at least four ordered macros, blur the textarea, press `M`, then `4`; assert `onSubmitMessage` receives only the fourth macro, `onSendKeys` is not called, and the menu closes.
- Assert the first nine entries have visible/accessibility shortcut labels and a tenth entry remains clickable without a digit shortcut.
- Focus the textarea and assert typing `m`/digits remains normal and does not open or execute the menu.
- Assert Control/Alt/Meta plus a digit is ignored by macro handling, protecting the page-level agent-switch shortcut.

Run:

```bash
cd web
pnpm test -- src/components/__tests__/NativeAgentPanel.test.tsx src/components/__tests__/PromptBar.test.tsx
pnpm build
pnpm lint
```

## Risks and boundaries

- **Preserve execution semantics:** bypassing `handleMacro`/`onSubmitMessage` would regress native bang execution or turn tmux macro submission into the wrong transport. The hotkey must be only another selection mechanism.
- **Avoid async first-open races:** retaining fetch-on-open makes the advertised `M`, digit sequence timing-dependent. Eager loading is part of the feature, not an optional optimization.
- **Avoid global-key collisions:** reject modified keys and editable targets. In particular, unmodified macro digits must not interfere with the established modifier-plus-digit agent switcher.
- **No backend/config migration:** shortcut numbers are derived from the existing ordered macro list; no new macro schema, endpoint, persistence, or server handler is needed.
- **No TUI or board-page expansion:** scope is the two web agent views only. Number shortcuts in the TUI macro picker or other pages should be separate work.
