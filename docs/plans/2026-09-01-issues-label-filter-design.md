# Issues Label Filter Search Design

## Problem

The Issues page renders the label search field inside `DropdownMenuSubContent`. Users can open the Label submenu but cannot type into the field. The existing Assignee and Project submenus use the same native-input pattern, while the Label path is the reported failure surface.

## Decision

Keep the current `DropdownMenuSub` structure and label-selection semantics. Isolate the label search input from menu keyboard and pointer handling locally instead of replacing the menu with a second primitive.

The input remains controlled by local `search` state. Typing updates that state, filters the already-fetched workspace labels case-insensitively, and leaves checkbox selection wired to `toggleLabelFilter`. Menu item keyboard navigation remains unchanged outside the input.

## Components and data flow

1. `IssueDisplayControls` opens the existing filter dropdown and mounts `LabelSubContent`.
2. `LabelSubContent` fetches labels with `labelListOptions(useWorkspaceId())`.
3. The input receives focus when the label submenu opens and stops menu-level pointer/keyboard interception needed for text entry.
4. The filtered labels render as existing `DropdownMenuCheckboxItem` rows with `LabelChip`, counts, and current selection state.
5. Selecting a row continues to call `useViewStoreApi().getState().toggleLabelFilter`; no store or filtering semantics change.

## Error handling and scope

No new network behavior is introduced. Query loading/error behavior remains the existing empty-list fallback. The change is limited to label search interaction; no shared dropdown primitive or unrelated searchable controls are refactored.

## Verification

Add a component-level regression test that opens the Issues filter, enters the Label submenu, types a label query, and verifies the matching label remains while a non-matching label is removed. Run that focused test and the relevant existing Issues view tests.
