"use client";

import { use } from "react";
import { SkillDetailPage } from "@rimedeck/views/skills";

export default function SkillDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <SkillDetailPage skillId={id} />;
}
