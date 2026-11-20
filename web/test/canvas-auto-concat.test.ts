import { expect, test } from "bun:test";

import { canvasConcatSourcesReady, orderCanvasConcatSources } from "../src/lib/canvas/canvas-auto-concat";
import { createStoryboardRow } from "../src/lib/canvas/canvas-project-domain";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

const video = (id: string, storageKey?: string): CanvasNodeData => ({
    id, type: CanvasNodeType.Video, title: id, position: { x: 0, y: 0 }, width: 240, height: 160, metadata: { storageKey },
});

test("auto concat follows storyboard row order and waits for media", () => {
    const script: CanvasNodeData = {
        id: "script", type: CanvasNodeType.Script, title: "分镜", position: { x: 0, y: 0 }, width: 240, height: 160,
        metadata: { storyboard: { rows: [createStoryboardRow(1, { id: "first" }), createStoryboardRow(2, { id: "second" })], visibleColumns: [], referenceNodeIds: [] } },
    };
    const nodes = [script, video("a", "resource:a"), video("b")];
    const connections: CanvasConnection[] = [
        { id: "x", fromNodeId: "script", toNodeId: "a", relation: "storyboard-output", storyboardRowId: "second" },
        { id: "y", fromNodeId: "script", toNodeId: "b", relation: "storyboard-output", storyboardRowId: "first" },
    ];
    expect(orderCanvasConcatSources(["a", "b"], nodes, connections)).toEqual(["b", "a"]);
    expect(canvasConcatSourcesReady(["b", "a"], nodes)).toBe(false);
    expect(canvasConcatSourcesReady(["b", "a"], [script, video("a", "resource:a"), video("b", "resource:b")])).toBe(true);
});
