// The rail's one icon family: tabler outline glyphs, re-exported here so every component imports
// its icons from one place (tree-shaking keeps only the named glyphs a bundle actually uses).
// The track logo and the filled rail mark are custom and stay in Logo.tsx — they are not tabler.
import {
  IconAffiliate,
  IconArrowLeft,
  IconArticle,
  IconBrandX,
  IconCalendar,
  IconCheck,
  IconChecklist,
  IconChevronDown,
  IconChevronLeft,
  IconChevronRight,
  IconChevronUp,
  IconCirclePlus,
  IconColumns2,
  IconCopy,
  IconExternalLink,
  IconEye,
  IconEyeOff,
  IconFileText,
  IconHistory,
  IconLink,
  IconMaximize,
  IconMinus,
  IconNotebook,
  IconPencil,
  IconPhotoOff,
  IconPictureInPicture,
  IconPin,
  IconPlus,
  IconRotate2,
  IconSearch,
  IconSettings,
  IconSitemap,
  IconX,
} from "@tabler/icons-react";
import type { TablerIcon } from "@tabler/icons-react";

export {
  IconAffiliate,
  IconArrowLeft,
  IconArticle,
  IconBrandX,
  IconCalendar,
  IconCheck,
  IconChecklist,
  IconChevronDown,
  IconChevronLeft,
  IconChevronRight,
  IconChevronUp,
  IconCirclePlus,
  IconColumns2,
  IconCopy,
  IconExternalLink,
  IconEye,
  IconEyeOff,
  IconFileText,
  IconHistory,
  IconLink,
  IconMaximize,
  IconMinus,
  IconNotebook,
  IconPencil,
  IconPhotoOff,
  IconPictureInPicture,
  IconPin,
  IconPlus,
  IconRotate2,
  IconSearch,
  IconSettings,
  IconSitemap,
  IconX,
};

type RailIconProps = {
  Icon: TablerIcon;
  size?: number;
  stroke?: number;
  className?: string;
} & Record<string, unknown>;

// Draws any tabler glyph the way design.md fixes the family: a 24-unit viewBox at 20px, fill none,
// round caps and joins. Tabler's default stroke-width is 2, so 1.5 is always set explicitly.
// Callers may override size/stroke/className for variants; everything else stays fixed. Extra props
// (data-state, data-mode, …) ride through to the svg root.
export function RailIcon({ Icon, size = 20, stroke = 1.5, className = "rail-icon-svg", ...rest }: RailIconProps) {
  return (
    <Icon
      size={size}
      stroke={stroke}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
      fill="none"
      {...rest}
    />
  );
}
