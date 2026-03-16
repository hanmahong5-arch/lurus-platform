import NextAuth from "next-auth";
import type { NextAuthConfig } from "next-auth";

declare module "next-auth" {
  interface Session {
    accessToken?: string;
    user: {
      id: string;
      name?: string | null;
      email?: string | null;
      image?: string | null;
      roles?: string[];
    };
  }
}

export const authConfig: NextAuthConfig = {
  providers: [
    {
      id: "zitadel",
      name: "Zitadel",
      type: "oidc",
      issuer: process.env.ZITADEL_ISSUER || "https://auth.lurus.cn",
      clientId: process.env.ZITADEL_CLIENT_ID || "",
      clientSecret: process.env.ZITADEL_CLIENT_SECRET || "",
      authorization: {
        params: {
          scope: "openid profile email urn:zitadel:iam:org:project:roles",
        },
      },
      profile(profile) {
        return {
          id: profile.sub,
          name: profile.name || profile.preferred_username,
          email: profile.email,
          image: profile.picture,
          roles: extractRoles(profile),
        };
      },
    },
  ],
  callbacks: {
    async jwt({ token, account, profile }) {
      if (account) {
        token.accessToken = account.access_token;
        token.roles = profile ? extractRoles(profile) : [];
      }
      return token;
    },
    async session({ session, token }) {
      session.accessToken = token.accessToken as string;
      if (session.user) {
        session.user.id = token.sub || "";
        session.user.roles = (token.roles as string[]) || [];
      }
      return session;
    },
    async authorized({ auth }) {
      if (!auth?.user) return false;
      const roles = auth.user.roles || [];
      return roles.includes("admin");
    },
  },
  pages: {
    signIn: "/login",
    error: "/login",
  },
};

function extractRoles(profile: Record<string, unknown>): string[] {
  // Zitadel puts roles in urn:zitadel:iam:org:project:roles claim
  const rolesClaim =
    (profile["urn:zitadel:iam:org:project:roles"] as Record<
      string,
      unknown
    >) || {};
  return Object.keys(rolesClaim);
}

export const { handlers, auth, signIn, signOut } = NextAuth(authConfig);
