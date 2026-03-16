"use client";

import * as React from "react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { login, signInWithOAuth } from "@/lib/actions/auth";
import Link from "next/link";
import { useActionState, useTransition } from "react";
import Form from "next/form";
import { Loader2Icon, ShieldCheckIcon } from "lucide-react";
import { FormState } from "@schema";

export function LoginForm({
	className,
	...props
}: React.ComponentProps<"div">) {
	const [formState, formAction, isPending] = useActionState<
		FormState,
		FormData
	>(login, {});
	const [isOAuthPending, startOAuthTransition] = useTransition();

	const handleSSOLogin = () => {
		startOAuthTransition(async () => {
			await signInWithOAuth("keycloak");
		});
	};

	return (
		<div className={cn("flex flex-col gap-6", className)} {...props}>
			<Card>
				<CardHeader className="text-center">
					<CardTitle className="text-xl">Welcome back</CardTitle>
					<CardDescription>Login with your Lurus account or email</CardDescription>
				</CardHeader>

				<CardContent>
					<Form action={formAction}>
						<div className="grid gap-6">
							{/* Lurus SSO Login */}
							<div className="flex flex-col gap-4">
								<Button
									variant="outline"
									className="w-full"
									type="button"
									onClick={handleSSOLogin}
									disabled={isOAuthPending || isPending}
								>
									{isOAuthPending ? (
										<Loader2Icon className="mr-2 h-4 w-4 animate-spin" />
									) : (
										<ShieldCheckIcon className="mr-2 h-4 w-4" />
									)}
									Login with Lurus Account
								</Button>
							</div>

							<div className="after:border-border relative text-center text-sm after:absolute after:inset-0 after:top-1/2 after:z-0 after:flex after:items-center after:border-t">
								<span className="bg-card text-muted-foreground relative z-10 px-2">
									Or continue with email
								</span>
							</div>

							<div className="grid gap-6">
								<div className="grid gap-3">
									<Label htmlFor="email">Email</Label>
									<Input
										id="email"
										type="email"
										name="email"
										placeholder="m@example.com"
										required
										autoComplete="username"
									/>
								</div>

								<div className="grid gap-3">
									<div className="flex items-center">
										<Label htmlFor="password">Password</Label>
										{/*<a*/}
										{/*	href="#"*/}
										{/*	className="ml-auto text-sm underline-offset-4 hover:underline"*/}
										{/*>*/}
										{/*	Forgot your password?*/}
										{/*</a>*/}
									</div>
									<Input
										id="password"
										name="password"
										type="password"
										required
										autoComplete="current-password"
									/>
								</div>

								{/* inline server feedback (optional) */}
								{formState?.error && (
									<div className="text-center">
										<span className="text-sm text-red-600">
											{formState.error}
										</span>
									</div>
								)}
								{formState?.message && !formState.error && (
									<div className="text-center">
										<span className="text-sm text-green-600">
											{formState.message}
										</span>
									</div>
								)}

								<Button type="submit" className="w-full" disabled={isPending}>
									{isPending && (
										<Loader2Icon className="mr-2 h-4 w-4 animate-spin" />
									)}
									Login
								</Button>
							</div>

							<div className="text-center text-sm">
								Don&apos;t have an account?{" "}
								<Link
									href="/auth/signup"
									className="underline underline-offset-4"
								>
									Sign up
								</Link>
							</div>
						</div>
					</Form>
				</CardContent>
			</Card>

			{/*<div className="text-muted-foreground *:[a]:hover:text-primary text-center text-xs text-balance *:[a]:underline *:[a]:underline-offset-4">*/}
			{/*	By clicking continue, you agree to our <a href="#">Terms of Service</a>{" "}*/}
			{/*	and <a href="#">Privacy Policy</a>.*/}
			{/*</div>*/}
		</div>
	);
}
