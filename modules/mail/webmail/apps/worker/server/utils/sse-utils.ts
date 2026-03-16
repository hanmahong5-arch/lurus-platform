import type { EventStream } from "h3";

// in-memory registry (cleared on server restart)
const connections = new Map<string, EventStream[]>();

export function registerConn(userId: string, stream: EventStream) {
	if (!connections.has(userId)) {
		connections.set(userId, []);
	}
	connections.get(userId)!.push(stream);
}

export function unregisterConn(userId: string, stream: EventStream) {
	const list = connections.get(userId);
	if (!list) return;
	connections.set(
		userId,
		list.filter((s) => s !== stream),
	);
	if (connections.get(userId)!.length === 0) {
		connections.delete(userId);
	}
}

export function pushToUser(userId: string, event: string, data: any) {
	const list = connections.get(userId);
	if (!list) return;
	const payload = `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
	for (const stream of list) {
		stream.push(payload);
	}
}
