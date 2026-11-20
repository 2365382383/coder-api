import { describe, expect, test } from "bun:test";

import { imageSettingsPatch } from "../web/src/embedded/image-settings-values";

const normalized = {
  size: "",
  quality: "auto",
  aspect_ratio: "1:1",
  resolution: "1K",
  output_format: "png",
};

describe("embedded image settings normalization", () => {
  test("does not write the dimensions-only size field for ratio/resolution models", () => {
    expect(imageSettingsPatch(normalized, "aspect_ratio_resolution")).toEqual({
      quality: "auto",
      imageAspectRatio: "1:1",
      imageResolution: "1K",
      imageOutputFormat: "png",
    });
  });

  test("keeps size for dimensions models", () => {
    expect(imageSettingsPatch({ ...normalized, size: "1024x1024" }, "dimensions")).toEqual({
      quality: "auto",
      imageAspectRatio: "1:1",
      imageResolution: "1K",
      imageOutputFormat: "png",
      size: "1024x1024",
    });
  });
});
