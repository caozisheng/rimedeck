"use client";

import { use } from "react";
import { AgentDetailPage } from "@rimedeck/views/agents";

export default function AgentDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <AgentDetailPage agentId={id} />;
}
