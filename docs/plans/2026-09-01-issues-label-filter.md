# Issues Label Filter Search Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the Labels filter search input focusable and typeable while preserving existing label filtering behavior.

**Architecture:** Keep `LabelSubContent` inside the existing Base UI dropdown submenu. Add only the local event isolation required for the native search input so the parent menu does not consume its pointer or keyboard events. Verify the observable behavior through a focused component regression test.

**Tech Stack:** React, TypeScript, Base UI DropdownMenu, TanStack Query, Vitest, Testing Library, user-event.

---

### Task 1: Add failing label search regression test

**Files:**
- Modify: `packages/views/issues/components/issues-page.test.tsx`
- Test target: shared Issues page filter controls and Label submenu

**Step 1: Extend test mocks with workspace labels and Base UI menu behavior needed by the test.**

Provide two labels, one matching and one non-matching the query, through the same labels query used by `LabelSubContent`. Mock or render the menu primitives consistently with existing test conventions.

**Step 2: Write the failing test.**

Render `IssuesPage`, open the Filter menu, open Label, locate the search input, type a distinctive query, and assert the matching label is present while the non-matching label is absent. The test must exercise focus and typing, not only the pure filtering helper.

**Step 3: Run the focused test and verify it fails for the reported interaction.**

Run:

```bash
pnpm --filter @rimedeck/web exec vitest run packages/views/issues/components/issues-page.test.tsx -t "label filter search"
```

Expected: failure demonstrating that the input does not accept the typed query or that the visible label list does not update.

### Task 2: Implement local event isolation

**Files:**
- Modify: `packages/views/issues/components/issues-header.tsx:464-472`

**Step 1: Add the smallest input-local event handlers required by the failing test.**

Prevent the parent dropdown's pointer interaction from reclaiming focus, and stop menu-level keyboard handling from interpreting text-entry keys as menu typeahead. Preserve the controlled `value`, `onChange`, placeholder, and autofocus behavior.

**Step 2: Run the focused regression test.**

Run the command from Task 1.

Expected: PASS, with matching labels filtered by typed text.

### Task 3: Run focused existing verification

**Files:**
- No additional files.

**Step 1: Run the complete Issues page component test file.**

```bash
pnpm --filter @rimedeck/web exec vitest run packages/views/issues/components/issues-page.test.tsx
```

Expected: PASS.

**Step 2: Run the label filter utility tests.**

```bash
pnpm --filter @rimedeck/web exec vitest run packages/views/issues/utils/filter.test.ts
```

Expected: PASS.

**Step 3: Inspect the final diff for scope.**

Confirm only the design document, implementation plan, regression test, and label input interaction code changed; no shared dropdown semantics or unrelated filters were modified.
