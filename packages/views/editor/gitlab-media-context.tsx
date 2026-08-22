"use client";

import { createContext, use, useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "@rimedeck/core/api";

interface GitlabMediaScope {
  issueId: string;
  instanceUrl: string;
  apiOrigin: string;
}

const GitlabMediaContext = createContext<GitlabMediaScope | null>(null);

function normalizeGitlabMediaSource(rawUrl: string, scope: GitlabMediaScope): string | null {
  if (rawUrl.startsWith("/uploads/")) return rawUrl;
  try {
    const parsed = new URL(rawUrl);
    const isGitlabOrigin = parsed.origin === new URL(scope.instanceUrl).origin;
    const isLocalApiOrigin = parsed.origin === scope.apiOrigin;
    if ((isGitlabOrigin || isLocalApiOrigin) && parsed.pathname.startsWith("/uploads/")) {
      return parsed.pathname + parsed.search;
    }
  } catch {
    return null;
  }
  return null;
}

export function GitlabMediaProvider({ issueId, instanceUrl, children }: Omit<GitlabMediaScope, "apiOrigin"> & { children: ReactNode }) {
  const value = useMemo(() => ({
    issueId,
    instanceUrl: instanceUrl.replace(/\/+$/, ""),
    apiOrigin: new URL(api.getBaseUrl?.() || window.location.origin).origin,
  }), [issueId, instanceUrl]);
  return <GitlabMediaContext.Provider value={value}>{children}</GitlabMediaContext.Provider>;
}

export function useGitlabMediaUrl(rawUrl: string): string {
  const scope = use(GitlabMediaContext);
  const [objectUrl, setObjectUrl] = useState("");
  const sourceUrl = useMemo(() => scope ? normalizeGitlabMediaSource(rawUrl, scope) : null, [rawUrl, scope]);

  useEffect(() => {
    if (!scope || !sourceUrl) {
      setObjectUrl("");
      return;
    }
    let active = true;
    let created = "";
    api.getGitlabMedia(scope.issueId, sourceUrl).then((blob) => {
      if (!active) return;
      created = URL.createObjectURL(blob);
      setObjectUrl(created);
    }).catch(() => {
      if (active) setObjectUrl("");
    });
    return () => {
      active = false;
      if (created) URL.revokeObjectURL(created);
    };
  }, [scope, sourceUrl]);

  return sourceUrl ? objectUrl : rawUrl;
}
