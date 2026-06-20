import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { SOPDetailPage as SharedSOPDetailPage } from "@rimedeck/views/sops";
import { useWorkspaceId } from "@rimedeck/core/hooks";
import { sopDetailOptions } from "@rimedeck/core/workspace/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function SOPDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: sop } = useQuery(sopDetailOptions(wsId, id ?? ""));

  useDocumentTitle(sop?.name ?? "SOP");

  if (!id) return null;
  return <SharedSOPDetailPage sopId={id} />;
}
