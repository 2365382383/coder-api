export type CanvasImageSettingsMode = "dimensions" | "aspect_ratio_resolution" | string;

export type NormalizedCanvasImageConfig = {
  size: string;
  quality: string;
  aspect_ratio: string;
  resolution: string;
  output_format?: string;
};

export function imageSettingsPatch(
  normalized: NormalizedCanvasImageConfig,
  sizeMode: CanvasImageSettingsMode,
): Record<string, string> {
  const patch: Record<string, string> = {
    quality: normalized.quality,
    imageAspectRatio: normalized.aspect_ratio,
    imageResolution: normalized.resolution,
    imageOutputFormat: normalized.output_format || "",
  };
  if (sizeMode === "dimensions") patch.size = normalized.size;
  return patch;
}
