"use client";

import { useEffect, useState } from "react";
import { Bot, Clock3 } from "lucide-react";
import type { AgentTask } from "@rimedeck/core/types";
import { Card } from "@rimedeck/ui/components/ui/card";
import { cn } from "@rimedeck/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { TaskTimelinePreview } from "../../common/task-transcript";
import { useActorName } from "@rimedeck/core/workspace/hooks";
import type { TFunction } from "i18next";
import { useT } from "../../i18n";

interface AgentWorkCardProps {
  task: AgentTask;
  className?: string;
}

export function AgentWorkCard({ task, className }: AgentWorkCardProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const elapsed = formatDuration(
    task.started_at ?? task.dispatched_at ?? task.created_at,
    now,
  );
  const statusLabel = getStatusLabel(task.status, t);
  const showTranscript =
    task.status !== "queued" && task.status !== "waiting_local_directory";
  const waitingLabel = getWaitingLabel(task.status, t);

  return (
    <Card
      className={cn(
        "!gap-0 !py-0 overflow-clip border-info/30 bg-info/5",
        className,
      )}
      data-testid="agent-work-card"
    >
      <div className="flex items-center gap-2.5 px-4 py-3">
        <ActorAvatar
          actorType="agent"
          actorId={task.agent_id}
          size={24}
          enableHoverCard
          showStatusDot
        />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-sm font-medium">
              {getActorName("agent", task.agent_id)}
            </span>
            <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-info/10 px-1.5 py-0.5 text-[11px] font-medium text-info">
              <Bot className="h-3 w-3" />
              {t(($) => $.execution_log.status_running)}
            </span>
          </div>
          <div className="mt-0.5 flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
            <span className="truncate">{statusLabel}</span>
            <span className="inline-flex shrink-0 items-center gap-1 tabular-nums">
              <Clock3 className="h-3 w-3" />
              {elapsed}
            </span>
          </div>
        </div>
      </div>

      {showTranscript ? (
        <TaskTimelinePreview
          taskId={task.id}
          className="mx-4 mb-3 border-info/20 bg-background/60"
          maxItems={10}
          emptyFallback={
            <div className="mx-4 mb-3 rounded-md border border-dashed border-info/20 bg-background/60 p-2 text-xs text-muted-foreground">
              {waitingLabel}
            </div>
          }
        />
      ) : (
        <div className="mx-4 mb-3 rounded-md border border-dashed border-info/20 bg-background/60 p-2 text-xs text-muted-foreground">
          {waitingLabel}
        </div>
      )}
    </Card>
  );
}

function getWaitingLabel(
  status: AgentTask["status"],
  t: TFunction<"issues">,
): string {
  switch (status) {
    case "queued":
      return t(($) => $.execution_log.status_queued);
    case "dispatched":
      return t(($) => $.execution_log.status_dispatched);
    case "waiting_local_directory":
      return t(($) => $.execution_log.status_waiting_local_directory);
    case "running":
      return t(($) => $.execution_log.status_running);
    default:
      return getStatusLabel(status, t);
  }
}

function getStatusLabel(
  status: AgentTask["status"],
  t: TFunction<"issues">,
): string {
  switch (status) {
    case "queued":
      return t(($) => $.execution_log.status_queued);
    case "dispatched":
      return t(($) => $.execution_log.status_dispatched);
    case "waiting_local_directory":
      return t(($) => $.execution_log.status_waiting_local_directory);
    case "running":
      return t(($) => $.execution_log.status_running);
    case "completed":
      return t(($) => $.execution_log.status_completed);
    case "failed":
      return t(($) => $.execution_log.status_failed);
    case "cancelled":
      return t(($) => $.execution_log.status_cancelled);
  }
}
