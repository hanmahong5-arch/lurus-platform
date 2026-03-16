"use server";

import { cookies } from "next/headers";
import {
	ThemeNameSchema,
	ThemeModeSchema,
	THEME_COOKIE,
	MODE_COOKIE,
	RESOLVED_COOKIE,
	type ThemeName,
	type ThemeMode,
} from "@schema/types/themes";

const ONE_YEAR = 60 * 60 * 24 * 365;

export async function setThemeServer(theme: unknown) {
	const t = ThemeNameSchema.parse(theme);
	(await cookies()).set(THEME_COOKIE, t, {
		path: "/",
		sameSite: "lax",
		maxAge: ONE_YEAR,
	});
	return t as ThemeName;
}

export async function setModeServer(mode: unknown) {
	const m = ThemeModeSchema.parse(mode);
	(await cookies()).set(MODE_COOKIE, m, {
		path: "/",
		sameSite: "lax",
		maxAge: ONE_YEAR,
	});
	if (m !== "system") {
		(await cookies()).set(RESOLVED_COOKIE, m, {
			path: "/",
			sameSite: "lax",
			maxAge: ONE_YEAR,
		});
	}
	return m as ThemeMode;
}

export async function setResolvedServer(value: Partial<ThemeMode>) {
	(await cookies()).set(RESOLVED_COOKIE, value, {
		path: "/",
		sameSite: "lax",
		maxAge: ONE_YEAR,
	});
}
