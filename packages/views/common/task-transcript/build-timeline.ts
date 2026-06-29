import type { TaskMessagePayload } from "@rimedeck/core/types/events";
import { redactSecrets } from "./redact";

/** A unified timeline entry: tool calls, thinking, text, logs, and errors in chronological order. */
export interface TimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "text" | "log" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
}

function canMergeStreamingText(prev: TimelineItem, next: TimelineItem): boolean {
  return (prev.type === "thinking" || prev.type === "text") && prev.type === next.type;
}

/** Merge adjacent text/thinking fragments that were split only by daemon flush timing. */
export function coalesceTimelineItems(items: TimelineItem[]): TimelineItem[] {
  const sorted = [...items].sort((a, b) => a.seq - b.seq);
  const out: TimelineItem[] = [];

  for (const item of sorted) {
    const prev = out[out.length - 1];
    if (prev && canMergeStreamingText(prev, item)) {
      out[out.length - 1] = {
        ...prev,
        content: `${prev.content ?? ""}${item.content ?? ""}`,
        created_at: item.created_at ?? prev.created_at,
      };
      continue;
    }
    out.push(item);
  }

  return out;
}

export function appendTimelineItem(items: TimelineItem[], item: TimelineItem): TimelineItem[] {
  return coalesceTimelineItems([...items, item]);
}

function redactTimelineItems(items: TimelineItem[]): TimelineItem[] {
  return items.map((item) => ({
    ...item,
    content: item.content ? redactSecrets(item.content) : item.content,
    output: item.output ? redactSecrets(item.output) : item.output,
  }));
}

/** Build a chronologically ordered timeline from raw task messages. */
export function buildTimeline(msgs: TaskMessagePayload[]): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const msg of msgs) {
    const content = msg.content;
    const type = normalizeTaskMessageType(msg.type, content);
    if (type == null) continue;
    items.push({
      seq: msg.seq,
      type,
      tool: msg.tool,
      content,
      input: msg.input,
      output: msg.output,
      created_at: msg.created_at,
    });
  }
  return redactTimelineItems(coalesceTimelineItems(items));
}

function normalizeTaskMessageType(
  type: TaskMessagePayload["type"],
  content: string | undefined,
): TimelineItem["type"] | null {
  if (type === "progress") {
    return isVisibleLifecycleProgress(content) ? "log" : null;
  }
  if (type === "thinking" && isLegacyPublicProgress(content)) {
    return null;
  }
  return type;
}

function isVisibleLifecycleProgress(content: string | undefined): boolean {
  const text = content?.trim() ?? "";
  return (
    text === "Task queued" ||
    text === "Task dispatched to runtime" ||
    text === "Runtime started task" ||
    text === "Waiting for local directory"
  );
}

function isLegacyPublicProgress(content: string | undefined): boolean {
  const text = content?.trim() ?? "";
  if (text === "") return false;
  return (
    /^Running [^:.\n]+(?::|\.)(?:\s|$)/.test(text) ||
    /^[^.\n]+ result received; reviewing output\./.test(text) ||
    /^[^.\n]+ finished with no output\./.test(text)
  );
}
