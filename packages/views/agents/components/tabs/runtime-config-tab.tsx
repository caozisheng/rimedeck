"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Loader2, Save } from "lucide-react";
import type { Agent } from "@rimedeck/core/types";
import {
  OPENCLAW_GATEWAY_TOKEN_MASK,
  type OpenclawRoutingMode,
  type OpenclawRuntimeConfig,
  openclawRuntimeConfigEquals,
  parseOpenclawRuntimeConfig,
  serializeOpenclawRuntimeConfig,
} from "@rimedeck/core/agents";
import { Button } from "@rimedeck/ui/components/ui/button";
import { Input } from "@rimedeck/ui/components/ui/input";
import { Label } from "@rimedeck/ui/components/ui/label";
import { Switch } from "@rimedeck/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../../i18n";

interface FormState {
  timeoutMinutes: string;
  mode: OpenclawRoutingMode;
  host: string;
  port: string;
  token: string;
  tls: boolean;
  tokenWasMasked: boolean;
}

function configToForm(raw: unknown, openclaw: OpenclawRuntimeConfig): FormState {
  const masked = openclaw.gateway?.token === OPENCLAW_GATEWAY_TOKEN_MASK;
  return {
    timeoutMinutes: parseExecutionTimeoutMinutes(raw),
    mode: openclaw.mode ?? "local",
    host: openclaw.gateway?.host ?? "",
    port: openclaw.gateway?.port ? String(openclaw.gateway.port) : "",
    token: masked ? "" : (openclaw.gateway?.token ?? ""),
    tls: openclaw.gateway?.tls === true,
    tokenWasMasked: masked,
  };
}

function formToOpenclawConfig(state: FormState): OpenclawRuntimeConfig {
  const cfg: OpenclawRuntimeConfig = { mode: state.mode };
  if (state.mode === "gateway") {
    const gw: NonNullable<OpenclawRuntimeConfig["gateway"]> = {};
    if (state.host.trim() !== "") gw.host = state.host.trim();
    const portNum = Number.parseInt(state.port, 10);
    if (Number.isFinite(portNum) && portNum > 0) gw.port = portNum;
    if (state.tls) gw.tls = true;
    if (state.tokenWasMasked && state.token === "") {
      gw.token = OPENCLAW_GATEWAY_TOKEN_MASK;
    } else if (state.token !== "") {
      gw.token = state.token;
    }
    if (Object.keys(gw).length > 0) cfg.gateway = gw;
  }
  return cfg;
}

function formToRuntimeConfig(
  raw: unknown,
  state: FormState,
  includeOpenclaw: boolean,
): Record<string, unknown> {
  const out = objectRecord(raw);
  const execution = objectRecord(out.execution);
  const timeoutText = state.timeoutMinutes.trim();

  if (timeoutText === "") {
    delete execution.timeout_minutes;
  } else {
    const timeout = Number.parseFloat(timeoutText);
    if (Number.isFinite(timeout) && timeout > 0) {
      execution.timeout_minutes = timeout;
    }
  }
  if (Object.keys(execution).length > 0) {
    out.execution = execution;
  } else {
    delete out.execution;
  }

  if (includeOpenclaw) {
    delete out.mode;
    delete out.gateway;
    Object.assign(out, serializeOpenclawRuntimeConfig(formToOpenclawConfig(state)));
  }

  return out;
}

function parseExecutionTimeoutMinutes(raw: unknown): string {
  const root = objectRecord(raw);
  const execution = objectRecord(root.execution);
  const value = execution.timeout_minutes;
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) return "";
  return String(value);
}

function objectRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  return { ...(value as Record<string, unknown>) };
}

