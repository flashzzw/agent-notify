# Notification icons

Windows uses these embedded PNGs for the small notification source icon:

- `codex.png`: blue/purple terminal icon, 96 × 96.
- `claude.png`: orange Claude mark, 96 × 96.
- `omo.png`: 128 × 128 PNG rendered from the user-provided
  [`assets/omo-icon-light.svg`](../../../assets/omo-icon-light.svg).

Codex and Claude preserve the previous local notifier's artwork. To regenerate
the OMO PNG from the repository root:

```bash
uvx --from CairoSVG==2.8.2 cairosvg assets/omo-icon-light.svg --output-width 128 --output-height 128 -o internal/notify/icons/omo.png
```

The generated PNG is checked in, so ordinary builds need no SVG renderer.

Go embeds these assets in the executable. Windows extracts them into
`%LOCALAPPDATA%\AgentNotify\icons` when needed, so both native and WSL installs
can show logos without downloading images or relying on a Linux file path.
Toast bodies contain text only; no large `appLogoOverride` image is added.
