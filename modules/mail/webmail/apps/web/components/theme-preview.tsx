"use client";

import React from "react";

const Swatch = ({ name, bg, fg }: { name: string; bg: string; fg: string }) => (
	<div className="rounded-lg border border-border overflow-hidden">
		<div className={`px-3 py-4 ${bg} ${fg}`}>
			<div className="text-sm font-medium">{name}</div>
			<div className="text-xs opacity-80">
				{bg.replace("bg-", "")} / {fg.replace("text-", "")}
			</div>
		</div>
	</div>
);

export default function ThemePreview() {
	return (
		<div className="min-h-screen bg-background text-foreground">
			<div className="mx-auto max-w-5xl p-6 space-y-8">
				<header className="flex items-center justify-between">
					<h1 className="text-2xl font-bold">Theme Preview</h1>
					<div className="flex gap-2">
						{/* Simple toggles for quick testing */}
						<button
							className="px-3 py-1.5 rounded-md border border-border hover:bg-foreground/5"
							onClick={() => document.documentElement.classList.toggle("dark")}
						>
							Toggle Dark
						</button>
						<select
							className="px-3 py-1.5 rounded-md border border-input bg-background"
							onChange={(e) =>
								document.documentElement.setAttribute(
									"data-theme",
									e.target.value,
								)
							}
							// defaultValue={
							//     document.documentElement.getAttribute("data-theme") ?? "indigo"
							// }
						>
							<option value="indigo">indigo</option>
							<option value="violet">violet</option>
							<option value="teal">teal</option>
						</select>
					</div>
				</header>

				{/* Core pairs */}
				<section>
					<h2 className="mb-3 text-lg font-semibold">Semantic Pairs</h2>
					<div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-4">
						<Swatch
							name="Background/Foreground"
							bg="bg-background"
							fg="text-foreground"
						/>
						<Swatch name="Card" bg="bg-card" fg="text-card-foreground" />
						<Swatch
							name="Popover"
							bg="bg-popover"
							fg="text-popover-foreground"
						/>
						<Swatch name="Muted" bg="bg-muted" fg="text-muted-foreground" />
						<Swatch name="Accent" bg="bg-accent" fg="text-accent-foreground" />
						<Swatch
							name="Secondary"
							bg="bg-secondary"
							fg="text-secondary-foreground"
						/>
						<Swatch
							name="Primary"
							bg="bg-primary"
							fg="text-primary-foreground"
						/>
						<Swatch
							name="Destructive"
							bg="bg-destructive"
							fg="text-primary-foreground"
						/>
					</div>
				</section>

				{/* Brand scale preview */}
				<section>
					<h2 className="mb-3 text-lg font-semibold">Brand Scale</h2>
					<div className="grid grid-cols-2 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-11 gap-2">
						{[50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950].map((n) => (
							<div
								key={n}
								className={`h-14 rounded-md border border-border flex items-center justify-center bg-brand-${n} text-white/90`}
							>
								<span className="text-xs">brand-{n}</span>
							</div>
						))}
					</div>
				</section>

				{/* Inputs & rings */}
				<section>
					<h2 className="mb-3 text-lg font-semibold">Inputs & Rings</h2>
					<div className="grid gap-4 md:grid-cols-2">
						<div className="space-y-2 rounded-lg border border-border p-4">
							<label className="text-sm">Input</label>
							<input
								placeholder="Type…"
								className="w-full rounded-md bg-background border border-input px-3 py-2
                           focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
							/>
							<p className="text-xs text-muted-foreground">
								Uses <code>--input</code> and <code>--ring</code>
							</p>
						</div>
						<div className="space-y-2 rounded-lg border border-border p-4">
							<label className="text-sm">Primary Button</label>
							<button className="px-3 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90">
								Primary
							</button>
							<p className="text-xs text-muted-foreground">
								Uses <code>--primary</code> / <code>--primary-foreground</code>
							</p>
						</div>
					</div>
				</section>
			</div>
		</div>
	);
}
