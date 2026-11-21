import { expect, mock, test } from "bun:test";

Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined },
});

const publishAsset = mock(async (operation: string) => ({
    url: operation === "assets.publish_video" ? "https://coderapi.vip/media-upload/reference" : "https://coderapi.vip/image-upload/reference",
}));
mock.module("../src/embedded/config", () => ({
    videoAdapterForModel: (selection: string) => selection,
    canvasHeaders: () => ({}),
    canvasImageCapabilities: () => undefined,
    resolveModelRequestConfig: (config: Record<string, unknown>, selection: string) => ({
        ...config,
        model: selection.split("::").at(-1),
        canvasGroup: "image",
    }),
}));
mock.module("../src/embedded/bridge", () => ({
    canvasData: publishAsset,
    canvasRequest: async () => {
        throw new Error("Unexpected canvas request");
    },
}));

const { canvasVideoBody } = await import("../src/embedded/video-request");

const config = {
    model: "video-model",
    videoSeconds: "8",
    size: "16:9",
    vquality: "720p",
    videoMode: "reference",
    videoGenerateAudio: "true",
    videoWatermark: "false",
};
const referenceVideo = { id: "video-1", name: "reference.mp4", type: "video/mp4", url: "blob:local/reference", storageKey: "video:reference" };

test("builds an OpenAI-compatible request with image and public video references without submitting it", async () => {
    const body = await canvasVideoBody(
        config,
        "openai",
        "Continue the scene",
        [
            {
                id: "image-1",
                name: "reference.png",
                type: "image/png",
                dataUrl: "data:image/png;base64,AA==",
                storageKey: "image:reference",
            },
        ],
        { videos: [referenceVideo] },
    );

    expect(typeof body).toBe("string");
    expect(JSON.parse(body as string)).toMatchObject({
        input_reference: "https://coderapi.vip/image-upload/reference",
        reference_video_urls: ["https://coderapi.vip/media-upload/reference"],
    });
    expect(publishAsset).toHaveBeenCalledWith("assets.publish_image", { id: "image:reference" }, undefined);
    expect(publishAsset).toHaveBeenCalledWith("assets.publish_video", { id: "video:reference" }, undefined);
});

test("builds native Doubao content for a public reference video", async () => {
    const body = await canvasVideoBody(config, "doubao", "Continue the scene", [], {
        videos: [{ ...referenceVideo, url: "https://media.example/reference.mp4", storageKey: undefined }],
    });

    expect(typeof body).toBe("string");
    expect(JSON.parse(body as string)).toMatchObject({
        metadata: {
            content: [{ type: "video_url", video_url: { url: "https://media.example/reference.mp4" }, role: "reference_video" }],
        },
    });
});

test("rejects local-only video assets before creating a task", async () => {
    await expect(
        canvasVideoBody(config, "openai", "Continue the scene", [], {
            videos: [{ ...referenceVideo, storageKey: undefined }],
        }),
    ).rejects.toThrow();
});

test("keeps Sora's video reference limitation explicit", async () => {
    await expect(canvasVideoBody(config, "sora", "Continue the scene", [], { videos: [referenceVideo] })).rejects.toThrow();
});
