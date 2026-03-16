import { z } from "zod";

export const THEME_NAMES = ["brand", "indigo", "violet", "teal"] as const;
export const THEME_MODES = ["light", "dark", "system"] as const;

export const ThemeNameSchema = z.enum(THEME_NAMES);
export const ThemeModeSchema = z.enum(THEME_MODES);

export type ThemeName = z.infer<typeof ThemeNameSchema>;
export type ThemeMode = z.infer<typeof ThemeModeSchema>;

export const AppearanceSchema = z.object({
	theme: ThemeNameSchema,
	mode: ThemeModeSchema,
});

export const THEME_COOKIE = "lurus.theme"; // "indigo" | "violet" | "teal"
export const MODE_COOKIE = "lurus.mode"; // "light" | "dark" | "system"
export const RESOLVED_COOKIE = "lurus.resolved"; // "light" | "dark"
