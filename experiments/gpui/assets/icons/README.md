# Native rail assets

The outline paths are copied unchanged from `@tabler/icons-react` **3.46.0**,
the version pinned by `web/package-lock.json`. The SVG wrapper matches
`web/src/components/icons.tsx`: 24-unit viewBox, no fill, 1.5-unit stroke,
round caps/joins, displayed at 20 logical pixels. See `TABLER-LICENSE`.

`Brand.svg` reuses the geometry from `web/public/track-icon.svg`. Its letter
is an alpha cutout because GPUI tints SVG alpha masks rather than preserving
foreground/background colors. The native rail applies its current ink color.
All assets are embedded in the binary; no runtime filesystem, font or network
lookup is used. These are application-owned assets, never note-provided SVGs.
