"use client";

import { IssuesPage } from "@rimedeck/views/issues/components";
import { ErrorBoundary } from "@rimedeck/ui/components/common/error-boundary";

export default function Page() {
  return (
    <ErrorBoundary>
      <IssuesPage />
    </ErrorBoundary>
  );
}
