import { useState, useEffect } from "react";
import { cn } from "../../lib/utils";

interface RimeDeckIconProps extends React.ComponentProps<"span"> {
  animate?: boolean;
  noSpin?: boolean;
  bordered?: boolean;
  size?: "sm" | "md" | "lg";
}

const borderedSizes = {
  sm: { wrapper: "p-1.5", icon: "size-3.5" },
  md: { wrapper: "p-2", icon: "size-4" },
  lg: { wrapper: "p-2.5", icon: "size-5" },
};

function RimedeckSvg({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 100 100"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      {/* Main shape: stacked cards + R */}
      <path
        fill="currentColor"
        d="M21.5,37.9 L22.5,42.0 L42.8,53.7 L66.8,36.3 L61.3,28.7 L69.3,29.3 L70.1,50.2 L58.8,58.6 L58.8,68.0 L66.8,79.5 L78.7,79.5 L67.0,62.7 L79.1,52.9 L78.7,19.9 L46.5,19.9 Z"
      />
      {/* Top card */}
      <path
        fill="currentColor"
        d="M63.5,34.6 L42.4,49.8 L25.0,39.6 L46.9,23.8 Z"
      />
      {/* Middle card */}
      <path
        fill="currentColor"
        d="M67.4,40.8 L42.4,57.6 L21.1,45.1 L21.1,54.1 L42.4,66.6 L67.0,49.0 Z"
      />
      {/* Bottom card */}
      <path
        fill="currentColor"
        d="M20.9,58.8 L21.3,67.4 L41.8,79.5 L55.9,69.9 L55.5,61.1 L42.2,70.7 Z"
      />
    </svg>
  );
}

export function RimeDeckIcon({
  className,
  animate = false,
  noSpin = false,
  bordered = false,
  size = "sm",
  ...props
}: RimeDeckIconProps) {
  const [entranceDone, setEntranceDone] = useState(!animate);

  useEffect(() => {
    if (!animate) return;
    const timer = setTimeout(() => setEntranceDone(true), 600);
    return () => clearTimeout(timer);
  }, [animate]);

  if (bordered) {
    const sizeConfig = borderedSizes[size];
    return (
      <span
        className={cn(
          "inline-flex items-center justify-center border border-border rounded-md",
          sizeConfig.wrapper,
          className
        )}
        aria-hidden="true"
        {...props}
      >
        <span
          className={cn(
            "block",
            sizeConfig.icon,
            !entranceDone && "animate-entrance-spin",
            entranceDone && !noSpin && "hover:animate-spin"
          )}
        >
          <RimedeckSvg className="size-full" />
        </span>
      </span>
    );
  }

  return (
    <span
      className={cn(
        "inline-block size-[1em]",
        !entranceDone && "animate-entrance-spin",
        entranceDone && !noSpin && "hover:animate-spin",
        className
      )}
      aria-hidden="true"
      {...props}
    >
      <RimedeckSvg className="size-full" />
    </span>
  );
}
