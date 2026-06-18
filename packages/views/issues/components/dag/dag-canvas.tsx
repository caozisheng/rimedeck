"use client";

import { useState, useCallback, useRef, type WheelEvent, type PointerEvent, type ReactNode } from "react";

interface DagCanvasProps {
  width: number;
  height: number;
  children: ReactNode;
  onContextMenu?: (e: React.MouseEvent<SVGSVGElement>) => void;
}

const MIN_SCALE = 0.3;
const MAX_SCALE = 2;

/**
 * SVG canvas with zoom (wheel) and pan (drag) support.
 * Children are rendered inside a scaled/translated <g> element.
 */
export function DagCanvas({ width, height, children, onContextMenu }: DagCanvasProps) {
  const [transform, setTransform] = useState({ x: 0, y: 0, scale: 1 });
  const dragRef = useRef<{ startX: number; startY: number; origX: number; origY: number } | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const handleWheel = useCallback((e: WheelEvent<SVGSVGElement>) => {
    e.preventDefault();
    const delta = -e.deltaY * 0.001;
    setTransform((prev) => {
      const newScale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, prev.scale + delta));
      // Zoom toward cursor position
      const rect = (e.target as Element).closest("svg")?.getBoundingClientRect();
      if (!rect) return { ...prev, scale: newScale };
      const cx = e.clientX - rect.left;
      const cy = e.clientY - rect.top;
      const ratio = newScale / prev.scale;
      return {
        x: cx - ratio * (cx - prev.x),
        y: cy - ratio * (cy - prev.y),
        scale: newScale,
      };
    });
  }, []);

  const handlePointerDown = useCallback((e: PointerEvent<SVGSVGElement>) => {
    // Only pan on middle-click or when clicking the background
    if (e.button !== 0 && e.button !== 1) return;
    const target = e.target as Element;
    if (target.closest(".dag-node-card, .dag-node-interactive")) return;
    dragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      origX: transform.x,
      origY: transform.y,
    };
    (e.target as Element).closest("svg")?.setPointerCapture(e.pointerId);
  }, [transform.x, transform.y]);

  const handlePointerMove = useCallback((e: PointerEvent<SVGSVGElement>) => {
    const drag = dragRef.current;
    if (!drag) return;
    const dx = e.clientX - drag.startX;
    const dy = e.clientY - drag.startY;
    setTransform((prev) => ({
      ...prev,
      x: drag.origX + dx,
      y: drag.origY + dy,
    }));
  }, []);

  const handlePointerUp = useCallback(() => {
    dragRef.current = null;
  }, []);

  const resetView = useCallback(() => {
    setTransform({ x: 0, y: 0, scale: 1 });
  }, []);

  const fitToView = useCallback(() => {
    const container = containerRef.current;
    if (!container) return;
    const cw = container.clientWidth;
    const ch = container.clientHeight;
    const scaleX = cw / width;
    const scaleY = ch / height;
    const scale = Math.min(scaleX, scaleY, 1) * 0.9;
    const x = (cw - width * scale) / 2;
    const y = (ch - height * scale) / 2;
    setTransform({ x, y, scale });
  }, [width, height]);

  return (
    <div ref={containerRef} className="relative flex-1 min-h-0 h-full overflow-hidden bg-muted/20">
      {/* Controls */}
      <div className="absolute top-3 right-3 z-10 flex gap-1">
        <button
          onClick={fitToView}
          className="rounded-md bg-card border px-2 py-1 text-xs text-muted-foreground hover:bg-accent"
        >
          Fit
        </button>
        <button
          onClick={resetView}
          className="rounded-md bg-card border px-2 py-1 text-xs text-muted-foreground hover:bg-accent"
        >
          Reset
        </button>
      </div>

      <svg
        width="100%"
        height="100%"
        style={{ display: "block", minHeight: 0 }}
        className="cursor-grab active:cursor-grabbing"
        onWheel={handleWheel}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onContextMenu={onContextMenu}
      >
        <g transform={`translate(${transform.x}, ${transform.y}) scale(${transform.scale})`}>
          {children}
        </g>
      </svg>
    </div>
  );
}
