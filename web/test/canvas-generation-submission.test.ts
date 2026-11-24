import { describe, expect, test } from "bun:test";

import { canvasGenerationPromptMetadata, canvasGenerationRequestFingerprint, restoreCanvasGenerationPreparation, runCanvasGenerationSubmissionOnce } from "@/lib/canvas/canvas-generation-submission";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

function fingerprint(overrides: Partial<Parameters<typeof canvasGenerationRequestFingerprint>[0]> = {}) {
    return canvasGenerationRequestFingerprint({
        nodeId: "node-1",
        mode: "video",
        prompt: "夜晚的城市",
        model: "video-model",
        options: { size: "16:9", videoSeconds: 10, vquality: "768p" },
        context: {
            referenceImages: [{ id: "image-1", name: "reference.png", type: "image/png", dataUrl: "", storageKey: "resource:image-1" }],
            referenceVideos: [],
            referenceAudios: [],
            characterReferences: [],
            resolvedCharacterVersions: [],
            resolvedCharacterVoices: [],
        },
        ...overrides,
    });
}

describe("canvas generation submission", () => {
    test("本地校验失败恢复准备状态，同时保留用户编辑，不覆盖已绑定的新任务", () => {
        const original: CanvasNodeData = { id: "node-1", type: CanvasNodeType.Image, position: { x: 0, y: 0 }, width: 300, height: 200, metadata: { status: "idle", composerContent: "旧内容" } };
        const preparing: CanvasNodeData = { ...original, metadata: { ...original.metadata, status: "loading", taskStage: "正在准备参考素材", taskCreatedAt: "2026-01-01T00:00:00.000Z", taskProgress: 0, composerContent: "用户新内容" } };
        const restored = restoreCanvasGenerationPreparation(preparing, original, "2026-01-01T00:00:00.000Z");
        expect(restored.metadata?.status).toBe("idle");
        expect(restored.metadata?.taskStage).toBeUndefined();
        expect(restored.metadata?.composerContent).toBe("用户新内容");
        const submitted = { ...preparing, metadata: { ...preparing.metadata, taskId: "task-1" } };
        expect(restoreCanvasGenerationPreparation(submitted, original, "2026-01-01T00:00:00.000Z")).toBe(submitted);
        expect(restoreCanvasGenerationPreparation(preparing, original, "different-request")).toBe(preparing);
    });
    test("持久化时分离编辑器槽位提示词和模型提示词", () => {
        expect(canvasGenerationPromptMetadata("自我介绍 @图片1", "电影感：自我介绍 @图片1")).toEqual({
            composerContent: "自我介绍 @图片1",
            prompt: "电影感：自我介绍 @图片1",
        });
    });

    test("同一节点的并发提交只执行一次", async () => {
        const locks = new Map<string, Promise<unknown>>();
        let executions = 0;
        let duplicates = 0;
        let release!: () => void;
        const pending = new Promise<void>((resolve) => {
            release = resolve;
        });
        const run = () =>
            runCanvasGenerationSubmissionOnce(
                locks,
                "node-1",
                async () => {
                    executions += 1;
                    await pending;
                    return "done";
                },
                () => {
                    duplicates += 1;
                },
            );

        const submissions = [run(), run(), run()];
        await Promise.resolve();
        expect(executions).toBe(1);
        expect(duplicates).toBe(2);
        release();
        expect(await Promise.all(submissions)).toEqual(["done", "done", "done"]);

        await run();
        expect(executions).toBe(2);
    });

    test("相同输入生成稳定指纹，关键内容变化会改变指纹", () => {
        expect(fingerprint({ options: { vquality: "768p", videoSeconds: 10, size: "16:9" } })).toBe(fingerprint());
        expect(fingerprint({ prompt: "白天的城市" })).not.toBe(fingerprint());
        expect(fingerprint({ model: "another-model" })).not.toBe(fingerprint());
        expect(fingerprint({ options: { size: "9:16", videoSeconds: 10, vquality: "768p" } })).not.toBe(fingerprint());
        expect(
            fingerprint({
                context: {
                    referenceImages: [{ id: "image-2", name: "other.png", type: "image/png", dataUrl: "", storageKey: "resource:image-2" }],
                    referenceVideos: [],
                    referenceAudios: [],
                    characterReferences: [],
                    resolvedCharacterVersions: [],
                    resolvedCharacterVoices: [],
                },
            }),
        ).not.toBe(fingerprint());
    });
});
