import { afterEach, describe, expect, test } from "bun:test";

import { requestImageQuestion, requestToolResponse } from "../src/services/api/image";
import { resolveTextWire } from "../src/services/api/text-wire";
import { createModelChannel, defaultConfig, type AiConfig } from "../src/stores/use-config-store";

const originalFetch = globalThis.fetch;

afterEach(() => {
    globalThis.fetch = originalFetch;
});

function systemTextConfig(protocol: string, textWire?: "chat-completion" | "responses" | "claude-api"): AiConfig {
    const channel = createModelChannel({
        id: "system-text",
        name: "System",
        baseUrl: "https://gateway.example/api/system-text",
        apiKey: "system",
        apiFormat: "openai",
        scope: "system",
        models: ["chat-model"],
        modelCosts: [{ model: "chat-model", capability: "text", protocol, ...(textWire ? { textWire } : {}), billingMode: "token", unitPriceMicrocredits: 0 }],
    });
    return { ...defaultConfig, channels: [channel], model: "system-text::chat-model", textModel: "system-text::chat-model" };
}

describe("text request wire", () => {
    test("润色工具请求的输出上限进入 DeepSeek 请求体", async () => {
        let body: Record<string, unknown> = {};
        globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
            body = JSON.parse(String(init?.body));
            return new Response(JSON.stringify({ choices: [{ message: { content: "完成" } }] }), { headers: { "Content-Type": "application/json" } });
        }) as typeof fetch;
        const result = await requestToolResponse(systemTextConfig("deepseek-chat", "chat-completion"), [{ role: "user", content: "你好" }], [], "auto", undefined, { maxOutputTokens: 4096 });
        expect(body.max_tokens).toBe(4096);
        expect(result.content).toBe("完成");
    });
    test("uses the catalog wire before the protocol ID", async () => {
        expect(await resolveTextWire({ textWire: "chat-completion", interfaceType: "deepseek-chat", channelId: "system-text" })).toBe("chat-completion");
        expect(await resolveTextWire({ interfaceType: "openai-response" })).toBe("responses");
        expect(await resolveTextWire({ interfaceType: "claude-api" })).toBe("claude-api");
        expect(await resolveTextWire({ interfaceType: "" })).toBeUndefined();
    });

    test("system channels never fetch the plugin catalog", async () => {
        let loads = 0;
        const loader = async () => {
            loads += 1;
            return [{ value: "cohere-chat-v2" }];
        };
        expect(await resolveTextWire({ interfaceType: "cohere-chat-v2", channelId: "system-text" }, loader)).toBeUndefined();
        expect(loads).toBe(0);
    });

    test("custom channels resolve plugin protocols from the catalog", async () => {
        const loader = async () => [{ value: "kimi-chat", textWire: "chat-completion" as const }, { value: "cohere-chat-v2" }];
        expect(await resolveTextWire({ interfaceType: "kimi-chat" }, loader)).toBe("chat-completion");
        expect(await resolveTextWire({ interfaceType: "cohere-chat-v2" }, loader)).toBeUndefined();
        expect(await resolveTextWire({ interfaceType: "kimi-chat" }, async () => Promise.reject(new Error("offline")))).toBeUndefined();
    });

    test("chat-wire plugin models stream through /chat/completions instead of /responses", async () => {
        const urls: string[] = [];
        globalThis.fetch = (async (input: RequestInfo | URL) => {
            urls.push(String(input));
            return new Response(`data: ${JSON.stringify({ choices: [{ delta: { content: "回答" } }] })}\n\n`, { headers: { "Content-Type": "text/event-stream" } });
        }) as typeof fetch;
        let content = "";
        const answer = await requestImageQuestion(systemTextConfig("deepseek-chat", "chat-completion"), [{ role: "user", content: "你好" }], (value) => {
            content = value;
        });
        expect(answer).toBe("回答");
        expect(content).toBe("回答");
        expect(urls).toHaveLength(1);
        expect(urls[0].endsWith("/chat/completions")).toBe(true);
    });
});
