import { create } from "zustand";
import { persist } from "zustand/middleware";

export type Theme = "light" | "dark" | "system";
export type MailLayout = "default" | "compact" | "comfortable";
export type ReadingPanePosition = "right" | "bottom" | "hidden";

export interface UIPreferences {
	// Theme
	theme: Theme;

	// Mail list
	mailLayout: MailLayout;
	readingPane: ReadingPanePosition;
	showAvatars: boolean;
	showPreview: boolean;
	groupByDate: boolean;

	// Sidebar
	sidebarCollapsed: boolean;
	sidebarWidth: number;

	// Compose
	defaultFontSize: number;
	signatureEnabled: boolean;

	// Actions
	setTheme: (theme: Theme) => void;
	setMailLayout: (layout: MailLayout) => void;
	setReadingPane: (position: ReadingPanePosition) => void;
	toggleAvatars: () => void;
	togglePreview: () => void;
	toggleGroupByDate: () => void;
	setSidebarCollapsed: (collapsed: boolean) => void;
	setSidebarWidth: (width: number) => void;
	setDefaultFontSize: (size: number) => void;
	toggleSignature: () => void;
}

export const useUIStore = create<UIPreferences>()(
	persist(
		(set) => ({
			theme: "system",
			mailLayout: "default",
			readingPane: "right",
			showAvatars: true,
			showPreview: true,
			groupByDate: true,
			sidebarCollapsed: false,
			sidebarWidth: 280,
			defaultFontSize: 14,
			signatureEnabled: true,

			setTheme: (theme) => set({ theme }),
			setMailLayout: (mailLayout) => set({ mailLayout }),
			setReadingPane: (readingPane) => set({ readingPane }),
			toggleAvatars: () => set((s) => ({ showAvatars: !s.showAvatars })),
			togglePreview: () => set((s) => ({ showPreview: !s.showPreview })),
			toggleGroupByDate: () => set((s) => ({ groupByDate: !s.groupByDate })),
			setSidebarCollapsed: (sidebarCollapsed) => set({ sidebarCollapsed }),
			setSidebarWidth: (sidebarWidth) => set({ sidebarWidth }),
			setDefaultFontSize: (defaultFontSize) => set({ defaultFontSize }),
			toggleSignature: () => set((s) => ({ signatureEnabled: !s.signatureEnabled })),
		}),
		{
			name: "lurus-ui-preferences",
			partialize: (state) => ({
				theme: state.theme,
				mailLayout: state.mailLayout,
				readingPane: state.readingPane,
				showAvatars: state.showAvatars,
				showPreview: state.showPreview,
				groupByDate: state.groupByDate,
				sidebarCollapsed: state.sidebarCollapsed,
				sidebarWidth: state.sidebarWidth,
				defaultFontSize: state.defaultFontSize,
				signatureEnabled: state.signatureEnabled,
			}),
		},
	),
);
