"use client";

import { handleServerActionResponse } from "@/lib/client-utils";
import { sendPhoneLoginname } from "@/lib/server/phone-loginname";
import { LoginSettings } from "@zitadel/proto/zitadel/settings/v2/login_settings_pb";
import { useTranslations } from "next-intl";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { Alert } from "./alert";
import { AutoSubmitForm } from "./auto-submit-form";
import { Button, ButtonVariants } from "./button";
import { Spinner } from "./spinner";

// CN mobile: 11 digits starting with 1
const CN_PHONE_REGEX = /^1[3-9]\d{9}$/;
const CN_CALLING_CODE = "+86";

type Inputs = {
  phone: string;
};

type Props = {
  loginName: string | undefined;
  requestId: string | undefined;
  loginSettings: LoginSettings | undefined;
  organization?: string;
  defaultOrganization?: string;
  submit: boolean;
};

export function PhoneLoginForm({ loginName, requestId, organization, defaultOrganization, loginSettings: _loginSettings, submit }: Props) {
  const t = useTranslations("loginname");

  const { register, handleSubmit, formState } = useForm<Inputs>({
    mode: "onChange",
    defaultValues: {
      // Strip +86 prefix if pre-filled from upstream redirect
      phone: loginName ? loginName.replace(/^\+86/, "") : "",
    },
  });

  const router = useRouter();

  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string>("");
  const [showWechatToast, setShowWechatToast] = useState<boolean>(false);
  const [samlData, setSamlData] = useState<{ url: string; fields: Record<string, string> } | null>(null);

  const submitPhone = useCallback(
    async (values: Inputs) => {
      const trimmed = values.phone.trim();
      if (!CN_PHONE_REGEX.test(trimmed)) {
        setError(t("errors.invalidPhone"));
        return;
      }

      setLoading(true);
      setError("");

      try {
        const phoneE164 = `${CN_CALLING_CODE}${trimmed}`;
        const res = await sendPhoneLoginname({
          phone: phoneE164,
          requestId,
          organization,
          defaultOrganization,
        });

        handleServerActionResponse(res, router, setSamlData, setError);
      } catch {
        setError(t("errors.internalError"));
      } finally {
        setLoading(false);
      }
    },
    [requestId, organization, defaultOrganization, router, t],
  );

  useEffect(() => {
    if (submit && loginName) {
      const stripped = loginName.replace(/^\+86/, "");
      submitPhone({ phone: stripped });
    }
  }, [submit, loginName, submitPhone]);

  // Show WeChat placeholder toast
  const handleWechatClick = () => {
    setShowWechatToast(true);
    setTimeout(() => setShowWechatToast(false), 3000);
  };

  // Handler: fall back to classic username/password form
  const handlePasswordModeClick = () => {
    const params = new URLSearchParams({ mode: "classic" });
    if (requestId) params.set("requestId", requestId);
    if (organization) params.set("organization", organization);
    router.push("/loginname?" + params);
  };

  return (
    <>
      {samlData && <AutoSubmitForm url={samlData.url} fields={samlData.fields} />}

      <form className="w-full" onSubmit={handleSubmit(submitPhone)}>
        {/* Phone input with +86 prefix */}
        <div className="mb-2">
          <label className="text-12px text-input-light-label dark:text-input-dark-label relative flex flex-col">
            <span className="mb-1 leading-3">{t("labels.phoneNumber")}</span>
            <div className="flex items-stretch">
              <span
                className="flex h-[40px] items-center rounded-l-md border border-r-0 border-input-light-border dark:border-input-dark-border bg-input-light-background dark:bg-input-dark-background px-3 text-base text-gray-500 dark:text-gray-400 select-none"
                aria-hidden="true"
              >
                {CN_CALLING_CODE}
              </span>
              <input
                type="tel"
                inputMode="numeric"
                maxLength={11}
                autoComplete="tel-national"
                autoFocus
                placeholder="13800138000"
                className="h-[40px] mb-[2px] p-[7px] bg-input-light-background dark:bg-input-dark-background transition-colors duration-300 grow border border-input-light-border dark:border-input-dark-border hover:border-black hover:dark:border-white focus:border-primary-light-500 focus:dark:border-primary-dark-500 focus:outline-none focus:ring-0 text-base text-black dark:text-white placeholder:italic placeholder-gray-700 dark:placeholder-gray-700 rounded-r-md"
                data-testid="phone-text-input"
                {...register("phone", {
                  required: t("required.loginName"),
                  pattern: {
                    value: CN_PHONE_REGEX,
                    message: t("errors.invalidPhone"),
                  },
                })}
              />
            </div>
            <div className="leading-14.5px h-14.5px text-12px text-warn-light-500 dark:text-warn-dark-500 flex flex-row items-center">
              <span>{formState.errors.phone?.message ?? " "}</span>
            </div>
          </label>
        </div>

        {error && (
          <div className="py-4" data-testid="error">
            <Alert>{error}</Alert>
          </div>
        )}

        {/* Primary CTA */}
        <div className="mt-2 flex w-full flex-col gap-3">
          <Button
            data-testid="submit-button"
            type="submit"
            className="w-full"
            variant={ButtonVariants.Primary}
            disabled={loading || !formState.isValid}
          >
            {loading && <Spinner className="mr-2 h-5 w-5" />}
            {t("submit")}
          </Button>

          {/* WeChat scan placeholder */}
          <div className="relative">
            <Button
              type="button"
              className="w-full"
              variant={ButtonVariants.Secondary}
              onClick={handleWechatClick}
              data-testid="wechat-button"
            >
              <WechatIcon className="mr-2 h-5 w-5" />
              {t("wechatLogin")}
            </Button>
            {showWechatToast && (
              <div className="absolute bottom-full left-1/2 mb-2 -translate-x-1/2 whitespace-nowrap rounded-md bg-gray-800 dark:bg-gray-700 px-3 py-1.5 text-sm text-white shadow-lg">
                {t("wechatComingSoon")}
              </div>
            )}
          </div>
        </div>

        {/* Secondary: collapse password login */}
        <div className="mt-4 text-center">
          <button
            type="button"
            className="hover:text-primary-light-500 dark:hover:text-primary-dark-500 text-sm text-gray-500 dark:text-gray-400 transition-all"
            onClick={handlePasswordModeClick}
            data-testid="password-mode-button"
          >
            {t("otherLoginMethods")}
          </button>
        </div>

        {/* Legal notice */}
        <p className="mt-4 text-center text-xs text-gray-400 dark:text-gray-600">
          {t("legalNotice")}
        </p>
      </form>
    </>
  );
}

