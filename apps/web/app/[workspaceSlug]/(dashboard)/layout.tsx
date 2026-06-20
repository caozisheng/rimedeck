"use client";

import { DashboardLayout } from "@rimedeck/views/layout";
import { RimeDeckIcon } from "@rimedeck/ui/components/common/rimedeck-icon";
import { SearchCommand, SearchTrigger } from "@rimedeck/views/search";
import { ChatFab, ChatWindow } from "@rimedeck/views/chat";
import { WebNotificationBridge } from "@/components/web-notification-bridge";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<RimeDeckIcon className="size-6" />}
      searchSlot={<SearchTrigger />}
      extra={
        <>
          <SearchCommand />
          <ChatWindow />
          <ChatFab />
          <WebNotificationBridge />
        </>
      }
    >
      {children}
    </DashboardLayout>
  );
}
