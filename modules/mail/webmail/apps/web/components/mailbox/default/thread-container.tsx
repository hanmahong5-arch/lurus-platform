"use client";
import React from "react";
import { Divider } from "@mantine/core";
import { MessageEntity } from "@db";
import {
	CollapsibleThread,
	ThreadSummaryBar,
	CollapsedMessageHeader,
} from "./collapsible-thread";

type ThreadContainerProps = {
	messages: MessageEntity[];
	renderMessage: (
		message: MessageEntity,
		index: number,
		isExpanded: boolean,
		toggleExpand: () => void
	) => React.ReactNode;
};

export default function ThreadContainer({
	messages,
	renderMessage,
}: ThreadContainerProps) {
	// If only 1-2 messages, show all expanded without summary bar
	if (messages.length <= 2) {
		return (
			<>
				{messages.map((message, index) => (
					<div key={message.id}>
						{renderMessage(message, index, true, () => {})}
						<Divider className="opacity-50 mb-6" ml="xl" mr="xl" />
					</div>
				))}
			</>
		);
	}

	// For 3+ messages, use collapsible view
	return (
		<CollapsibleThread messages={messages}>
			{({ expandedIds, toggleExpand, isExpanded, expandAll, collapseAll }) => (
				<>
					<ThreadSummaryBar
						messages={messages}
						expandedCount={expandedIds.size}
						onExpandAll={expandAll}
						onCollapseAll={collapseAll}
					/>

					{messages.map((message, index) => {
						const expanded = isExpanded(message.id);

						return (
							<div key={message.id}>
								{expanded ? (
									<>
										{renderMessage(message, index, true, () =>
											toggleExpand(message.id)
										)}
									</>
								) : (
									<CollapsedMessageHeader
										message={message}
										onClick={() => toggleExpand(message.id)}
									/>
								)}
								{expanded && (
									<Divider className="opacity-50 mb-6" ml="xl" mr="xl" />
								)}
							</div>
						);
					})}
				</>
			)}
		</CollapsibleThread>
	);
}
