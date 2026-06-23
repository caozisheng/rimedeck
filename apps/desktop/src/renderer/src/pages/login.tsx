import { LoginPage } from "@rimedeck/views/auth";
import { DragStrip } from "@rimedeck/views/platform";
import { RimeDeckIcon } from "@rimedeck/ui/components/common/rimedeck-icon";

export function DesktopLoginPage() {
  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        logo={<RimeDeckIcon bordered size="lg" />}
        skipCode
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
      />
    </div>
  );
}
