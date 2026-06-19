import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { WorkflowDetailPage as SharedWorkflowDetailPage } from "@multica/views/workflows";
import { useWorkspaceId } from "@multica/core/hooks";
import { workflowDetailOptions } from "@multica/core/workspace/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function WorkflowDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: workflow } = useQuery(workflowDetailOptions(wsId, id ?? ""));

  useDocumentTitle(workflow?.name ?? "Workflow");

  if (!id) return null;
  return <SharedWorkflowDetailPage workflowId={id} />;
}
