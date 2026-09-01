# Issues Label Filter Search Design

## Problem

The Issues page renders the label search field inside `DropdownMenuSubContent`. Users can open the Label submenu but cannot type into the field. Base UI's menu typeahead handles printable keydown events from the popup and calls `preventDefault`, so the native input never receives text. The existing Assignee and Project submenus use the same native-input pattern, but the Label path is the reported failure surface.

## Decision

Keep the current `DropdownMenuSub` structure and label-selection semantics. Isolate the label search input from the menu's keyboard typeahead locally instead of replacing the menu with a second primitive.

The input remains controlled by local `search` state. Typing updates that state and filters the already-fetched workspace labels case-insensitively. Pressing Enter selects an exact case-insensitive label-name match, or the sole remaining result when there is no exact match, then clears the search. Checkbox selection remains wired to `toggleLabelFilter`. Menu item keyboard navigation and dismissal remain unchanged because only printable text-entry keys and a successful Enter selection are stopped.

## Components and data flow

1. `IssueDisplayControls` opens the existing filter dropdown and mounts `LabelSubContent`.
2. `LabelSubContent` fetches labels with `labelListOptions(useWorkspaceId())`.
3. The input stops printable, unmodified keydown events from reaching Base UI menu typeahead.
4. Enter resolves an exact label-name match first, then a sole filtered result, and passes its ID to `onToggle`.
5. The filtered labels render as existing `DropdownMenuCheckboxItem` rows with `LabelChip`, counts, and current selection state.
6. Selecting a row or pressing Enter calls `useViewStoreApi().getState().toggleLabelFilter`; no store or issue-filtering semantics change.

## Error handling and scope

No new network behavior is introduced. Query loading/error behavior remains the existing empty-list fallback. The change is limited to label search interaction; no shared dropdown primitive or unrelated searchable controls are refactored.

## Verification

Add component-level regression tests that open the Issues filter, enter the Label submenu, type a label query, and verify the matching option remains while a non-matching label is removed. Verify Enter applies the matching label ID and Escape still dismisses the submenu. Run those tests and the relevant existing Issues filtering tests.
