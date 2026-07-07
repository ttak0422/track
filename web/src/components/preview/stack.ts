// Stacking helpers shared by WikiLink and the floating windows.

// Base stacking order for previews. Each interaction bumps a preview above the other open previews.
export const previewBaseZIndex = 100;
// Hover intent: only open a preview once the pointer rests on a link, so sweeping the cursor down a
// column of links does not flash a popup under every one it crosses.
export const previewOpenDelay = 260;
// Media embeds are already fully visible in the note (unlike a note link, whose target is hidden),
// so their hover preview demands a longer rest — a pointer merely passing over or parking on a large
// image should not feel like an instant popup.
export const mediaPreviewOpenDelay = 700;

let previewStackOrder = 0;

export function nextPreviewStackOrder(): number {
  previewStackOrder += 1;
  return previewStackOrder;
}
