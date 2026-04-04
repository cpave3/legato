## 1. Numbered Option Extraction

- [x] 1.1 Add `extractNumberedActions` function to `internal/engine/prompt/detect.go` — scan lines from bottom up, collect contiguous `^\s*(\d+)\.\s+(.+)$` matches, return `[]Action` with digit as `Keys` and trimmed text as `Label` (reverse to restore top-down order)
- [x] 1.2 Add unit tests for `extractNumberedActions`: contiguous block, non-contiguous (gap breaks extraction), leading whitespace, no matches returns nil, single option, many options (5+)

## 2. Integrate Extraction into Detect

- [x] 2.1 Update `Detect()` tool approval branch: after type match, call `extractNumberedActions(tail)` — if non-empty, use extracted actions; otherwise fall back to current hardcoded `[Yes/Always/No]`
- [x] 2.2 Update `Detect()` plan approval branch: same pattern — call `extractNumberedActions(tail)`, fall back to hardcoded `[Accept/Reject]` if empty
- [x] 2.3 Remove the outdated comment about arrow-key selection list in detect.go

## 3. Update Tests

- [x] 3.1 Update existing tool approval test cases in `detect_test.go` to include numbered option lines in the test input, verify actions use digit keys
- [x] 3.2 Add test case: tool approval detected but no numbered lines → fallback to hardcoded arrow-key actions
- [x] 3.3 Add test case: plan approval with numbered options → digit keys
- [x] 3.4 Add test case: plan approval without numbered options → hardcoded Accept/Reject
- [x] 3.5 Add test case: free text prompt with numbered lines above → still returns FreeText with no actions
- [x] 3.6 Add test case: numbered lines with gap (non-contiguous) → only bottom block extracted
