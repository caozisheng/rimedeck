"use client";

import { use } from "react";
import { WorkflowDetailPage } from "@multica/views/workflows";

export default function WorkflowDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <WorkflowDetailPage workflowId={id} />;
}
