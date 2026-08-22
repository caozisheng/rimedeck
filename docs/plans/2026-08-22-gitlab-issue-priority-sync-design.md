# GitLab Issue Priority Sync Design

**Date:** 2026-08-22  
**Status:** Approved

## Problem

Changing a GitLab-mirrored Issue's priority must update the remote GitLab Issue. GitLab's Issue label update is destructive replacement, so the remote write must use the imported mapping identity and send a complete canonical label set rather than only a priority fragment. Remote imports and canonical responses must map the same labels back to the local `priority` field.

## Scope

This change applies only to Issues with `source_type = 'gitlab'`.

Priority mapping is exact, case-insensitive, and bidirectional:

| GitLab label | RimeDeck priority |
| --- | --- |
| `priority::urgent` | `urgent` |
| `priority::high` | `high` |
| `priority::medium` | `medium` |
| `priority::low` | `low` |
| no priority mapping label | `none` |

Local `none` removes all priority mapping labels; it does not create `priority::none`.

If multiple priority mapping labels are present remotely, projection precedence is `urgent > high > medium > low`. All remote label relations remain stored for internal reconciliation; the next local write canonicalizes the priority dimension to one label or zero labels.

## Approach

Reuse the existing GitLab tracker architecture:

1. Centralize the `priority::urgent` mapping and priority precedence in `server/internal/gitlabtracker/mapping.go`.
2. Keep import/reconcile/canonical-apply projection through `ProjectIssueFields`, so remote labels update the local native priority field.
3. Keep local single and batch Issue updates local-first. When priority changes, build the complete desired label set from:
   - ordinary labels belonging to the same GitLab tracker;
   - the canonical workflow label derived from local status, if mapped;
   - the canonical priority label derived from local priority, if mapped.
4. Enqueue the existing `update_issue` outbox operation with that label set. Do not add a new `set_priority` operation.
5. The worker sends one GitLab PUT, then applies the canonical response through the existing revision guard. A stale response cannot overwrite a newer local revision.
6. Keep ordinary labels, local labels, and labels belonging to another tracker out of the remote payload.

## Error Handling and Invariants

- A remote write failure leaves the local priority visible and uses existing pending/retry/failed outbox behavior.
- A remote response is authoritative only when its desired revision still matches the local revision guard.
- `priority::none` is not a supported mapped label and remains an ordinary label if a GitLab project happens to define it.
- Unknown `priority::...` names remain ordinary labels and do not change native priority.
- Importing a remote priority label must preserve the complete relation and set `mapping_kind = 'priority'` on the mirrored label definition.
- A priority-only local update must still send workflow mapping and ordinary same-tracker labels so GitLab's destructive replacement cannot erase them.

## Verification

Focused tests must prove:

- mapping classification, projection, precedence, and canonical output for urgent/high/medium/low/none;
- first import and canonical apply map `priority::urgent` to local `urgent` while preserving complete label relations;
- single local priority updates enqueue `update_issue` with the complete canonical label set;
- batch priority updates do the same;
- the worker sends the expected labels in one remote PUT and applies the response;
- ordinary labels and workflow mapping survive a priority-only update;
- `none` removes priority mapping labels, while `priority::none` remains ordinary.
