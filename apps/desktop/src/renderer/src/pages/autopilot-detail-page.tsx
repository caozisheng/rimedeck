import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AutopilotDetailPage as AutopilotDetail } from "@rimedeck/views/autopilots/components";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { autopilotDetailOptions } from "@rimedeck/core/autopilots/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function AutopilotDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data } = useQuery(autopilotDetailOptions(wsId, id!));

  useDocumentTitle(data ? `⚡ ${data.autopilot.title}` : "Autopilot");

  if (!id) return null;
  return <AutopilotDetail autopilotId={id} />;
}
