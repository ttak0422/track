// The rail's one icon family: tabler outline glyphs, re-exported here so every component imports
// its icons from one place (tree-shaking keeps only the named glyphs a bundle actually uses).
// The track logo and the filled rail mark are custom and stay in Logo.tsx — they are not tabler.
import { IconSettings } from "@tabler/icons-react";
import type { TablerIcon } from "@tabler/icons-react";

export { IconSettings };

interface RailIconProps {
  Icon: TablerIcon;
  size?: number;
  stroke?: number;
  className?: string;
}

// Draws any tabler glyph the way design.md fixes the family: a 24-unit viewBox at 20px, fill none,
// round caps and joins. Tabler's default stroke-width is 2, so 1.5 is always set explicitly.
// Callers may override size/stroke/className for variants; everything else stays fixed.
export function RailIcon({ Icon, size = 20, stroke = 1.5, className = "rail-icon-svg" }: RailIconProps) {
  return (
    <Icon
      size={size}
      stroke={stroke}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
      fill="none"
    />
  );
}
