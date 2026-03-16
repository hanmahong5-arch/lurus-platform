import { describe, it, expect } from "vitest";
import {
	routeEmail,
	extractDomain,
	isChinaDomain,
	classifyRecipients,
} from "../../../lib/smtp/router";

describe("extractDomain", () => {
	it("extracts domain from standard email", () => {
		expect(extractDomain("user@example.com")).toBe("example.com");
	});

	it("handles email with display name parts", () => {
		expect(extractDomain("user@QQ.com")).toBe("qq.com");
	});

	it("returns empty string for invalid email", () => {
		expect(extractDomain("no-at-sign")).toBe("");
	});

	it("uses last @ for edge cases", () => {
		expect(extractDomain("user@sub@163.com")).toBe("163.com");
	});
});

describe("isChinaDomain", () => {
	it("recognizes QQ domains", () => {
		expect(isChinaDomain("qq.com")).toBe(true);
		expect(isChinaDomain("foxmail.com")).toBe(true);
		expect(isChinaDomain("vip.qq.com")).toBe(true);
	});

	it("recognizes Netease domains", () => {
		expect(isChinaDomain("163.com")).toBe(true);
		expect(isChinaDomain("126.com")).toBe(true);
		expect(isChinaDomain("yeah.net")).toBe(true);
	});

	it("recognizes Alibaba domains", () => {
		expect(isChinaDomain("aliyun.com")).toBe(true);
		expect(isChinaDomain("mxhichina.com")).toBe(true);
	});

	it("recognizes telecom domains", () => {
		expect(isChinaDomain("139.com")).toBe(true);
		expect(isChinaDomain("189.cn")).toBe(true);
	});

	it("returns false for international domains", () => {
		expect(isChinaDomain("gmail.com")).toBe(false);
		expect(isChinaDomain("outlook.com")).toBe(false);
		expect(isChinaDomain("yahoo.com")).toBe(false);
	});

	it("is case-insensitive", () => {
		expect(isChinaDomain("QQ.COM")).toBe(true);
		expect(isChinaDomain("Gmail.Com")).toBe(false);
	});
});

describe("routeEmail", () => {
	it("routes China recipients via SendCloud", () => {
		const result = routeEmail(["user@qq.com"]);
		expect(result.channel).toBe("sendcloud");
		expect(result.host).toBe("smtp.sendcloud.net");
	});

	it("routes international recipients via Stalwart", () => {
		const result = routeEmail(["user@gmail.com"]);
		expect(result.channel).toBe("stalwart");
		expect(result.host).toBe("stalwart.mail.svc.cluster.local");
	});

	it("routes mixed recipients via SendCloud (China takes priority)", () => {
		const result = routeEmail(["user@gmail.com", "user@163.com"]);
		expect(result.channel).toBe("sendcloud");
	});

	it("uses custom config when provided", () => {
		const result = routeEmail(["user@gmail.com"], {
			stalwart: { host: "custom-stalwart", port: 465, secure: true },
		});
		expect(result.host).toBe("custom-stalwart");
		expect(result.port).toBe(465);
		expect(result.secure).toBe(true);
	});

	it("uses SendCloud custom config for China recipients", () => {
		const result = routeEmail(["user@qq.com"], {
			sendcloud: {
				host: "custom-relay",
				port: 465,
				secure: true,
				apiUser: "test",
				apiKey: "key",
			},
		});
		expect(result.host).toBe("custom-relay");
		expect(result.port).toBe(465);
	});
});

describe("classifyRecipients", () => {
	it("separates China and international recipients", () => {
		const result = classifyRecipients([
			"a@qq.com",
			"b@gmail.com",
			"c@163.com",
			"d@outlook.com",
		]);
		expect(result.china).toEqual(["a@qq.com", "c@163.com"]);
		expect(result.international).toEqual(["b@gmail.com", "d@outlook.com"]);
	});

	it("handles all-China list", () => {
		const result = classifyRecipients(["a@qq.com", "b@126.com"]);
		expect(result.china).toHaveLength(2);
		expect(result.international).toHaveLength(0);
	});

	it("handles empty list", () => {
		const result = classifyRecipients([]);
		expect(result.china).toHaveLength(0);
		expect(result.international).toHaveLength(0);
	});
});
