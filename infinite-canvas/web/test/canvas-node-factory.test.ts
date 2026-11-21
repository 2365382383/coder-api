import { expect, test } from "bun:test";

Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined },
});

const { createCanvasNode } = await import("../src/lib/canvas/canvas-node-factory");
const { CanvasNodeType } = await import("../src/types/canvas");
const { defaultConfig } = await import("../src/stores/use-config-store");
const { buildNodeGenerationContext } = await import("../src/components/canvas/canvas-node-generation");
const { buildGenerationConfig, generationReferenceUrls, hydrateCanvasImages } = await import("../src/lib/canvas/canvas-generation-helpers");

test("new config nodes fit the grouped model picker and settings control", () => {
    const node = createCanvasNode(CanvasNodeType.Config, { x: 0, y: 0 });

    expect(node.width).toBeGreaterThanOrEqual(464);
});

test("image generation uses image settings saved on the canvas node", () => {
    const node = createCanvasNode(
        CanvasNodeType.Config,
        { x: 0, y: 0 },
        {
            generationMode: "image",
            imageAspectRatio: "3:2",
            imageResolution: "1K",
            imageOutputFormat: "jpeg",
            imageModeration: "low",
            imageOutputCompression: "72",
            quality: "low",
            count: 1,
        },
    );

    const config = buildGenerationConfig(defaultConfig, node, "image");

    expect(config).toMatchObject({
        imageAspectRatio: "3:2",
        imageResolution: "1K",
        imageOutputFormat: "jpeg",
        imageModeration: "low",
        imageOutputCompression: "72",
        quality: "low",
        count: "1",
    });
});

test("restores canvas images from their stored asset keys when inline content is empty", async () => {
    const image = createCanvasNode(
        CanvasNodeType.Image,
        { x: 0, y: 0 },
        {
            storageKey: "image:primary",
            content: "",
            images: [{ id: "alternative", content: "", storageKey: "image:alternative" }],
        },
    );

    const [restored] = await hydrateCanvasImages([image], {
        ensureImagePreview: async () => undefined,
        resolveImageUrl: async (storageKey, fallback) => (storageKey ? `blob:restored/${storageKey}` : fallback),
        uploadImage: async () => {
            throw new Error("Unexpected image upload during restore");
        },
    });

    expect(restored.metadata?.content).toBe("blob:restored/image:primary");
    expect(restored.metadata?.images?.[0].content).toBe("blob:restored/image:alternative");
});

test("passes connected image and video assets into the next video generation context", () => {
    const config = createCanvasNode(CanvasNodeType.Config, { x: 0, y: 0 }, { generationMode: "video" });
    const image = createCanvasNode(
        CanvasNodeType.Image,
        { x: 0, y: 0 },
        {
            content: "data:image/png;base64,AA==",
            storageKey: "image:reference",
        },
    );
    const video = createCanvasNode(
        CanvasNodeType.Video,
        { x: 0, y: 0 },
        {
            content: "blob:reference-video",
            storageKey: "video:reference",
        },
    );
    const nodes = [config, image, video];
    const connections = [
        { id: "image-input", fromNodeId: image.id, toNodeId: config.id },
        { id: "video-input", fromNodeId: video.id, toNodeId: config.id },
    ];

    const context = buildNodeGenerationContext(config.id, nodes, connections, "Animate these references");

    expect(context.referenceImages.map((reference) => reference.storageKey)).toEqual(["image:reference"]);
    expect(context.referenceVideos.map((reference) => reference.storageKey)).toEqual(["video:reference"]);
    expect(generationReferenceUrls(context)).toEqual(["image:reference", "video:reference"]);
});
