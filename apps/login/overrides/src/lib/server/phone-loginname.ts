"use server";

import { isClassifiedError } from "@/lib/grpc/interceptors/error-classification";
import { createLogger } from "@/lib/logger";
import { create } from "@zitadel/client";
import { RequestChallengesSchema } from "@zitadel/proto/zitadel/session/v2/challenge_pb";
import { ChecksSchema } from "@zitadel/proto/zitadel/session/v2/session_service_pb";
import { AddHumanUserRequestSchema } from "@zitadel/proto/zitadel/user/v2/user_service_pb";
import { OrganizationSchema } from "@zitadel/proto/zitadel/object/v2/object_pb";
import { getTranslations } from "next-intl/server";
import { headers } from "next/headers";
import { getServiceConfig } from "../service-url";
import { addHuman, getLoginSettings, searchUsers, SearchUsersCommand } from "../zitadel";
import { createSessionAndUpdateCookie } from "./cookie";

const logger = createLogger("phone-loginname");

// E.164 validation for Chinese mobile numbers (+86 + 11 digits starting with 1[3-9])
const CN_E164_REGEX = /^\+861[3-9]\d{9}$/;

export type SendPhoneLoginnameCommand = {
  phone: string; // already E.164 e.g. +8613800138000
  requestId?: string;
  organization?: string;
  defaultOrganization?: string;
};

/**
 * Phone-OTP-first login flow for Chinese B2B users.
 *
 * Steps:
 * 1. Validate E.164 phone format
 * 2. Search for existing user by phone number
 * 3. If not found: silently create a new human user (phone-only, no password)
 * 4. Create a session with otpSms challenge
 * 5. Redirect to OTP page
 */
export async function sendPhoneLoginname(command: SendPhoneLoginnameCommand) {
  const _headers = await headers();
  const { serviceConfig } = getServiceConfig(_headers);

  const t = await getTranslations("loginname");

  if (!CN_E164_REGEX.test(command.phone)) {
    return { error: t("errors.invalidPhone") };
  }

  const loginSettings = await getLoginSettings({
    serviceConfig,
    organization: command.organization,
  });

  if (!loginSettings) {
    return { error: t("errors.couldNotGetLoginSettings") };
  }

  // Search for existing user by phone
  const searchRequest: SearchUsersCommand = {
    serviceConfig,
    searchValue: command.phone,
    organizationId: command.organization,
    loginSettings,
  };

  const searchResult = await searchUsers(searchRequest);

  if (!searchResult) {
    logger.error("searchUsers returned undefined for phone", { phone: command.phone });
    return { error: t("errors.couldNotSearchUsers") };
  }

  if ("error" in searchResult && searchResult.error) {
    logger.debug("searchUsers returned error", { error: searchResult.error });
    return searchResult;
  }

  if (!("result" in searchResult)) {
    return { error: t("errors.couldNotSearchUsers") };
  }

  const users = searchResult.result ?? [];

  let userId: string | undefined;

  if (users.length > 1) {
    logger.error("Multiple users found for phone", { phone: command.phone, count: users.length });
    return { error: t("errors.moreThanOneUserFound") };
  }

  if (users.length === 1 && users[0].userId) {
    // Existing user found
    userId = users[0].userId;
    logger.debug("Existing user found by phone", { userId });
  } else {
    // No user found: auto-register with phone as identity
    logger.debug("No user found by phone, auto-registering", { phone: command.phone });

    const addRequest = create(AddHumanUserRequestSchema, {
      username: command.phone,
      profile: {
        givenName: command.phone,
        familyName: "",
      },
      phone: {
        phone: command.phone,
        verification: {
          case: "isVerified",
          value: false,
        },
      },
      ...(command.organization
        ? {
            organization: create(OrganizationSchema, {
              org: { case: "orgId", value: command.organization },
            }),
          }
        : {}),
    });

    let created;
    try {
      created = await addHuman({ serviceConfig, request: addRequest });
    } catch (err: any) {
      logger.error("Failed to auto-register phone user", { error: err?.message });
      return { error: t("errors.couldNotCreateUser") };
    }

    if (!created?.userId) {
      return { error: t("errors.couldNotCreateUser") };
    }

    userId = created.userId;
    logger.debug("Auto-registered phone user", { userId });
  }

  // Create session targeting this user, with otpSms challenge
  const checks = create(ChecksSchema, {
    user: { search: { case: "userId", value: userId } },
  });

  const challenges = create(RequestChallengesSchema, {
    otpSms: {},
  });

  const sessionOrError = await createSessionAndUpdateCookie({
    checks,
    requestId: command.requestId,
    challenges,
  }).catch((error) => {
    if (isClassifiedError(error) && error.message?.includes("Errors.User.NotActive")) {
      return { error: t("errors.userNotActive") };
    }
    logger.error("Failed to create session for phone user", { error: error?.message });
    return { error: t("errors.couldNotCreateSession") };
  });

  if ("error" in sessionOrError) {
    return sessionOrError;
  }

  const { session, sessionCookie } = sessionOrError;

  if (!session?.factors?.user?.loginName) {
    return { error: t("errors.couldNotCreateSession") };
  }

  // Redirect to OTP SMS verification page
  const otpParams = new URLSearchParams({
    loginName: session.factors.user.loginName,
    sessionId: sessionCookie.id,
  });

  if (command.requestId) {
    otpParams.append("requestId", command.requestId);
  }

  const orgId = command.organization ?? session.factors.user.organizationId;
  if (orgId) {
    otpParams.append("organization", orgId);
  }

  return { redirect: `/otp/sms?${otpParams}` };
}
