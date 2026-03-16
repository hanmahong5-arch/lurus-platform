import type { Message, Thread, Address } from './types';
import { cleanSubject } from './message';

/**
 * Group messages into a thread
 */
export function groupMessagesIntoThread(messages: Message[]): Thread | null {
  if (messages.length === 0) return null;

  const sorted = [...messages].sort((a, b) => b.date.getTime() - a.date.getTime());
  const latest = sorted[0];
  const oldest = sorted[sorted.length - 1];

  // Collect unique participants
  const participantMap = new Map<string, Address>();

  for (const msg of messages) {
    if (msg.from && !participantMap.has(msg.from.email)) {
      participantMap.set(msg.from.email, msg.from);
    }
    for (const to of msg.to || []) {
      if (!participantMap.has(to.email)) {
        participantMap.set(to.email, to);
      }
    }
  }

  const unreadCount = messages.filter((m) => !m.seen).length;
  const starred = messages.some((m) => m.flagged);
  const hasAttachments = messages.some((m) => m.hasAttachments);

  return {
    id: latest.threadId,
    ownerId: latest.ownerId,
    subject: cleanSubject(latest.subject),
    snippet: latest.snippet,
    messageCount: messages.length,
    unreadCount,
    starred,
    hasAttachments,
    latestMessageAt: latest.date,
    participants: Array.from(participantMap.values()),
    createdAt: oldest.createdAt,
    updatedAt: latest.updatedAt,
  };
}

/**
 * Calculate thread aggregates after message update
 */
export function calculateThreadAggregates(messages: Message[]): {
  unreadCount: number;
  starred: boolean;
  hasAttachments: boolean;
  messageCount: number;
} {
  return {
    unreadCount: messages.filter((m) => !m.seen).length,
    starred: messages.some((m) => m.flagged),
    hasAttachments: messages.some((m) => m.hasAttachments),
    messageCount: messages.length,
  };
}

/**
 * Check if a message belongs to a thread based on references
 */
export function isMessageInThread(message: Message, threadMessages: Message[]): boolean {
  const messageId = message.messageId;
  const inReplyTo = message.inReplyTo;
  const references = message.references || [];

  // Check if this message is referenced by any thread message
  for (const tm of threadMessages) {
    if (tm.messageId && (tm.inReplyTo === messageId || tm.references?.includes(messageId || ''))) {
      return true;
    }
  }

  // Check if this message references any thread message
  const threadMessageIds = threadMessages.map((m) => m.messageId).filter(Boolean) as string[];

  if (inReplyTo && threadMessageIds.includes(inReplyTo)) {
    return true;
  }

  for (const ref of references) {
    if (threadMessageIds.includes(ref)) {
      return true;
    }
  }

  return false;
}

/**
 * Build thread hierarchy (for conversation view)
 */
export type ThreadNode = {
  message: Message;
  children: ThreadNode[];
  depth: number;
};

export function buildThreadTree(messages: Message[]): ThreadNode[] {
  const messageMap = new Map<string, Message>();
  const nodeMap = new Map<string, ThreadNode>();
  const roots: ThreadNode[] = [];

  // Index messages by messageId
  for (const msg of messages) {
    if (msg.messageId) {
      messageMap.set(msg.messageId, msg);
    }
  }

  // Create nodes
  for (const msg of messages) {
    nodeMap.set(msg.id, { message: msg, children: [], depth: 0 });
  }

  // Build tree
  for (const msg of messages) {
    const node = nodeMap.get(msg.id)!;

    // Find parent
    let parentFound = false;
    if (msg.inReplyTo && messageMap.has(msg.inReplyTo)) {
      const parentMsg = messageMap.get(msg.inReplyTo)!;
      const parentNode = nodeMap.get(parentMsg.id);
      if (parentNode) {
        parentNode.children.push(node);
        node.depth = parentNode.depth + 1;
        parentFound = true;
      }
    }

    if (!parentFound) {
      // Check references
      for (let i = (msg.references || []).length - 1; i >= 0; i--) {
        const ref = msg.references![i];
        if (messageMap.has(ref)) {
          const parentMsg = messageMap.get(ref)!;
          const parentNode = nodeMap.get(parentMsg.id);
          if (parentNode) {
            parentNode.children.push(node);
            node.depth = parentNode.depth + 1;
            parentFound = true;
            break;
          }
        }
      }
    }

    if (!parentFound) {
      roots.push(node);
    }
  }

  // Sort children by date
  const sortChildren = (nodes: ThreadNode[]) => {
    nodes.sort((a, b) => a.message.date.getTime() - b.message.date.getTime());
    for (const node of nodes) {
      sortChildren(node.children);
    }
  };

  sortChildren(roots);

  return roots;
}

/**
 * Flatten thread tree to list (for display)
 */
export function flattenThreadTree(roots: ThreadNode[]): Message[] {
  const result: Message[] = [];

  const walk = (nodes: ThreadNode[]) => {
    for (const node of nodes) {
      result.push(node.message);
      walk(node.children);
    }
  };

  walk(roots);
  return result;
}

/**
 * Get thread preview (latest messages)
 */
export function getThreadPreview(messages: Message[], count = 3): Message[] {
  return [...messages].sort((a, b) => b.date.getTime() - a.date.getTime()).slice(0, count);
}
