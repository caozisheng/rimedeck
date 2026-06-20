"use client";

import type { Issue } from "@rimedeck/core/types";
import { StatusDonut } from "./analytics/status-donut";
import { PriorityBars } from "./analytics/priority-bars";
import { WorkloadBars } from "./analytics/workload-bars";
import { TrendChart } from "./analytics/trend-chart";
import { useT } from "../../i18n";

export function AnalyticsView({ issues }: { issues: Issue[] }) {
  const { t } = useT("issues");

  return (
    <div className="flex-1 min-h-0 overflow-y-auto p-4">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title={t(($) => $.analytics.status_title)}>
          <StatusDonut issues={issues} />
        </Card>

        <Card title={t(($) => $.analytics.priority_title)}>
          <PriorityBars issues={issues} />
        </Card>

        <Card title={t(($) => $.analytics.workload_title)}>
          <WorkloadBars issues={issues} />
        </Card>

        <Card title={t(($) => $.analytics.trend_title)}>
          <TrendChart issues={issues} />
        </Card>
      </div>
    </div>
  );
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <h3 className="mb-4 text-sm font-medium text-muted-foreground">{title}</h3>
      {children}
    </div>
  );
}
