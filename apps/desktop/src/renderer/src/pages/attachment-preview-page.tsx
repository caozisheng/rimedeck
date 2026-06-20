import { useParams, useSearchParams } from "react-router-dom";
import { AttachmentPreviewPage } from "@rimedeck/views/attachments";
import { ErrorBoundary } from "@rimedeck/ui/components/common/error-boundary";

export function AttachmentPreviewRoute() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const filename = searchParams.get("name") ?? undefined;

  if (!id) return null;
  return (
    <ErrorBoundary resetKeys={[id]}>
      <AttachmentPreviewPage attachmentId={id} filename={filename} />
    </ErrorBoundary>
  );
}
