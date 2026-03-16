/**
 * Identity Overview API Route (lurus-webmail)
 *
 * Server-side proxy to lurus-identity internal endpoint.
 * Extracts the Zitadel sub from Supabase user metadata, resolves the identity
 * account, then fetches the aggregated overview (VIP, wallet, subscription).
 * Returns 404 if no Zitadel identity is linked (email-only users).
 */

import { NextRequest, NextResponse } from 'next/server';
import { currentSession } from '@/lib/actions/auth';

const IDENTITY_URL =
  process.env.IDENTITY_SERVICE_URL ?? 'https://identity.lurus.cn';
const IDENTITY_INTERNAL_KEY = process.env.IDENTITY_SERVICE_INTERNAL_KEY ?? '';
const PRODUCT_ID = 'lurus-webmail';

export async function GET(_request: NextRequest) {
  const session = await currentSession();
  if (!session?.user) {
    return NextResponse.json({ error: 'unauthorized' }, { status: 401 });
  }

  // Zitadel sub is stored in user_metadata during OAuth callback
  const zitadelSub =
    (session.user.user_metadata?.sub as string | undefined) ??
    (session.user.user_metadata?.provider_id as string | undefined);

  if (!zitadelSub) {
    return NextResponse.json({ error: 'no linked identity account' }, { status: 404 });
  }

  const headers = { Authorization: `Bearer ${IDENTITY_INTERNAL_KEY}` };

  // Resolve identity account ID from Zitadel subject
  const accountRes = await fetch(
    `${IDENTITY_URL}/internal/v1/accounts/by-zitadel-sub/${encodeURIComponent(zitadelSub)}`,
    { headers },
  );
  if (!accountRes.ok) {
    return NextResponse.json({ error: 'account not found' }, { status: 404 });
  }
  const account = (await accountRes.json()) as { id: number };

  // Fetch aggregated overview
  const ovRes = await fetch(
    `${IDENTITY_URL}/internal/v1/accounts/${account.id}/overview?product_id=${encodeURIComponent(PRODUCT_ID)}`,
    { headers },
  );
  if (!ovRes.ok) {
    return NextResponse.json({ error: 'identity service unavailable' }, { status: 503 });
  }

  return NextResponse.json(await ovRes.json());
}
