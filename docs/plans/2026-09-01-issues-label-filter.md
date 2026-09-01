# Issues Label Filter Search Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the Labels filter search input accept text and apply a matching label when Enter is pressed.

**Architecture:** Keep `LabelSubContent` inside the existing Base UI dropdown submenu. Stop printable, unmodified keydown events at the native search input so Base UI menu typeahead cannot prevent text entry. On Enter, resolve an exact case-insensitive name match first, otherwise the sole filtered result, and pass its ID to the existing `toggleLabelFilter` path. Preserve Escape, Tab, modifier shortcuts, selection, and menu navigation.

**Tech Stack:** React, TypeScript, Base UI DropdownMenu, TanStack Query, Vitest, Testing Library, user-event.

---

### Task 1: Add failing label search regression test

**Files:**
- Modify: `packages/views/issues/components/issues-page.test.tsx`
- Test target: shared Issues page filter controls and Label submenu

**Step 1: Extend test mocks with workspace labels.**

Provide two labels, one matching and one non-matching the query, through the same labels query used by `LabelSubContent`.

**Step 2: Write the failing test.**

Render `IssuesPage`, open Filter, open Label, locate the opened submenu's search input, focus it, type a distinctive query, and assert the input value and visible labels. Assert Escape still closes the Label submenu. The test must exercise keyboard typing while the real Base UI menu primitives are mounted.

**Step 3: Run the focused test to verify it fails for the reported interaction.**

Run:

```bash
pnpm --filter @rimedeck/views exec vitest run issues/components/issues-page.test.tsx -t "allows typing in the label filter search input"
```

Expected before the production change: FAIL because Base UI menu typeahead prevents the input's printable keydown events.

### Task 2: Implement local keyboard event isolation

**Files:**
- Modify: `packages/views/issues/components/issues-header.tsx:465-476`

**Step 1: Add the smallest input-local handler.**

Stop propagation only when the input receives an unmodified printable key (`event.key.length === 1`, with Ctrl/Meta/Alt absent). Leave Escape, Tab, Shift+Tab, arrows, and modifier shortcuts available to Base UI's existing menu handlers.

**Step 2: Run the focused regression test.**

Run the command from Task 1.

Expected: PASS, with the input containing the typed query, only the matching label visible, and Escape dismissing the submenu.

### Task 3: Apply a matching label on Enter

**Files:**
- Modify: `packages/views/issues/components/issues-header.tsx:469-486`
- Test: `packages/views/issues/components/issues-page.test.tsx`

**Step 1: Write and run the failing Enter regression.**

Type an exact label name and press Enter. Assert `toggleLabelFilter` receives the matching label ID. Before implementation, the mock must have zero calls.

**Step 2: Implement the minimal Enter handler.**

For a non-empty normalized query, select an exact case-insensitive match; if none exists and the filtered result is unique, select it. Prevent default menu handling only when a match is applied, call `onToggle(match.id)`, and clear the search text.

**Step 3: Run the Enter regression.**

Expected: PASS.

### Task 4: Run focused existing verification

**Files:**
- No additional files.

**Step 1: Run the complete Issues page component test file.**

```bash
pnpm --filter @rimedeck/views exec vitest run issues/components/issues-page.test.tsx
```

Expected: PASS.

**Step 2: Run the label filtering utility tests.**

```bash
pnpm --filter @rimedeck/views exec vitest run issues/utils/filter.test.ts
```

Expected: PASS.

**Step 3: Run the views package typecheck.**

```bash
pnpm --filter @rimedeck/views typecheck
```

Expected: exit code 0.

**Step 4: Review the final diff for scope.**

Confirm only the label input interaction and its regression coverage changed beyond the design/plan documents; no shared dropdown semantics or unrelated filters were modified.
