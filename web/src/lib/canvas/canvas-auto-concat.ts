import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "@/types/canvas";
import { resourceIdFromStorageKey } from "@/services/api/resources";

/** Explicit selection order is the fallback; a shared storyboard gives stronger shot order. */
export function orderCanvasConcatSources(selectedIds: Iterable<string>, nodes: CanvasNodeData[], connections: CanvasConnection[]): string[] {
    const byId = new Map(nodes.map((node) => [node.id, node]));
    const selected = [...selectedIds].filter((id) => byId.get(id)?.type === CanvasNodeType.Video);
    const storyboardOrders = selected.map((id) => {
        const link = connections.find((item) => item.toNodeId === id && item.relation === "storyboard-output" && item.storyboardRowId);
        const source = link ? byId.get(link.fromNodeId) : undefined;
        const index = source?.metadata?.storyboard?.rows.findIndex((row) => row.id === link?.storyboardRowId) ?? -1;
        return { id, scriptId: source?.id, index };
    });
    const scriptId = storyboardOrders[0]?.scriptId;
    if (!scriptId || !storyboardOrders.every((item) => item.scriptId === scriptId && item.index >= 0)) return selected;
    return storyboardOrders.sort((left, right) => left.index - right.index).map((item) => item.id);
}

export function canvasConcatSourcesReady(sourceIds: string[], nodes: CanvasNodeData[]): boolean {
    const byId = new Map(nodes.map((node) => [node.id, node]));
    return sourceIds.length >= 2 && sourceIds.every((id) => {
        const node = byId.get(id);
        return node?.type === CanvasNodeType.Video && Boolean(resourceIdFromStorageKey(node.metadata?.storageKey));
    });
}
