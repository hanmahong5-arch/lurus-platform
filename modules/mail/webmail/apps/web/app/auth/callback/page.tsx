import { handleOAuthCallback } from "@/lib/actions/auth";
import { redirect } from "next/navigation";
import MailLogo from "@/components/common/mail-logo";

interface CallbackPageProps {
	searchParams: Promise<{ code?: string; error?: string; error_description?: string }>;
}

export default async function CallbackPage({ searchParams }: CallbackPageProps) {
	const params = await searchParams;

	// Handle OAuth errors
	if (params.error) {
		return (
			<div className="bg-muted flex min-h-svh flex-col items-center justify-center gap-6 p-6 md:p-10">
				<div className="flex w-full max-w-sm flex-col gap-6 text-center">
					<MailLogo size={56} className="mx-auto" />
					<h1 className="text-2xl font-semibold text-red-600">Authentication Error</h1>
					<p className="text-muted-foreground">
						{params.error_description || params.error || "An error occurred during authentication."}
					</p>
					<a
						href="/auth/login"
						className="text-primary underline underline-offset-4"
					>
						Back to Login
					</a>
				</div>
			</div>
		);
	}

	// Handle successful OAuth callback
	if (params.code) {
		const result = await handleOAuthCallback(params.code);

		if (!result.success && result.error) {
			return (
				<div className="bg-muted flex min-h-svh flex-col items-center justify-center gap-6 p-6 md:p-10">
					<div className="flex w-full max-w-sm flex-col gap-6 text-center">
						<MailLogo size={56} className="mx-auto" />
						<h1 className="text-2xl font-semibold text-red-600">Login Failed</h1>
						<p className="text-muted-foreground">{result.error}</p>
						<a
							href="/auth/login"
							className="text-primary underline underline-offset-4"
						>
							Try Again
						</a>
					</div>
				</div>
			);
		}

		// Success - redirect handled in handleOAuthCallback
		// This shouldn't be reached, but just in case
		redirect("/dashboard/platform/overview");
	}

	// No code or error - invalid callback
	return (
		<div className="bg-muted flex min-h-svh flex-col items-center justify-center gap-6 p-6 md:p-10">
			<div className="flex w-full max-w-sm flex-col gap-6 text-center">
				<MailLogo size={56} className="mx-auto" />
				<h1 className="text-2xl font-semibold">Processing...</h1>
				<p className="text-muted-foreground">
					Please wait while we complete your login.
				</p>
			</div>
		</div>
	);
}
