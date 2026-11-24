import { describe, expect, test } from "bun:test";
import { LEGACY_TEXT_THINKING_CONFIG, resolveThinkingLevel, thinkingLevelsFor, thinkingPresetConfig, thinkingRequestLabel, withThinkingLevels, type TextThinkingConfig } from "../src/lib/text-thinking";

const levels: TextThinkingConfig = { mode: "levels", levels: ["off", "low", "high", "max"], default: "high", switch: { kind: "thinking-type" }, intensity: { kind: "effort" } };
const always: TextThinkingConfig = { mode: "always", levels: ["low", "high"], switch: { kind: "none" }, intensity: { kind: "gemini-level" } };

// 与后端 TestResolveThinkingLevel 同一组用例，前后端归一规则必须一致。
describe("resolveThinkingLevel", () => {
    test.each([
        [levels, "auto", "high"],
        [levels, undefined, "high"],
        [levels, "deep", "high"],
        [levels, "medium", "low"],
        [levels, "minimal", "low"],
        [levels, "off", "off"],
        [always, "off", "low"],
        [always, "max", "high"],
        [{ mode: "none", switch: { kind: "none" }, intensity: { kind: "none" } } as TextThinkingConfig, "high", ""],
        [undefined, "high", ""],
    ] as const)("%#", (config, requested, expected) => {
        expect(resolveThinkingLevel(config as TextThinkingConfig | undefined, requested)).toBe(expected);
    });

    test("legacy models keep the old off/medium toggle", () => {
        expect(thinkingLevelsFor(LEGACY_TEXT_THINKING_CONFIG)).toEqual(["off", "medium"]);
        expect(resolveThinkingLevel(LEGACY_TEXT_THINKING_CONFIG, "auto")).toBe("off");
        expect(resolveThinkingLevel(LEGACY_TEXT_THINKING_CONFIG, "high")).toBe("medium");
    });

    test("Agent displays and submits the selected model's effective thinking level", () => {
        const deepseek: TextThinkingConfig = { ...levels, default: "low" };
        const glm: TextThinkingConfig = { ...always, levels: ["low", "medium", "high"], default: "low" };
        expect(resolveThinkingLevel(deepseek, "auto")).toBe("low");
        expect(thinkingRequestLabel("auto", resolveThinkingLevel(deepseek, "auto"))).toBe("默认 · 轻度");
        expect(resolveThinkingLevel(deepseek, "off")).toBe("off");
        expect(thinkingRequestLabel("off", resolveThinkingLevel(deepseek, "off"))).toBe("关闭");
        expect(resolveThinkingLevel(glm, "off")).toBe("low");
        expect(thinkingRequestLabel("off", resolveThinkingLevel(glm, "off"))).toBe("轻度");
        expect(thinkingRequestLabel("auto", resolveThinkingLevel(glm, "auto"))).toBe("默认 · 轻度");
    });

    test("removing a level prunes its mappings, budget and default without mutating the model", () => {
        const source = thinkingPresetConfig("openai-gpt5")!;
        source.default = "max";
        source.intensity.budgets = { low: 1024, max: 16000 };
        const next = withThinkingLevels(source, ["high", "off", "low"]);
        expect(next.levels).toEqual(["off", "low", "high"]);
        expect(next.default).toBe("");
        expect(next.intensity.values).toEqual({});
        expect(next.intensity.budgets).toEqual({ low: 1024 });
        expect(source.intensity.values!.max).toBe("xhigh");
        expect(source.intensity.budgets.max).toBe(16000);
    });

    test("Claude adaptive cannot be disabled and deep does not select max", () => {
        const config = thinkingPresetConfig("claude-adaptive")!;
        expect(thinkingLevelsFor(config)).not.toContain("off");
        expect(resolveThinkingLevel(config, "off")).toBe("low");
        expect(resolveThinkingLevel(config, "deep")).toBe("high");
        expect(resolveThinkingLevel(config, "auto")).toBe("");
    });

    test("preset configs are independent copies", () => {
        const first = thinkingPresetConfig("openai-gpt5")!;
        first.intensity.values!.max = "changed";
        expect(thinkingPresetConfig("openai-gpt5")!.intensity.values!.max).toBe("xhigh");
    });
});