function stableJson(value: unknown): string {
  if (!value || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  const obj = value as Record<string, unknown>;
  return `{${Object.keys(obj)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableJson(obj[key])}`)
    .join(",")}}`;
}

function canonicalizeRuntimeConfig(raw: unknown): Record<string, unknown> {
  const out = objectRecord(raw);
  const execution = objectRecord(out.execution);
  if (Object.keys(execution).length === 0) {
    delete out.execution;
  } else {
    out.execution = execution;
  }

  const openclaw = parseOpenclawRuntimeConfig(out);
  const hasGateway = !!openclaw.gateway && Object.keys(openclaw.gateway).length > 0;
  if ((openclaw.mode ?? "local") === "local" && !hasGateway) {
    delete out.mode;
    delete out.gateway;
  }

  return out;
}

export function RuntimeConfigTab({
  agent,
  runtimeProvider,
  onSave,
  onDirtyChange,
}: {
  agent: Agent;
  runtimeProvider?: string;
  onSave: (updates: { runtime_config: Record<string, unknown> }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const showOpenclaw = runtimeProvider === "openclaw";

  const originalOpenclaw = useMemo<OpenclawRuntimeConfig>(
    () => parseOpenclawRuntimeConfig(agent.runtime_config),
    [agent.runtime_config],
  );
  const originalForm = useMemo(
    () => configToForm(agent.runtime_config, originalOpenclaw),
    [agent.runtime_config, originalOpenclaw],
  );

  const [state, setState] = useState<FormState>(originalForm);
  const [saving, setSaving] = useState(false);

  const previousFormRef = useRef(originalForm);
  useEffect(() => {
    setState((current) =>
      formEquals(current, previousFormRef.current) ? originalForm : current,
    );
    previousFormRef.current = originalForm;
  }, [originalForm]);

  const currentOpenclaw = useMemo(() => formToOpenclawConfig(state), [state]);
  const currentRuntimeConfig = useMemo(
    () => formToRuntimeConfig(agent.runtime_config, state, showOpenclaw),
    [agent.runtime_config, showOpenclaw, state],
  );
  const dirty =
    stableJson(canonicalizeRuntimeConfig(agent.runtime_config)) !==
      stableJson(canonicalizeRuntimeConfig(currentRuntimeConfig)) ||
    (showOpenclaw && !openclawRuntimeConfigEquals(originalOpenclaw, currentOpenclaw));

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const portValid = state.port === "" || /^\d+$/.test(state.port);
  const timeoutValue = Number.parseFloat(state.timeoutMinutes);
  const timeoutValid =
    state.timeoutMinutes.trim() === "" ||
    (/^\d+(\.\d+)?$/.test(state.timeoutMinutes.trim()) &&
      Number.isFinite(timeoutValue) &&
      timeoutValue > 0 &&
      timeoutValue <= 24 * 60);
  const canSave = portValid && timeoutValid && !saving;
  const isGateway = state.mode === "gateway";

  const handleSave = async () => {
    if (!canSave) return;
    setSaving(true);
    try {
      await onSave({ runtime_config: currentRuntimeConfig });
      toast.success(t(($) => $.tab_body.runtime_config.saved_toast));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.runtime_config.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex h-full flex-col space-y-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.runtime_config.execution_intro)}
      </p>

      <fieldset className="space-y-2 rounded-md border p-3">
        <legend className="px-1 text-xs font-medium">
          {t(($) => $.tab_body.runtime_config.execution_legend)}
        </legend>
        <div className="max-w-xs space-y-1.5">
          <Label htmlFor="agent-timeout-minutes" className="text-xs">
            {t(($) => $.tab_body.runtime_config.timeout_label)}
          </Label>
          <div className="flex items-center gap-2">
            <Input
              id="agent-timeout-minutes"
              value={state.timeoutMinutes}
              onChange={(e) =>
                setState((s) => ({ ...s, timeoutMinutes: e.target.value }))
              }
              placeholder="Default"
              inputMode="decimal"
              aria-invalid={!timeoutValid || undefined}
              className="font-mono text-xs"
            />
            <span className="shrink-0 text-xs text-muted-foreground">
              {t(($) => $.tab_body.runtime_config.timeout_unit)}
            </span>
          </div>
          {!timeoutValid && (
            <p className="text-xs text-destructive">
              {t(($) => $.tab_body.runtime_config.timeout_invalid)}
            </p>
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.runtime_config.timeout_hint)}
        </p>
      </fieldset>

      {showOpenclaw && (
        <>
          <fieldset className="space-y-2">
            <Label className="text-xs font-medium">
              {t(($) => $.tab_body.runtime_config.mode_label)}
            </Label>
            <div className="flex gap-2">
              {(["local", "gateway"] as const).map((mode) => (
                <button
                  key={mode}
                  type="button"
                  onClick={() =>
                    setState((s) =>
                      s.mode === mode
                        ? s
                        : { ...s, mode, tokenWasMasked: false },
                    )
                  }
                  className={`rounded-md border px-3 py-1.5 text-xs ${
                    state.mode === mode
                      ? "border-foreground bg-foreground text-background"
                      : "border-border bg-background text-foreground hover:bg-muted"
                  }`}
                >
                  {t(($) => $.tab_body.runtime_config[`mode_${mode}`])}
                </button>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              {isGateway
                ? t(($) => $.tab_body.runtime_config.mode_gateway_hint)
                : t(($) => $.tab_body.runtime_config.mode_local_hint)}
            </p>
          </fieldset>

          <fieldset
            className={`space-y-3 rounded-md border p-3 ${isGateway ? "" : "opacity-50"}`}
            disabled={!isGateway}
          >
            <legend className="px-1 text-xs font-medium">
              {t(($) => $.tab_body.runtime_config.gateway_legend)}
            </legend>

            <div className="space-y-1.5">
              <Label htmlFor="openclaw-gw-host" className="text-xs">
                {t(($) => $.tab_body.runtime_config.host_label)}
              </Label>
              <Input
                id="openclaw-gw-host"
                value={state.host}
                onChange={(e) =>
                  setState((s) => ({ ...s, host: e.target.value }))
                }
                placeholder={t(($) => $.tab_body.runtime_config.host_placeholder)}
                className="font-mono text-xs"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="openclaw-gw-port" className="text-xs">
                {t(($) => $.tab_body.runtime_config.port_label)}
              </Label>
              <Input
                id="openclaw-gw-port"
                value={state.port}
                onChange={(e) =>
                  setState((s) => ({ ...s, port: e.target.value }))
                }
                placeholder="18789"
                inputMode="numeric"
                aria-invalid={!portValid || undefined}
                className="font-mono text-xs"
              />
              {!portValid && (
                <p className="text-xs text-destructive">
                  {t(($) => $.tab_body.runtime_config.port_invalid)}
                </p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="openclaw-gw-token" className="text-xs">
                {t(($) => $.tab_body.runtime_config.token_label)}
              </Label>
              <Input
                id="openclaw-gw-token"
                type="password"
                value={state.token}
                onChange={(e) =>
                  setState((s) => ({
                    ...s,
                    token: e.target.value,
                    tokenWasMasked: false,
                  }))
                }
                placeholder={
                  state.tokenWasMasked
                    ? t(($) => $.tab_body.runtime_config.token_masked_placeholder)
                    : t(($) => $.tab_body.runtime_config.token_placeholder)
                }
                autoComplete="off"
                className="font-mono text-xs"
              />
            </div>

            <div className="flex items-center justify-between gap-2 pt-1">
              <div>
                <Label htmlFor="openclaw-gw-tls" className="text-xs">
                  {t(($) => $.tab_body.runtime_config.tls_label)}
                </Label>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.tab_body.runtime_config.tls_hint)}
                </p>
              </div>
              <Switch
                id="openclaw-gw-tls"
                checked={state.tls}
                disabled={!isGateway}
                onCheckedChange={(checked: boolean) =>
                  setState((s) => ({ ...s, tls: checked }))
                }
              />
            </div>
          </fieldset>
        </>
      )}

      <div className="flex items-center justify-end gap-3 pt-2">
        {dirty && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button onClick={handleSave} disabled={!dirty || !canSave} size="sm">
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}

function formEquals(a: FormState, b: FormState): boolean {
  return (
    a.timeoutMinutes === b.timeoutMinutes &&
    a.mode === b.mode &&
    a.host === b.host &&
    a.port === b.port &&
    a.token === b.token &&
    a.tls === b.tls &&
    a.tokenWasMasked === b.tokenWasMasked
  );
}
