/* eslint-disable @typescript-eslint/no-unused-vars */

export function identify(_userId: string, _traits?: Record<string, unknown>) {}
export function resetAnalytics() {}
export function captureSignupSource() {}
export function captureEvent(_name: string, _props?: Record<string, unknown>) {}
export function setPersonProperties(_props: Record<string, unknown>) {}
export function captureDownloadIntent(_source: string) {}
export function captureFeedbackOpened(_source?: string, _wsId?: string) {}
export function capturePageview(_url: string) {}
export function captureDownloadPageViewed(_props?: Record<string, unknown>) {}
export function captureDownloadInitiated(_payload: DownloadInitiatedPayload) {}
export function initAnalytics() {}

export interface DownloadInitiatedPayload {
  [key: string]: unknown;
}
