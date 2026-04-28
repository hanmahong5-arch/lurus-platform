import { DynamicTheme } from "@/components/dynamic-theme";
import { SignInWithIdp } from "@/components/sign-in-with-idp";
import { Translated } from "@/components/translated";
import { PhoneLoginForm } from "@/components/phone-login-form";
import { UsernameForm } from "@/components/username-form";
import { LANGUAGE_COOKIE_NAME } from "@/lib/i18n";
import { getServiceConfig } from "@/lib/service-url";
import { getActiveIdentityProviders, getBrandingSettings, getDefaultOrg, getLoginSettings } from "@/lib/zitadel";
import { Organization } from "@zitadel/proto/zitadel/org/v2/org_pb";
import { Metadata } from "next";
import { getTranslations } from "next-intl/server";
import { cookies, headers } from "next/headers";

export async function generateMetadata(): Promise<Metadata> {
  const t = await getTranslations("loginname");
  return { title: t("title") };
}

export default async function Page(props: { searchParams: Promise<Record<string | number | symbol, string | undefined>> }) {
  const searchParams = await props.searchParams;

  const loginName = searchParams?.loginName;
  const requestId = searchParams?.requestId;
  const organization = searchParams?.organization;
  const suffix = searchParams?.suffix;
  const submit: boolean = searchParams?.submit === "true";
  // Allow ?mode=classic to fall back to the original email/username form
  const mode = searchParams?.mode;

  const _headers = await headers();
  const { serviceConfig } = getServiceConfig(_headers);

  let defaultOrganization;
  if (!organization) {
    const org: Organization | null = await getDefaultOrg({ serviceConfig });
    if (org) {
      defaultOrganization = org.id;
    }
  }

  const loginSettings = await getLoginSettings({ serviceConfig, organization: organization ?? defaultOrganization });

  const identityProviders = await getActiveIdentityProviders({
    serviceConfig,
    orgId: organization ?? defaultOrganization,
  }).then((resp) => {
    return resp.identityProviders;
  });

  const branding = await getBrandingSettings({ serviceConfig, organization: organization ?? defaultOrganization });

  // Detect locale: cookie takes precedence over Accept-Language header.
  // Phone-first UI is activated for zh locale only.
  const cookieStore = await cookies();
  const localeCookie = cookieStore.get(LANGUAGE_COOKIE_NAME)?.value ?? "";
  const acceptLanguage = _headers.get("accept-language") ?? "";
  const headerLocale = acceptLanguage.split(",")[0].split("-")[0].toLowerCase();
  const activeLocale = localeCookie || headerLocale;
  const isChineseLocale = activeLocale === "zh";

  // Use phone-first flow when: locale is Chinese, mode is not explicitly classic,
  // and local authentication is allowed
  const usePhoneFirst = isChineseLocale && mode !== "classic" && loginSettings?.allowLocalAuthentication;

  return (
    <DynamicTheme branding={branding}>
      <div className="flex flex-col space-y-4">
        <h1>
          <Translated i18nKey="title" namespace="loginname" />
        </h1>
        <p className="ztdl-p">
          <Translated i18nKey="description" namespace="loginname" />
        </p>
      </div>

      <div className="w-full">
        {usePhoneFirst ? (
          <PhoneLoginForm
            loginName={loginName}
            requestId={requestId}
            organization={organization}
            defaultOrganization={defaultOrganization}
            loginSettings={loginSettings}
            submit={submit}
          />
        ) : (
          loginSettings?.allowLocalAuthentication && (
            <UsernameForm
              loginName={loginName}
              requestId={requestId}
              organization={organization}
              defaultOrganization={defaultOrganization}
              loginSettings={loginSettings}
              suffix={suffix}
              submit={submit}
              allowRegister={!!loginSettings?.allowRegister}
            />
          )
        )}

        {loginSettings?.allowExternalIdp && !!identityProviders?.length && (
          <div className="w-full pt-6 pb-4">
            <SignInWithIdp
              identityProviders={identityProviders}
              requestId={requestId}
              organization={organization}
              postErrorRedirectUrl="/loginname"
              showLabel={loginSettings?.allowLocalAuthentication}
            />
          </div>
        )}
      </div>
    </DynamicTheme>
  );
}
