import type { MailRule, RuleAction, RuleActionType } from './types';

/**
 * Rule action result
 */
export type ActionResult = {
  action: RuleAction;
  success: boolean;
  error?: string;
};

/**
 * Combined results of applying a rule
 */
export type RuleResult = {
  rule: MailRule;
  matched: boolean;
  actions: ActionResult[];
  stopProcessing: boolean;
};

/**
 * Validate rule actions
 */
export function validateRuleActions(actions: RuleAction[]): { valid: boolean; errors: string[] } {
  const errors: string[] = [];

  if (!actions || actions.length === 0) {
    errors.push('At least one action is required');
    return { valid: false, errors };
  }

  const actionTypes = new Set<string>();

  for (const action of actions) {
    // Check for duplicate conflicting actions
    if (actionTypes.has(action.type)) {
      errors.push(`Duplicate action: ${action.type}`);
    }
    actionTypes.add(action.type);

    // Validate action-specific requirements
    switch (action.type) {
      case 'move_to_mailbox':
        if (!action.value) {
          errors.push('move_to_mailbox requires a mailbox ID');
        }
        break;

      case 'add_label':
      case 'remove_label':
        if (!action.value) {
          errors.push(`${action.type} requires a label ID`);
        }
        break;

      case 'forward':
        if (!action.value || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(action.value)) {
          errors.push('forward requires a valid email address');
        }
        break;

      case 'reply':
        // Reply might need template ID or body
        break;
    }

    // Check for conflicting actions
    if (action.type === 'mark_read' && actionTypes.has('mark_unread')) {
      errors.push('Cannot have both mark_read and mark_unread actions');
    }
    if (action.type === 'star' && actionTypes.has('unstar')) {
      errors.push('Cannot have both star and unstar actions');
    }
    if (action.type === 'mark_important' && actionTypes.has('mark_not_important')) {
      errors.push('Cannot have both mark_important and mark_not_important actions');
    }
    if (action.type === 'archive' && actionTypes.has('trash')) {
      errors.push('Cannot have both archive and trash actions');
    }
    if (action.type === 'delete' && (actionTypes.has('archive') || actionTypes.has('trash'))) {
      errors.push('Cannot combine delete with archive or trash');
    }
  }

  return { valid: errors.length === 0, errors };
}

/**
 * Get human-readable description of an action
 */
export function describeAction(action: RuleAction): string {
  switch (action.type) {
    case 'move_to_mailbox':
      return `Move to folder`;
    case 'add_label':
      return `Add label`;
    case 'remove_label':
      return `Remove label`;
    case 'mark_read':
      return 'Mark as read';
    case 'mark_unread':
      return 'Mark as unread';
    case 'star':
      return 'Add star';
    case 'unstar':
      return 'Remove star';
    case 'archive':
      return 'Archive';
    case 'trash':
      return 'Move to trash';
    case 'delete':
      return 'Delete permanently';
    case 'forward':
      return `Forward to ${action.value}`;
    case 'reply':
      return 'Send auto-reply';
    case 'skip_inbox':
      return 'Skip inbox';
    case 'mark_important':
      return 'Mark as important';
    case 'mark_not_important':
      return 'Mark as not important';
    default:
      return action.type;
  }
}

/**
 * Get action priority (lower = execute first)
 */
export function getActionPriority(type: RuleActionType): number {
  const priorities: Record<RuleActionType, number> = {
    // Labeling first
    add_label: 1,
    remove_label: 2,

    // Then flags
    mark_read: 10,
    mark_unread: 10,
    star: 11,
    unstar: 11,
    mark_important: 12,
    mark_not_important: 12,

    // Skip inbox before move
    skip_inbox: 20,

    // Moving
    move_to_mailbox: 30,
    archive: 31,
    trash: 32,

    // Notifications
    forward: 40,
    reply: 41,

    // Delete last
    delete: 100,
  };

  return priorities[type] ?? 50;
}

/**
 * Sort actions by execution priority
 */
export function sortActionsByPriority(actions: RuleAction[]): RuleAction[] {
  return [...actions].sort((a, b) => getActionPriority(a.type) - getActionPriority(b.type));
}

/**
 * Check if action modifies message flags
 */
export function isFlsagModifyingAction(type: RuleActionType): boolean {
  return ['mark_read', 'mark_unread', 'star', 'unstar', 'mark_important', 'mark_not_important'].includes(type);
}

/**
 * Check if action moves message
 */
export function isMovingAction(type: RuleActionType): boolean {
  return ['move_to_mailbox', 'archive', 'trash', 'delete'].includes(type);
}

/**
 * Check if action requires external action (email send)
 */
export function isExternalAction(type: RuleActionType): boolean {
  return ['forward', 'reply'].includes(type);
}

/**
 * Merge multiple rule results
 */
export function mergeRuleResults(results: RuleResult[]): {
  appliedRules: MailRule[];
  allActions: RuleAction[];
  errors: string[];
} {
  const appliedRules: MailRule[] = [];
  const allActions: RuleAction[] = [];
  const errors: string[] = [];

  for (const result of results) {
    if (!result.matched) continue;

    appliedRules.push(result.rule);

    for (const actionResult of result.actions) {
      if (actionResult.success) {
        allActions.push(actionResult.action);
      } else if (actionResult.error) {
        errors.push(`Rule "${result.rule.name}": ${actionResult.error}`);
      }
    }

    if (result.stopProcessing) break;
  }

  return { appliedRules, allActions, errors };
}