// Minimal WeChat icon SVG (brand green #07C160)
function WechatIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <path d="M8.691 2.188C3.891 2.188 0 5.476 0 9.53c0 2.212 1.17 4.203 3.002 5.55a.59.59 0 0 1 .213.665l-.39 1.48c-.019.07-.048.141-.048.213 0 .163.13.295.295.295a.29.29 0 0 0 .166-.054l1.937-1.29a.587.587 0 0 1 .332-.103.579.579 0 0 1 .188.032c.81.23 1.673.355 2.563.355.17 0 .339-.004.507-.013-.083-.315-.13-.644-.13-.983 0-3.573 3.361-6.47 7.508-6.47.258 0 .512.015.764.04C15.588 4.614 12.43 2.188 8.69 2.188zm-2.48 4.41c.52 0 .942.423.942.943s-.422.942-.942.942a.942.942 0 0 1-.942-.942c0-.52.422-.942.942-.942zm4.96 0c.52 0 .942.423.942.943s-.422.942-.942.942a.942.942 0 0 1-.942-.942c0-.52.421-.942.941-.942zM24 15.266c0-3.21-3.034-5.814-6.777-5.814s-6.778 2.604-6.778 5.814c0 3.21 3.035 5.813 6.778 5.813.748 0 1.468-.099 2.145-.28a.479.479 0 0 1 .157-.027.49.49 0 0 1 .277.086l1.617 1.077a.241.241 0 0 0 .138.044.245.245 0 0 0 .246-.246.245.245 0 0 0-.04-.176l-.326-1.234a.49.49 0 0 1 .177-.554C23.064 18.838 24 17.13 24 15.266zm-9.283-.952a.785.785 0 0 1-.785-.785c0-.433.352-.785.785-.785s.785.352.785.785a.785.785 0 0 1-.785.785zm4.998 0a.785.785 0 0 1-.785-.785c0-.433.352-.785.785-.785s.785.352.785.785a.785.785 0 0 1-.785.785z" />
    </svg>
  );
}
