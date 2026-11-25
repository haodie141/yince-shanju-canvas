import { describe, expect, test } from "bun:test";

import { currentAgentTurnMessages } from "@/lib/canvas/cloud-agent-turn";
import { latestAgentPlanItems } from "@/lib/canvas/cloud-agent-plan";
import { latestAgentMediaBatch, type CloudAgentMediaBatchProgress } from "@/lib/canvas/cloud-agent-media-batch";

const plan = [{ id: "1", title: "本轮任务", status: "doing" as const }];
const batch: CloudAgentMediaBatchProgress = { batchId: "batch-1", mode: "image", phase: "done", concurrency: 1, total: 1, items: [] };

describe("Agent 当前轮状态", () => {
    test("新消息提交后不再显示上一轮的待办和批次", () => {
        const messages = [
            { id: "user-old", role: "user" },
            { id: "plan-old", role: "tool", planItems: plan },
            { id: "media-batch-old-batch-1", role: "tool", mediaBatch: batch },
            { id: "user-new-key", role: "user" },
        ];
        expect(currentAgentTurnMessages(messages, { id: "old" })).toEqual([]);
        const current = currentAgentTurnMessages(messages, { id: "new", idempotencyKey: "new-key" });
        expect(latestAgentPlanItems(current, "new")).toEqual([]);
        expect(latestAgentMediaBatch(current, "new")).toBeNull();
    });

    test("同一轮的插话不清除正在执行的计划和批次", () => {
        const messages = [
            { id: "user-run", role: "user" },
            { id: "plan-run", role: "tool", planItems: plan },
            { id: "media-batch-run-batch-1", role: "tool", mediaBatch: batch },
            { id: "interjection", role: "user", interjection: "delivered" },
        ];
        const current = currentAgentTurnMessages(messages, { id: "run" });
        expect(latestAgentPlanItems(current, "run")).toEqual(plan);
        expect(latestAgentMediaBatch(current, "run")).toEqual(batch);
    });

    test("继承计划不会冒充本轮计划，显式更新和清空均生效", () => {
        expect(latestAgentPlanItems([{ id: "plan-run", planItems: plan, planInherited: true }], "run")).toEqual([]);
        expect(latestAgentPlanItems([{ id: "plan-run", planItems: plan, planInherited: false }], "run")).toEqual(plan);
        expect(latestAgentPlanItems([{ id: "plan-old", planItems: plan }, { id: "plan-run", planItems: [] }], "run")).toEqual([]);
    });

    test("迟到的其他轮批次不能覆盖本轮进度", () => {
        const current = [
            { id: "media-batch-run-batch-1", mediaBatch: batch },
            { id: "media-batch-old-late", mediaBatch: { ...batch, batchId: "late" } },
        ];
        expect(latestAgentMediaBatch(current, "run")?.batchId).toBe("batch-1");
    });
});
