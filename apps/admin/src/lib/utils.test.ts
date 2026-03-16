import { describe, it, expect } from "vitest";
import { cn, formatCurrency, formatDate } from "./utils";

describe("cn", () => {
  it("merges class names", () => {
    expect(cn("px-2", "py-1")).toBe("px-2 py-1");
  });

  it("resolves tailwind conflicts via twMerge", () => {
    // twMerge keeps the last conflicting utility
    expect(cn("px-2", "px-4")).toBe("px-4");
  });

  it("handles conditional classes via clsx", () => {
    expect(cn("base", false && "hidden", "extra")).toBe("base extra");
  });

  it("returns empty string for no inputs", () => {
    expect(cn()).toBe("");
  });

  it("filters out falsy values", () => {
    expect(cn(undefined, null, "", "valid")).toBe("valid");
  });
});

describe("formatCurrency", () => {
  it("formats positive amounts in CNY", () => {
    const result = formatCurrency(1234.56);
    // Intl format for zh-CN CNY includes the yuan sign
    expect(result).toContain("1,234.56");
  });

  it("formats zero", () => {
    const result = formatCurrency(0);
    expect(result).toContain("0.00");
  });

  it("formats negative amounts", () => {
    const result = formatCurrency(-99.9);
    expect(result).toContain("99.90");
  });

  it("formats large numbers with grouping separators", () => {
    const result = formatCurrency(1000000);
    expect(result).toContain("1,000,000.00");
  });
});

describe("formatDate", () => {
  it("formats a Date object", () => {
    // Use a fixed date to avoid timezone issues; just check it contains year/month/day
    const date = new Date("2026-03-13T10:30:00Z");
    const result = formatDate(date);
    expect(result).toContain("2026");
    expect(result).toContain("03");
    expect(result).toContain("13");
  });

  it("formats a date string", () => {
    const result = formatDate("2025-01-15T08:00:00Z");
    expect(result).toContain("2025");
    expect(result).toContain("01");
    expect(result).toContain("15");
  });

  it("includes time components (hour and minute)", () => {
    const result = formatDate("2026-06-01T14:30:00Z");
    // The formatted result should include time
    expect(result).toMatch(/\d{2}:\d{2}/);
  });
});
