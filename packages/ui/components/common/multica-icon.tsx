import { useState, useEffect } from "react";
import { cn } from "../../lib/utils";

interface MulticaIconProps extends React.ComponentProps<"span"> {
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
      {/* Back card */}
      <rect x="22" y="10" width="56" height="68" rx="8" stroke="currentColor" strokeWidth="5" fill="none" />
      {/* Middle card */}
      <rect x="14" y="20" width="56" height="68" rx="8" stroke="currentColor" strokeWidth="5" fill="none" />
      {/* Front card */}
      <rect x="30" y="30" width="56" height="68" rx="8" stroke="currentColor" strokeWidth="4" fill="currentColor" fillOpacity="0.1" />
      {/* Letter R */}
      <text
        x="58"
        y="72"
        textAnchor="middle"
        dominantBaseline="central"
        fill="currentColor"
        fontFamily="system-ui, sans-serif"
        fontWeight="700"
        fontSize="34"
      >
        R
      </text>
      {/* Arrow curving up-right */}
      <path
        d="M20 88 C20 78, 28 72, 38 72"
        stroke="currentColor"
        strokeWidth="4.5"
        strokeLinecap="round"
        fill="none"
      />
      <path
        d="M34 66 L40 72 L34 78"
        stroke="currentColor"
        strokeWidth="4.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        fill="none"
      />
    </svg>
  );
}

export function MulticaIcon({
  className,
  animate = false,
  noSpin = false,
  bordered = false,
  size = "sm",
  ...props
}: MulticaIconProps) {
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
