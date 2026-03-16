import { identityAdmin } from "./client";

// --- Types ---

export interface Account {
  id: number;
  lurus_id: string;
  zitadel_sub: string;
  display_name: string;
  avatar_url: string;
  email: string;
  status: number;
  locale: string;
  aff_code: string;
  created_at: string;
  updated_at: string;
}

export interface VIPSummary {
  level: number;
  level_name: string;
  level_en: string;
  points: number;
  level_expires_at?: { time: string };
}

export interface WalletSummary {
  balance: number;
  frozen: number;
}

export interface SubscriptionSummary {
  id: number;
  product_id: string;
  plan_code: string;
  status: string;
  expires_at?: string;
  auto_renew: boolean;
}

export interface AccountDetail {
  account: Account;
  vip: VIPSummary;
  wallet: WalletSummary;
  subscriptions: SubscriptionSummary[];
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface Product {
  id: string;
  name: string;
  description: string;
  status: string;
  created_at: string;
}

export interface ProductPlan {
  id: number;
  product_id: string;
  code: string;
  name: string;
  price_cny: number;
  duration_days: number;
  features: Record<string, unknown>;
  status: string;
}

export interface Invoice {
  id: number;
  account_id: number;
  invoice_no: string;
  amount: number;
  status: string;
  payment_method: string;
  created_at: string;
}

export interface FinancialReportRow {
  period: string;
  gross_revenue: number;
  refund_total: number;
  net_revenue: number;
  transaction_count: number;
}

export interface AdminSetting {
  key: string;
  value: string;
  is_secret: boolean;
  updated_by: string;
  updated_at: string;
}

export interface Organization {
  id: number;
  name: string;
  status: string;
  owner_account_id: number;
  created_at: string;
}

export interface RedemptionCode {
  code: string;
  product_id: string;
  plan_code: string;
  duration_days: number;
  expires_at?: string;
  redeemed: boolean;
}

// --- API Functions ---

export async function listAccounts(
  token: string,
  params: { q?: string; page?: number; page_size?: number },
) {
  return identityAdmin<PaginatedResponse<Account>>("/accounts", token, {
    params: params as Record<string, string | number>,
  });
}

export async function getAccount(token: string, id: number) {
  return identityAdmin<AccountDetail>(`/accounts/${id}`, token);
}

export async function adjustWallet(
  token: string,
  accountId: number,
  amount: number,
  description: string,
) {
  return identityAdmin<WalletSummary>(
    `/accounts/${accountId}/wallet/adjust`,
    token,
    {
      method: "POST",
      body: JSON.stringify({ amount, description }),
    },
  );
}

export async function grantEntitlement(
  token: string,
  accountId: number,
  productId: string,
  key: string,
  value: string,
) {
  return identityAdmin<{ granted: boolean }>(
    `/accounts/${accountId}/grant`,
    token,
    {
      method: "POST",
      body: JSON.stringify({ product_id: productId, key, value }),
    },
  );
}

export async function listInvoices(
  token: string,
  params: { account_id?: number; page?: number; page_size?: number },
) {
  return identityAdmin<PaginatedResponse<Invoice>>("/invoices", token, {
    params: params as Record<string, string | number>,
  });
}

export async function approveRefund(
  token: string,
  refundNo: string,
  reviewerId: string,
  reviewNote: string,
) {
  return identityAdmin<{ approved: boolean }>(
    `/refunds/${refundNo}/approve`,
    token,
    {
      method: "POST",
      body: JSON.stringify({
        reviewer_id: reviewerId,
        review_note: reviewNote,
      }),
    },
  );
}

export async function rejectRefund(
  token: string,
  refundNo: string,
  reviewerId: string,
  reviewNote: string,
) {
  return identityAdmin<{ rejected: boolean }>(
    `/refunds/${refundNo}/reject`,
    token,
    {
      method: "POST",
      body: JSON.stringify({
        reviewer_id: reviewerId,
        review_note: reviewNote,
      }),
    },
  );
}

export async function getFinancialReport(
  token: string,
  from: string,
  to: string,
  groupBy: "day" | "month" = "day",
) {
  return identityAdmin<FinancialReportRow[]>("/reports/financial", token, {
    params: { from, to, group_by: groupBy },
  });
}

export async function batchGenerateCodes(
  token: string,
  params: {
    count: number;
    product_id: string;
    plan_code: string;
    duration_days: number;
    expires_at?: string;
    notes?: string;
  },
) {
  return identityAdmin<RedemptionCode[]>("/redemption-codes/batch", token, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function listSettings(token: string) {
  return identityAdmin<{ settings: AdminSetting[] }>("/settings", token);
}

export async function updateSettings(
  token: string,
  settings: { key: string; value: string }[],
) {
  return identityAdmin<{ updated: number }>("/settings", token, {
    method: "PUT",
    body: JSON.stringify({ settings }),
  });
}

export async function listOrganizations(
  token: string,
  params: { limit?: number; offset?: number },
) {
  return identityAdmin<{ data: Organization[] }>("/organizations", token, {
    params: params as Record<string, string | number>,
  });
}

export async function updateOrganizationStatus(
  token: string,
  id: number,
  status: "active" | "suspended",
) {
  return identityAdmin<{ ok: boolean }>(`/organizations/${id}`, token, {
    method: "PATCH",
    body: JSON.stringify({ status }),
  });
}

export async function createProduct(token: string, product: Partial<Product>) {
  return identityAdmin<Product>("/products", token, {
    method: "POST",
    body: JSON.stringify(product),
  });
}

export async function updateProduct(
  token: string,
  id: string,
  product: Partial<Product>,
) {
  return identityAdmin<Product>(`/products/${id}`, token, {
    method: "PUT",
    body: JSON.stringify(product),
  });
}

export async function createPlan(
  token: string,
  productId: string,
  plan: Partial<ProductPlan>,
) {
  return identityAdmin<ProductPlan>(`/products/${productId}/plans`, token, {
    method: "POST",
    body: JSON.stringify(plan),
  });
}

export async function updatePlan(
  token: string,
  planId: number,
  plan: Partial<ProductPlan>,
) {
  return identityAdmin<ProductPlan>(`/plans/${planId}`, token, {
    method: "PUT",
    body: JSON.stringify(plan),
  });
}
