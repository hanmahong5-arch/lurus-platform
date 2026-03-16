"use client";
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
import { signup, signInWithOAuth } from "@/lib/actions/auth";
import Link from "next/link";
import React, { useActionState, useTransition } from "react";
import { FormState } from "@schema";
import Form from "next/form";
import { Loader2Icon, ShieldCheckIcon } from "lucide-react";

export function SignupForm({
	className,
	...props
}: React.ComponentProps<"div">) {
	const passwordRef = React.useRef<HTMLInputElement>(null);
	const retypeRef = React.useRef<HTMLInputElement>(null);

	const validatePasswords = React.useCallback(() => {
		const pass = passwordRef.current?.value ?? "";
		const re = retypeRef.current?.value ?? "";
		if (!re) {
			retypeRef.current?.setCustomValidity("");
			return;
		}
		if (pass !== re) {
			retypeRef.current?.setCustomValidity("Passwords do not match");
		} else {
			retypeRef.current?.setCustomValidity("");
		}
	}, []);

	const [formState, formAction, isPending] = useActionState<
		FormState,
		FormData
	>(signup, {});
	const [isOAuthPending, startOAuthTransition] = useTransition();

	const handleSSOSignup = () => {
		startOAuthTransition(async () => {
			await signInWithOAuth("keycloak");
		});
	};

	return (
		<div className={cn("flex flex-col gap-6", className)} {...props}>
			<Card>
				<CardHeader className="text-center">
					<CardTitle className="text-xl">Welcome</CardTitle>
					<CardDescription>Create an account with Lurus or email</CardDescription>
				</CardHeader>
				<CardContent>
					<Form action={formAction}>
						<div className="grid gap-6">
							{/* Lurus SSO Signup */}
							<div className="flex flex-col gap-4">
								<Button
									variant="outline"
									className="w-full"
									type="button"
									onClick={handleSSOSignup}
									disabled={isOAuthPending || isPending}
								>
									{isOAuthPending ? (
										<Loader2Icon className="mr-2 h-4 w-4 animate-spin" />
									) : (
										<ShieldCheckIcon className="mr-2 h-4 w-4" />
									)}
									Sign up with Lurus Account
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
										autoComplete="email"
									/>
								</div>
								<div className="grid gap-3">
									<div className="flex items-center">
										<Label htmlFor="password">Password</Label>
									</div>
									<Input
										id="password"
										name="password"
										type="password"
										required
										minLength={8}
										autoComplete="new-password"
										ref={passwordRef}
										onInput={validatePasswords}
									/>
								</div>
								<div className="grid gap-3">
									<div className="flex items-center">
										<Label htmlFor="password">Retype Password</Label>
									</div>

									<Input
										id="retypePassword"
										name="retypePassword"
										type="password"
										required
										autoComplete="new-password"
										ref={retypeRef}
										onInput={validatePasswords}
									/>
									<p
										className="text-xs text-muted-foreground"
										aria-live="polite"
									>
										Password must match exactly.
									</p>
								</div>

								{formState.error && (
									<div className={"text-center"}>
										<span className="text-sm text-red-600">
											{formState.error}
										</span>
									</div>
								)}
								<Button type="submit" className="w-full" disabled={isPending}>
									{isPending && <Loader2Icon className="animate-spin" />}
									Submit
								</Button>
							</div>
							<div className="text-center text-sm">
								Already have an account?{" "}
								<Link
									href={"/auth/login"}
									className="underline underline-offset-4"
								>
									Login
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
