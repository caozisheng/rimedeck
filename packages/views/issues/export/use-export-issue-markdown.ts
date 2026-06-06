"use client";

import { useState, useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Issue, AgentTask } from "@multica/core/types";
import { api } from "@multica/core/api";
import { issueKeys, issueTimelineOptions } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { buildTimeline } from "../../common/task-transcript/build-timeline";
import type { TimelineItem } from "../../common/task-transcript/build-timeline";
import { formatActivity, type ActivityT } from "../utils/format-activity";
import { buildIssueMarkdown } from "./build-issue-markdown";
import { downloadTextFile } from "./download-file";
import { useT } from "../../i18n";

const MAX_TRANSCRIPT_TASKS = 10;

export function useExportIssueMarkdown(issue: Issue | null) {
  const [isExporting, setIsExporting] = useState(false);
  const queryClient = useQueryClient();
  const { getActorName } = useActorName();
  const { t } = useT("issues");

  const exportMarkdown = useCallback(async () => {
    if (!issue || isExporting) return;
    setIsExporting(true);

    try {
      // Fetch timeline + tasks in parallel, prefer cache
      const [timeline, tasks] = await Promise.all([
        queryClient.fetchQuery(issueTimelineOptions(issue.id)),
        queryClient.fetchQuery({
          queryKey: issueKeys.tasks(issue.id),
          queryFn: () => api.listTasksByIssue(issue.id),
          staleTime: 30_000,
        }),
      ]);

      // Fetch transcripts for completed/failed tasks
      const terminalTasks = tasks
        .filter((task: AgentTask) => task.status === "completed" || task.status === "failed")
        .sort((a: AgentTask, b: AgentTask) =>
          new Date(b.completed_at ?? b.created_at).getTime() -
          new Date(a.completed_at ?? a.created_at).getTime(),
        )
        .slice(0, MAX_TRANSCRIPT_TASKS);

      const taskTranscripts = new Map<string, { task: AgentTask; items: TimelineItem[] }>();

      if (terminalTasks.length > 0) {
        const messageResults = await Promise.all(
          terminalTasks.map((task: AgentTask) =>
            api.listTaskMessages(task.id).catch(() => []),
          ),
        );

        for (let i = 0; i < terminalTasks.length; i++) {
          const msgs = messageResults[i] ?? [];
          const task = terminalTasks[i];
          if (task && msgs.length > 0) {
            taskTranscripts.set(task.id, {
              task,
              items: buildTimeline(msgs),
            });
          }
        }
      }

      const activityT = t as unknown as ActivityT;
      const markdown = buildIssueMarkdown({
        issue,
        timeline,
        taskTranscripts,
        getActorName,
        formatActivityEntry: (entry) =>
          formatActivity(entry, activityT, getActorName),
      });

      const slug = issue.title
        .toLowerCase()
        .replace(/[^a-z0-9一-鿿가-힯぀-ゟ゠-ヿ]+/g, "-")
        .slice(0, 50)
        .replace(/-$/, "");
      const filename = `${issue.identifier}-${slug}.md`;

      downloadTextFile(filename, markdown);
      toast.success(t(($) => $.detail.export_success));
    } catch {
      toast.error(t(($) => $.detail.export_failed));
    } finally {
      setIsExporting(false);
    }
  }, [issue, isExporting, queryClient, getActorName, t]);

  return { exportMarkdown, isExporting };
}
