import { IconMicrophone } from "@tabler/icons-react";

// One mic, always: recording state reads from the button's fill and the
// status line, never from swapping the glyph to a muted mic.
export function VoiceIcon() {
  return <IconMicrophone size={18} stroke={1.5} aria-hidden="true" />;
}
