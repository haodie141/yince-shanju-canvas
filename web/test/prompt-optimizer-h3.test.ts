import { expect, test } from "bun:test";
import { promptOptimizerPlugin } from "../src/lib/plugins/builtin/prompt-optimizer";

test("H3 润色使用视频规格并拒绝丢失素材编号的结果", async () => {
    let messages: any[] = [];
    let maxOutputTokens = 0;
    let output = "@图片1 人物在雨中行走";
    const provider = promptOptimizerPlugin.createPromptOptimizer!({
        services: { ai: { text: { requestToolResponse: async (request: any) => {
            messages = request.messages;
            maxOutputTokens = request.maxOutputTokens;
            return { content: "", toolCalls: [{ name: "optimize_prompt", arguments: JSON.stringify({ optimizedPrompt: output }) }] };
        } } } },
    } as any);
    const input = { prompt: "@图片1 在雨中行走", instruction: "镜头慢慢推近", mode: "refine" as const, generationMode: "video" as const, targetProtocol: "shanju-h3-cinema-unified-fast", videoSettings: { seconds: "10", resolution: "768p竖" } };
    expect((await provider.optimize(input)).optimizedPrompt).toBe(output);
    expect(maxOutputTokens).toBe(64000);
    expect(messages[0].content).toContain('"optimizedPrompt":"人物在公园中行走');
    expect(messages[0].content).toContain("不要照搬内容");
    const content = messages[1].content[0].text;
    expect(content).toContain("MiniMax H3");
    expect(content).toContain("目标时长：10 秒");
    expect(content).toContain("768p竖");
    expect(content).toContain("用户原始提示词：\n@图片1 在雨中行走");
    expect(content).toContain("用户补充的润色要求：\n镜头慢慢推近");
    output = "人物在雨中行走";
    await expect(provider.optimize(input)).rejects.toThrow("丢失了原有素材引用");
});
