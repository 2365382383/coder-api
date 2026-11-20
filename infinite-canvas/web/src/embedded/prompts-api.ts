import { useAssetStore } from "../stores/use-asset-store";
import type { Prompt, PromptListResponse } from "../services/api/prompts";
import { canvasRequest } from "./bridge";

export type {
  Prompt,
  PromptListResponse,
  PromptSourceStatus,
  PromptSourceRefreshResult,
  PromptSourceRefreshSummary,
} from "../services/api/prompts";
export { formatPromptDate } from "../services/api/prompts";

export const ALL_PROMPTS_OPTION = "all";
const BUNDLED_PROMPT_CATEGORY = "精选提示词";

type BundledCatalogItem = {
  id: string;
  title: string;
  prompt: string;
  description?: string;
  tags?: string[];
  author?: string;
  source_id?: string;
  source_url?: string;
  created_at?: string;
  mode?: string;
  cover?: string;
  references?: string[];
};

type BundledCatalog = { items?: BundledCatalogItem[] };

let bundledPromptsPromise: Promise<Prompt[]> | undefined;

function bundledAssetUrl(path: string | undefined) {
  const normalized = path?.replace(/^\/+/, "") || "";
  if (!normalized.startsWith("media/") || normalized.includes("..")) {
    return "";
  }
  return `/prompt-catalog/${normalized}`;
}

function bundledCatalogPrompts() {
  bundledPromptsPromise ??= canvasRequest("prompts.catalog")
    .then(async (response) => {
      if (!response.ok) {
        throw new Error(`Prompt catalog failed (${response.status})`);
      }
      const catalog = (await response.json()) as BundledCatalog;
      return (catalog.items || []).flatMap((item) => {
        if (!item.id || !item.title || !item.prompt) {
          return [];
        }
        return [
          {
            id: item.id,
            title: item.title,
            prompt: item.prompt,
            description: item.description || "",
            coverUrl: bundledAssetUrl(item.cover),
            referenceImageUrls: (item.references || []).flatMap((path) => {
              const url = bundledAssetUrl(path);
              return url ? [url] : [];
            }),
            tags: item.tags || [],
            preview: "",
            createdAt: item.created_at || "",
            updatedAt: item.created_at || "",
            author: item.author || "",
            sourceUrl: item.source_url || "",
            imageMode: item.mode,
            sourceId: item.source_id || "bundled",
            category: BUNDLED_PROMPT_CATEGORY,
            githubUrl: item.source_url || "",
          } satisfies Prompt,
        ];
      });
    })
    .catch((error) => {
      bundledPromptsPromise = undefined;
      throw error;
    });
  return bundledPromptsPromise;
}

// The embedded prompt library combines the reviewed same-origin catalog with
// the current user's text assets. External feeds and WebDAV stay disabled.
export async function fetchPrompts({
  keyword = "",
  tag = [],
  category = ALL_PROMPTS_OPTION,
  page = 1,
  pageSize = 20,
}: {
  keyword?: string;
  tag?: string[];
  category?: string;
  page?: number;
  pageSize?: number;
} = {}): Promise<PromptListResponse> {
  const accountPrompts: Prompt[] = useAssetStore
    .getState()
    .assets.filter((asset) => asset.kind === "text")
    .map((asset) => ({
      id: asset.id,
      title: asset.title,
      prompt: asset.data.content,
      description: asset.note || "",
      coverUrl: "",
      referenceImageUrls: [],
      tags: asset.tags,
      preview: "",
      createdAt: asset.createdAt,
      updatedAt: asset.updatedAt,
      sourceId: "account",
      category: "我的提示词",
      githubUrl: "",
    }));
  const bundledPrompts = await bundledCatalogPrompts();
  const items = [...bundledPrompts, ...accountPrompts]
    .filter(
      (item) =>
        (!keyword || `${item.title}\n${item.prompt}`.includes(keyword)) &&
        (!tag.length || tag.every((entry) => item.tags.includes(entry))) &&
        (category === ALL_PROMPTS_OPTION || category === item.category),
    );
  return {
    items: items.slice((page - 1) * pageSize, page * pageSize),
    total: items.length,
    categories: [BUNDLED_PROMPT_CATEGORY, "我的提示词"],
    tags: [...new Set(items.flatMap((item) => item.tags))],
  };
}

export const fetchSourcePrompts = async (): Promise<Prompt[]> => [];
export const fetchPromptSourceStatuses = async () => ({});
export const refreshSource = async () => {
  throw new Error("External prompt sources are disabled");
};
export const refreshAllSources = async () => ({
  results: [],
  total: 0,
  successCount: 0,
  failureCount: 0,
});
export const refreshDueSources = refreshAllSources;
