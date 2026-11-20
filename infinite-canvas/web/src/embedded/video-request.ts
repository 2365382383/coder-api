import { dataUrlToFile } from "../lib/image-utils";
import { computeVideoSize, inferVideoRatio } from "../lib/media-size";
import { imageToDataUrl } from "../services/image-storage";
import { videoAdapterForModel, type AiConfig } from "./config";
import type { ReferenceImage } from "../types/image";
import i18n from "./messages";
import { canvasData } from "./bridge";

async function publicReferenceURL(image: ReferenceImage, signal?: AbortSignal): Promise<string> {
  const url = typeof image.url === "string" ? image.url.trim() : "";
  if (/^https?:\/\//i.test(url)) return url;
  const storageKey = typeof image.storageKey === "string" ? image.storageKey.trim() : "";
  if (!storageKey) throw new Error(i18n.t("embedded.videoPublicReferenceRequired"));
  const published = await canvasData<{ url?: unknown }>("assets.publish_image", { id: storageKey }, signal);
  const publicURL = typeof published?.url === "string" ? published.url.trim() : "";
  if (!/^https:\/\//i.test(publicURL)) throw new Error(i18n.t("embedded.videoPublicReferenceRequired"));
  return publicURL;
}

export async function canvasVideoBody(config: AiConfig, selection: string, prompt: string, references: ReferenceImage[], options?: { signal?: AbortSignal; videos?: unknown[]; audios?: unknown[] }): Promise<FormData | string> {
  const adapter = videoAdapterForModel(selection);
  if (adapter === "unsupported") throw new Error(i18n.t("embedded.videoUnsupported"));
  if (options?.videos?.length || options?.audios?.length) throw new Error(i18n.t("embedded.videoMediaUnsupported"));
  const seconds = Number(config.videoSeconds);
  if (!Number.isInteger(seconds) || seconds < 1 || seconds > 3600) throw new Error(i18n.t("apiErrors.videoTaskCreateFailed"));
  if (adapter === "sora" && ![4, 8, 12].includes(seconds)) throw new Error(i18n.t("embedded.soraDuration"));
  const ratio = inferVideoRatio(config.size);
  if (adapter === "doubao") {
    if (config.videoMode !== "reference" && references.length > 2) throw new Error(i18n.t("embedded.videoFramesLimit"));
    const content = await Promise.all(references.map(async (image, index) => {
      let role = "reference_image";
      if (config.videoMode !== "reference") role = index === 0 ? "first_frame" : "last_frame";
      return { type: "image_url", image_url: { url: await imageToDataUrl(image, options) }, role };
    }));
    let outputRatio = ratio === "auto" ? "16:9" : ratio;
    if (references.length && config.videoMode !== "reference") outputRatio = "adaptive";
    return JSON.stringify({ model: config.model, prompt, seconds, metadata: {
      content, ratio: outputRatio,
      resolution: `${(config.vquality || "720").replace(/p$/, "")}p`,
      generate_audio: config.videoGenerateAudio !== "false", watermark: config.videoWatermark === "true",
    } });
  }
  if (adapter === "sora") {
    const size = /^\d+x\d+$/.test(config.size) ? config.size : computeVideoSize(config.vquality || "720", ratio === "auto" ? "16:9" : ratio);
    const mode = config.videoMode === "reference" ? "reference" : "frames";
    const files = await Promise.all(references.map(async (image) => dataUrlToFile({ ...image, dataUrl: await imageToDataUrl(image, options) })));
    const form = new FormData();
    form.set("model", config.model);
    form.set("prompt", prompt);
    form.set("seconds", String(seconds));
    form.set("size", size);
    form.set("generate_audio", String(config.videoGenerateAudio !== "false"));
    form.set("watermark", String(config.videoWatermark === "true"));
    form.set("mode", mode);
    if (mode === "frames") {
      if (files[0]) form.append("first_frame", files[0], "first.png");
      if (files[1]) form.append("last_frame", files[1], "last.png");
    } else {
      files.forEach((file) => form.append("image[]", file, "ref.png"));
    }
    return form;
  }
  const payload: Record<string, unknown> = {
    model: config.model,
    prompt,
    duration: seconds,
    resolution: `${(config.vquality || "720").replace(/p$/, "")}p`,
    aspect_ratio: ratio === "auto" ? "16:9" : ratio,
  };
  if (references.length === 1) payload.input_reference = await publicReferenceURL(references[0], options?.signal);
  else if (references.length > 1) {
    const publicURLs: string[] = [];
    for (const image of references) publicURLs.push(await publicReferenceURL(image, options?.signal));
    payload.reference_image_urls = publicURLs;
  }
  return JSON.stringify(payload);
}
