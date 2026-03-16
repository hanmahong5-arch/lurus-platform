"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ExternalLink } from "lucide-react";

const monitoringLinks = [
  {
    name: "Grafana",
    url: "https://grafana.lurus.cn",
    description: "Metrics dashboards & alerting",
  },
  {
    name: "Prometheus",
    url: "https://prometheus.lurus.cn",
    description: "Metrics collection & queries",
  },
  {
    name: "Jaeger",
    url: "https://jaeger.lurus.cn",
    description: "Distributed tracing",
  },
  {
    name: "Loki",
    url: "https://loki.lurus.cn",
    description: "Log aggregation",
  },
  {
    name: "ArgoCD",
    url: "https://argocd.lurus.cn",
    description: "GitOps deployment management",
  },
];

export default function SystemPage() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">系统状态</h1>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {monitoringLinks.map((link) => (
          <Card key={link.name}>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">{link.name}</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="mb-3 text-xs text-muted-foreground">
                {link.description}
              </p>
              <a
                href={link.url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-sm text-primary hover:underline"
              >
                打开
                <ExternalLink className="h-3 w-3" />
              </a>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-medium">
            Grafana Dashboard
          </CardTitle>
        </CardHeader>
        <CardContent>
          <iframe
            src="https://grafana.lurus.cn/d/k8s-cluster/kubernetes-cluster?orgId=1&kiosk"
            className="h-[600px] w-full rounded-md border"
            title="Grafana Dashboard"
          />
        </CardContent>
      </Card>
    </div>
  );
}
