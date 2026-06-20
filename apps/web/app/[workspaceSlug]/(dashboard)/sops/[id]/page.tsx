"use client";

import { use } from "react";
import { SOPDetailPage } from "@rimedeck/views/sops";

export default function SOPDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <SOPDetailPage sopId={id} />;
}
