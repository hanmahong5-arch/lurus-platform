"use client";

import { createContext, useContext } from "react";
import { PublicConfig } from "@schema/types/config";

const ConfigContext = createContext<PublicConfig | undefined>(undefined);

export function ConfigProvider({
	value,
	children,
}: {
	value: PublicConfig;
	children: React.ReactNode;
}) {
	return (
		<ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
	);
}

export function useConfigContext() {
	const ctx = useContext(ConfigContext);
	if (!ctx)
		throw new Error("useConfigContext must be used within <ConfigProvider>");
	return ctx;
}
