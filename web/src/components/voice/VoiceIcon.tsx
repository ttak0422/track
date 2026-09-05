import { IconMicrophone, IconMicrophoneOff } from "@tabler/icons-react";

export function VoiceIcon({ listening }: { listening: boolean }) {
  const Icon = listening ? IconMicrophoneOff : IconMicrophone;
  return <Icon size={18} stroke={1.5} aria-hidden="true" />;
}
