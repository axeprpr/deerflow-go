#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_UI_DIR="$REPO_ROOT/third_party/deerflow-ui/frontend"
FALLBACK_UI_DIR="$(cd "$REPO_ROOT/.." && pwd)/deerflow-ui-full/frontend"
UI_DIR="${DEERFLOW_UI_DIR:-}"

if [[ -z "$UI_DIR" ]]; then
  if [[ -d "$DEFAULT_UI_DIR" ]]; then
    UI_DIR="$DEFAULT_UI_DIR"
  elif [[ -d "$FALLBACK_UI_DIR" ]]; then
    UI_DIR="$FALLBACK_UI_DIR"
  else
    echo "deerflow-ui frontend not found. Set DEERFLOW_UI_DIR or add third_party/deerflow-ui/frontend." >&2
    exit 1
  fi
fi

if [[ ! -f "$UI_DIR/package.json" ]]; then
  echo "invalid UI dir: $UI_DIR" >&2
  exit 1
fi

WORK_DIR="$REPO_ROOT/.tmp/ui-build"
DIST_DIR="$REPO_ROOT/internal/webui/dist"
rm -rf "$WORK_DIR" "$DIST_DIR"
mkdir -p "$WORK_DIR" "$DIST_DIR"
cp -a "$UI_DIR/." "$WORK_DIR/"

rm -rf "$WORK_DIR/src/app/api" "$WORK_DIR/src/app/mock/api"

cat > "$WORK_DIR/next.config.js" <<'PATCH'
import "./src/env.js";

/** @type {import("next").NextConfig} */
const config = {
  devIndicators: false,
  output: "export",
  typescript: {
    ignoreBuildErrors: true,
  },
  images: {
    unoptimized: true,
  },
};

export default config;
PATCH

cat > "$WORK_DIR/src/app/layout.tsx" <<'PATCH'
import "@/styles/globals.css";
import "katex/dist/katex.min.css";

import { type Metadata } from "next";

import { ThemeProvider } from "@/components/theme-provider";
import { I18nProvider } from "@/core/i18n/context";

export const metadata: Metadata = {
  title: "DeerFlow",
  description: "A LangChain-based framework for building super agents.",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  const locale = "en-US";
  return (
    <html lang={locale} suppressContentEditableWarning suppressHydrationWarning>
      <body>
        <ThemeProvider attribute="class" enableSystem disableTransitionOnChange>
          <I18nProvider initialLocale={locale}>{children}</I18nProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
PATCH

cat > "$WORK_DIR/src/app/workspace/layout.tsx" <<'PATCH'
"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useCallback, useEffect, useLayoutEffect, useState, Suspense } from "react";
import { Toaster } from "sonner";

import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { CommandPalette } from "@/components/workspace/command-palette";
import { WorkspaceSidebar } from "@/components/workspace/workspace-sidebar";
import { getLocalSettings, useLocalSettings } from "@/core/settings";

const queryClient = new QueryClient();

export default function WorkspaceLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  const [settings, setSettings] = useLocalSettings();
  const [open, setOpen] = useState(false);
  useLayoutEffect(() => {
    setOpen(!getLocalSettings().layout.sidebar_collapsed);
  }, []);
  useEffect(() => {
    setOpen(!settings.layout.sidebar_collapsed);
  }, [settings.layout.sidebar_collapsed]);
  const handleOpenChange = useCallback(
    (open: boolean) => {
      setOpen(open);
      setSettings("layout", { sidebar_collapsed: !open });
    },
    [setSettings],
  );
  return (
    <QueryClientProvider client={queryClient}>
      <SidebarProvider
        className="h-screen"
        open={open}
        onOpenChange={handleOpenChange}
      >
        <Suspense fallback={null}>
          <WorkspaceSidebar />
        </Suspense>
        <SidebarInset className="min-w-0">
          <Suspense fallback={null}>{children}</Suspense>
        </SidebarInset>
      </SidebarProvider>
      <Suspense fallback={null}>
        <CommandPalette />
      </Suspense>
      <Toaster position="top-center" />
    </QueryClientProvider>
  );
}
PATCH

mv "$WORK_DIR/src/app/workspace/chats/[thread_id]/page.tsx" \
  "$WORK_DIR/src/app/workspace/chats/[thread_id]/client-page.tsx"
cat > "$WORK_DIR/src/app/workspace/chats/[thread_id]/page.tsx" <<'PATCH'
import { Suspense } from "react";

import ClientPage from "./client-page";

export function generateStaticParams() {
  return [{ thread_id: "new" }];
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <ClientPage />
    </Suspense>
  );
}
PATCH

mv "$WORK_DIR/src/app/workspace/agents/[agent_name]/chats/[thread_id]/page.tsx" \
  "$WORK_DIR/src/app/workspace/agents/[agent_name]/chats/[thread_id]/client-page.tsx"
cat > "$WORK_DIR/src/app/workspace/agents/[agent_name]/chats/[thread_id]/page.tsx" <<'PATCH'
import { Suspense } from "react";

import ClientPage from "./client-page";

export function generateStaticParams() {
  return [{ agent_name: "general-purpose", thread_id: "new" }];
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <ClientPage />
    </Suspense>
  );
}
PATCH

mv "$WORK_DIR/src/app/workspace/agents/page.tsx" \
  "$WORK_DIR/src/app/workspace/agents/client-page.tsx"
cat > "$WORK_DIR/src/app/workspace/agents/page.tsx" <<'PATCH'
"use client";

import { Suspense } from "react";

import ClientPage from "./client-page";

export default function AgentsPage() {
  return (
    <Suspense fallback={null}>
      <ClientPage />
    </Suspense>
  );
}
PATCH

mv "$WORK_DIR/src/app/workspace/agents/new/page.tsx" \
  "$WORK_DIR/src/app/workspace/agents/new/client-page.tsx"
cat > "$WORK_DIR/src/app/workspace/agents/new/page.tsx" <<'PATCH'
"use client";

import { Suspense } from "react";

import ClientPage from "./client-page";

export default function Page() {
  return (
    <Suspense fallback={null}>
      <ClientPage />
    </Suspense>
  );
}
PATCH

if command -v pnpm >/dev/null 2>&1; then
  PNPM_BIN=(pnpm)
else
  PNPM_BIN=(corepack pnpm)
fi

(
  cd "$WORK_DIR"
  SKIP_ENV_VALIDATION=1 NEXT_PUBLIC_STATIC_WEBSITE_ONLY=false "${PNPM_BIN[@]}" install --frozen-lockfile
  SKIP_ENV_VALIDATION=1 NEXT_PUBLIC_STATIC_WEBSITE_ONLY=false "${PNPM_BIN[@]}" build
)

cp -a "$WORK_DIR/out/." "$DIST_DIR/"
echo "embedded ui generated at $DIST_DIR"
